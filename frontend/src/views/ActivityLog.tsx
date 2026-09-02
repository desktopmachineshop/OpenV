import React, { useCallback, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { DomainEvent, eventsAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { ErrorBanner } from '../components/ui';

// Event types emitted by the backend (internal/domain/events/events.go).
const EVENT_TYPES = [
  'artifact.created',
  'artifact.updated',
  'artifact.deleted',
  'link.created',
  'link.updated',
  'link.deleted',
  'baseline.captured',
  'chatter.created',
  'testrun.recorded',
  'workitem.created',
  'workitem.moved',
  'workitem.updated',
  'agentrun.finished',
];

// Cursor pagination: each page asks for PAGE_SIZE events older than the
// cursor from the previous page's X-Next-Cursor header (absent header means
// the log is exhausted).
const PAGE_SIZE = 100;

// Tint the type badge by entity family so the list scans visually.
const typeBadgeStyle = (eventType: string): React.CSSProperties => {
  const family = eventType.split('.')[0];
  const palette: Record<string, { bg: string; border: string; text: string }> = {
    artifact: { bg: 'var(--tint-blue)', border: 'var(--tint-blue-border)', text: 'var(--accent-text)' },
    link: { bg: 'var(--tint-purple)', border: 'var(--tint-purple-border)', text: 'var(--purple)' },
    baseline: { bg: 'var(--tint-green)', border: 'var(--tint-green-border)', text: 'var(--success-text)' },
    testrun: { bg: 'var(--tint-green)', border: 'var(--tint-green-border)', text: 'var(--success-text)' },
    workitem: { bg: 'var(--tint-yellow)', border: 'var(--tint-yellow-border)', text: 'var(--warning-text)' },
    agentrun: { bg: 'var(--tint-purple)', border: 'var(--tint-purple-border)', text: 'var(--purple)' },
  };
  const c = palette[family] || {
    bg: 'var(--neutral-soft)',
    border: 'var(--border)',
    text: 'var(--text-muted)',
  };
  return {
    display: 'inline-block',
    padding: '2px 10px',
    borderRadius: 12,
    background: c.bg,
    border: `1px solid ${c.border}`,
    color: c.text,
    fontSize: 11.5,
    fontWeight: 600,
    whiteSpace: 'nowrap',
  };
};

// What each actor kind is called in the table.
const ACTOR_KIND_LABELS: Record<string, string> = {
  user: 'User',
  agent: 'Agent run',
  worker: 'Runner',
  system: 'System',
};

// The server resolves each event's actor into a kind, the ID inside the actor
// string and the name behind it. Fall back to parsing "user:<id>" /
// "agent:<run_id>" / "system" here for responses that carry no names.
const formatActor = (event: DomainEvent): { label: string; kind?: string; detail?: string } => {
  const actor = event.actor;
  const kindLabel = event.actor_kind ? ACTOR_KIND_LABELS[event.actor_kind] || event.actor_kind : undefined;
  if (event.actor_kind) {
    return {
      label: event.actor_name || kindLabel || actor,
      // The kind is only worth repeating once the name has taken the lead.
      kind: event.actor_name ? kindLabel : undefined,
      detail: event.actor_id,
    };
  }
  if (actor === 'system' || !actor) return { label: 'System' };
  const sep = actor.indexOf(':');
  if (sep > 0) {
    const kind = actor.slice(0, sep);
    const id = actor.slice(sep + 1);
    if (kind === 'user') return { label: 'User', detail: id };
    if (kind === 'agent') return { label: 'Agent run', detail: id };
  }
  return { label: actor };
};

const shortId = (id?: string) => (id ? `${id.slice(0, 8)}…` : '—');

const cellStyle: React.CSSProperties = {
  padding: '9px 12px',
  borderBottom: '1px solid var(--neutral-soft)',
  color: 'var(--text-body)',
  verticalAlign: 'top',
};

export const ActivityLog: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  // Events are org-scoped on the backend; refetch once a deep-link workspace
  // switch lands (see the ProjectLayout comment about issues #99/#111/#112).
  const activeOrgId = useAppStore((s) => s.activeOrgId);

  const [events, setEvents] = useState<DomainEvent[]>([]);
  const [typeFilter, setTypeFilter] = useState('all');
  // Cursor for the next (older) page, from the X-Next-Cursor response header;
  // null means there is nothing older to load.
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  // Fetch one page. Without `before` this (re)loads the newest page and
  // replaces the list; with `before` it appends the older page.
  const loadPage = useCallback(
    (before?: string) => {
      if (!projectId) return;
      setLoading(true);
      const query: { project_id: string; event_type?: string; limit: number; before?: string } = {
        project_id: projectId,
        limit: PAGE_SIZE,
      };
      if (typeFilter !== 'all') query.event_type = typeFilter;
      if (before) query.before = before;
      eventsAPI
        .list(query)
        .then((res) => {
          const page = res.data || [];
          setEvents((prev) => {
            if (!before) return page;
            // Guard against duplicates if a refresh raced the append.
            const seen = new Set(prev.map((e) => e.id));
            return [...prev, ...page.filter((e) => !seen.has(e.id))];
          });
          setNextCursor(res.headers?.['x-next-cursor'] || null);
          setError('');
        })
        .catch((err) => setError(`Failed to load activity: ${apiErrorMessage(err)}`))
        .finally(() => setLoading(false));
    },
    // activeOrgId: events are scoped by the X-Org-ID header the API client
    // injects, so refetch when the active workspace changes (e.g.
    // ProjectLayout's cross-org deep-link sync, issues #99/#111/#112).
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [projectId, typeFilter, activeOrgId]
  );

  // (Re)load the newest page whenever the scope changes.
  useEffect(() => {
    loadPage();
  }, [loadPage]);

  // Reset open rows when the filter or project changes (the list itself is
  // replaced by the reload above).
  useEffect(() => {
    setExpanded(new Set());
  }, [typeFilter, projectId]);

  const toggleExpanded = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  // The server advertises the cursor for the next (older) page only when one
  // may exist.
  const maybeMore = nextCursor !== null;

  return (
    <div style={{ padding: 20, height: '100%', display: 'flex', flexDirection: 'column' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Activity</h2>
        <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>
          {loading ? 'Loading…' : `${events.length} event${events.length === 1 ? '' : 's'}`}
        </span>
        <div style={{ flex: 1 }} />
        <label style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>Type</label>
        <select
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          aria-label="Filter by event type"
          style={{ width: 200, padding: '6px 10px' }}
        >
          <option value="all">all</option>
          {EVENT_TYPES.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
      </div>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 8 }} />

      <div style={{ flex: 1, overflowY: 'auto', minHeight: 0 }}>
        <div className="table-container">
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr>
                {['Time', 'Type', 'Actor', 'Entity', ''].map((h, i) => (
                  <th
                    key={h || `col-${i}`}
                    style={{
                      textAlign: 'left',
                      borderBottom: '2px solid var(--neutral-soft)',
                      padding: '10px 12px',
                      color: 'var(--text-muted)',
                      fontWeight: 600,
                      background: 'var(--surface)',
                    }}
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {events.map((event) => {
                const actor = formatActor(event);
                const isOpen = expanded.has(event.id);
                const hasPayload = event.payload && Object.keys(event.payload).length > 0;
                return (
                  <React.Fragment key={event.id}>
                    <tr
                      onClick={() => toggleExpanded(event.id)}
                      style={{ cursor: 'pointer', background: isOpen ? 'var(--tint-blue)' : 'var(--surface)' }}
                    >
                      <td style={{ ...cellStyle, whiteSpace: 'nowrap' }}>
                        {new Date(event.created_at).toLocaleString()}
                      </td>
                      <td style={cellStyle}>
                        <span style={typeBadgeStyle(event.event_type)}>{event.event_type}</span>
                      </td>
                      <td style={cellStyle}>
                        <span title={actor.label}>{actor.label}</span>
                        {actor.kind && (
                          <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 6 }}>{actor.kind}</span>
                        )}
                        {actor.detail && (
                          <span
                            title={actor.detail}
                            style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 6, fontFamily: 'monospace' }}
                          >
                            {shortId(actor.detail)}
                          </span>
                        )}
                      </td>
                      <td style={cellStyle}>
                        {event.entity_name || event.entity_id ? (
                          <div style={{ display: 'flex', alignItems: 'baseline', gap: 6, maxWidth: 420 }}>
                            {event.entity_name && (
                              <span
                                title={event.entity_name}
                                style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                              >
                                {event.entity_name}
                              </span>
                            )}
                            {event.entity_kind && !event.entity_name && (
                              <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{event.entity_kind}</span>
                            )}
                            {event.entity_id && (
                              <span
                                title={event.entity_id}
                                style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--text-muted)' }}
                              >
                                {shortId(event.entity_id)}
                              </span>
                            )}
                          </div>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td style={{ ...cellStyle, textAlign: 'right', color: 'var(--text-muted)', width: 32 }}>
                        <span aria-hidden="true">{isOpen ? '▾' : '▸'}</span>
                      </td>
                    </tr>
                    {isOpen && (
                      <tr>
                        <td colSpan={5} style={{ ...cellStyle, background: 'var(--surface)' }}>
                          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 6 }}>
                            Event <span style={{ fontFamily: 'monospace' }}>{event.id}</span>
                            {event.entity_id && (
                              <>
                                {' · '}
                                {event.entity_kind || 'entity'}{' '}
                                {event.entity_name && <span style={{ color: 'var(--text-body)' }}>{event.entity_name} </span>}
                                <span style={{ fontFamily: 'monospace' }}>{event.entity_id}</span>
                              </>
                            )}
                            {actor.detail && (
                              <>
                                {' · '}
                                {actor.kind ? actor.kind.toLowerCase() : 'actor'}{' '}
                                {actor.label && <span style={{ color: 'var(--text-body)' }}>{actor.label} </span>}
                                <span style={{ fontFamily: 'monospace' }}>{actor.detail}</span>
                              </>
                            )}
                          </div>
                          {hasPayload ? (
                            <pre
                              style={{
                                margin: 0,
                                padding: '8px 10px',
                                background: 'var(--bg-app)',
                                border: '1px solid var(--border)',
                                borderRadius: 4,
                                fontSize: 12,
                                overflowX: 'auto',
                                whiteSpace: 'pre-wrap',
                                overflowWrap: 'anywhere',
                                color: 'var(--text-body)',
                              }}
                            >
                              {JSON.stringify(event.payload, null, 2)}
                            </pre>
                          ) : (
                            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>No payload.</span>
                          )}
                        </td>
                      </tr>
                    )}
                  </React.Fragment>
                );
              })}
              {events.length === 0 && !loading && (
                <tr>
                  <td colSpan={5} style={{ padding: 16, color: 'var(--text-muted)', background: 'var(--surface)' }}>
                    {typeFilter === 'all'
                      ? 'No activity yet. Events appear here as artifacts, links, work items, test runs and agent runs change.'
                      : `No ${typeFilter} events yet.`}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        {maybeMore && (
          <div style={{ padding: '12px 0', textAlign: 'center' }}>
            <button
              onClick={() => nextCursor && loadPage(nextCursor)}
              disabled={loading}
              style={{
                background: 'var(--surface)',
                color: 'var(--text)',
                border: '1px solid var(--border)',
                padding: '7px 18px',
                borderRadius: 4,
                cursor: loading ? 'default' : 'pointer',
                fontSize: 13,
              }}
            >
              {loading ? 'Loading…' : 'Load more'}
            </button>
          </div>
        )}
      </div>
    </div>
  );
};

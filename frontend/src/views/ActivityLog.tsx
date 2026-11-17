import React, { useCallback, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { DomainEvent, artifactAPI, eventsAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { ErrorBanner, buttonStyle, panelStyle } from '../components/ui';
import { EVENT_TYPE_LABELS, dayLabel } from '../components/activityEvents';
import type { DomainEventType } from '../generated/contract';
import { EventDetail, EventSentence, type ArtifactNames } from './ActivityLogParts';

// A project's Activity log (#734): each event a sentence in the words the
// Home uses (components/activityEvents), under its day, linking to what it
// is about; opened, the detail drawn for its kind of event, with the raw
// event behind a toggle. The type filter below is the log's one filter;
// eventQuery is where any other filter joins the request.

// Every event type a project's activity can hold: the types the backend
// emits (internal/domain/events/events.go), in its order, less the
// workspace-level org.* membership events, which carry no project and so
// never appear in a project's log. A typed subset of the generated
// contract's DomainEventType (refactor plan X4b), so a type Go renames or
// drops fails tsc; src/arch/vocabParity.test.ts holds this list to Go's.
const EVENT_TYPES: DomainEventType[] = [
  'artifact.created',
  'artifact.updated',
  'artifact.deleted',
  'artifact.status_changed',
  'artifact.restored',
  'artifact.reinstated',
  'link.created',
  'link.updated',
  'link.deleted',
  'baseline.captured',
  'baseline.deleted',
  'project.review_round_started',
  'chatter.created',
  'testrun.recorded',
  'workitem.created',
  'workitem.moved',
  'workitem.updated',
  'agentrun.finished',
  'agentrun.successors_skipped',
  'proposal.created',
  'project.member_added',
  'project.member_role_changed',
  'project.member_removed',
];

// Cursor pagination: each page asks for PAGE_SIZE events older than the
// cursor from the previous page's X-Next-Cursor header (absent header means
// the log is exhausted).
const PAGE_SIZE = 100;

/** The events request for one page: the project, the filter, and the cursor. */
const eventQuery = (projectId: string, typeFilter: string, before?: string) => {
  const query: { project_id: string; event_type?: string; limit: number; before?: string } = {
    project_id: projectId,
    limit: PAGE_SIZE,
  };
  if (typeFilter !== 'all') query.event_type = typeFilter;
  if (before) query.before = before;
  return query;
};

/** The events grouped under their day's header, newest first as the server sends them. */
const byDay = (events: DomainEvent[], now: Date): { day: string; events: DomainEvent[] }[] => {
  const out: { day: string; events: DomainEvent[] }[] = [];
  for (const e of events) {
    const day = dayLabel(e.created_at, now);
    const last = out[out.length - 1];
    if (last && last.day === day) last.events.push(e);
    else out.push({ day, events: [e] });
  }
  return out;
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
  // The project's artifacts by id, for the refs and titles the sentences
  // name; a log whose read fails names them by the event's own title.
  const [names, setNames] = useState<ArtifactNames>(new Map());

  useEffect(() => {
    if (!projectId) return;
    let live = true;
    artifactAPI
      .list(projectId)
      .then((r) => live && setNames(new Map((Array.isArray(r.data) ? r.data : []).map((a) => [a.id, a]))))
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [projectId, activeOrgId]);

  // Fetch one page. Without `before` this (re)loads the newest page and
  // replaces the list; with `before` it appends the older page.
  const loadPage = useCallback(
    (before?: string) => {
      if (!projectId) return;
      setLoading(true);
      eventsAPI
        .list(eventQuery(projectId, typeFilter, before))
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
    <div
      data-help-key="activity"
      className="ov-gutter" 
      style={{ padding: 16, height: '100%', display: 'flex', flexDirection: 'column' }}
    >
      <div className="ov-page-head" style={{ marginBottom: 12 }}>
        <h2>Activity</h2>
        <span className="ov-page-sub">
          {loading ? 'Loading…' : `${events.length} event${events.length === 1 ? '' : 's'}`}
        </span>
        <div style={{ flex: 1 }} />
        <label style={{ margin: 0, fontSize: 12, fontWeight: 600, color: 'var(--text-muted)' }}>Type</label>
        <select
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          aria-label="Filter by event type"
          style={{ width: 200, height: 32, padding: '0 8px', fontSize: 13, borderRadius: 6 }}
        >
          <option value="all">All events</option>
          {EVENT_TYPES.map((t) => (
            <option key={t} value={t}>
              {EVENT_TYPE_LABELS[t] || t}
            </option>
          ))}
        </select>
      </div>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 8 }} />

      <div style={{ flex: 1, overflowY: 'auto', minHeight: 0 }}>
        {events.length === 0 && !loading && (
          <p style={{ ...panelStyle, margin: 0, padding: 16, color: 'var(--text-muted)' }}>
            {typeFilter === 'all'
              ? 'No activity yet. Events appear here as artifacts, links, work items, test runs and agent runs change.'
              : `No ${(EVENT_TYPE_LABELS[typeFilter] || typeFilter).toLowerCase()} events yet.`}
          </p>
        )}
        {byDay(events, new Date()).map((group) => (
          <section key={group.day} aria-label={group.day} style={{ marginBottom: 12 }}>
            <h3 style={{ margin: '0 0 6px', fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
              {group.day}
            </h3>
            <ol style={{ ...panelStyle, listStyle: 'none', margin: 0, padding: 0, fontSize: 13 }}>
              {group.events.map((event) => {
                const isOpen = expanded.has(event.id);
                return (
                  <li key={event.id} style={{ borderBottom: '1px solid var(--border-soft)', background: isOpen ? 'var(--tint-blue)' : undefined }}>
                    <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8, padding: '8px 12px' }}>
                      <EventSentence e={event} names={names} />
                      <button
                        type="button"
                        aria-expanded={isOpen}
                        aria-label={isOpen ? 'Hide details' : 'Show details'}
                        title={isOpen ? 'Hide details' : 'Show details'}
                        onClick={() => toggleExpanded(event.id)}
                        style={{ flex: 'none', width: 32, height: 28, border: 'none', background: 'none', color: 'var(--text-muted)', cursor: 'pointer' }}
                      >
                        <span aria-hidden="true">{isOpen ? '▾' : '▸'}</span>
                      </button>
                    </div>
                    {isOpen && (
                      <div style={{ padding: '4px 12px 12px 58px', background: 'var(--surface)' }}>
                        <EventDetail e={event} names={names} />
                      </div>
                    )}
                  </li>
                );
              })}
            </ol>
          </section>
        ))}

        {maybeMore && (
          <div style={{ padding: '12px 0', textAlign: 'center' }}>
            <button
              onClick={() => nextCursor && loadPage(nextCursor)}
              disabled={loading}
              style={buttonStyle('secondary', { disabled: loading })}
            >
              {loading ? 'Loading…' : 'Load more'}
            </button>
          </div>
        )}
      </div>
    </div>
  );
};

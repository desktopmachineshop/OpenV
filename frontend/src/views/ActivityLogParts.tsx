import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { agentRunsAPI, artifactAPI, type AgentRun, type Artifact, type DomainEvent } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { Avatar } from '../components/Avatar';
import { WordDiff } from '../components/ArtifactWordDiff';
import { Chip, MONO, mutedStyle, plainLink, refStyle, toneFor } from '../components/ui';
import { TONE_COLOR, eventTarget, humanWord, namesThing, toneOf, verbOf, whoOf } from '../components/activityEvents';
import { STATUS_META } from '../utils/artifactFacts';

// One event of a project's Activity log, as a sentence (#734): who, by name
// and kind, did what, to which thing, linked to it (#718), and on opening
// it the detail drawn for its kind of event, with the raw event behind a
// toggle for debugging.

/** What the log knows of the project's artifacts, by id: their ref and title. */
export type ArtifactNames = Map<string, Pick<Artifact, 'ref' | 'title' | 'type'>>;

const str = (v: unknown): string => (typeof v === 'string' ? v : '');

const ACTOR_KIND: Record<string, string> = { user: 'person', agent: 'agent', worker: 'runner', system: 'system' };

/** The actor's picture or initial for a person, and a mark for an agent, a runner or the system. */
const ActorMark: React.FC<{ e: DomainEvent }> = ({ e }) => {
  const kind = e.actor_kind || (e.actor?.startsWith('user:') ? 'user' : e.actor?.startsWith('agent:') ? 'agent' : 'system');
  if (kind === 'user') return <Avatar name={whoOf(e)} size={22} />;
  const mark = kind === 'agent' ? '✦' : kind === 'worker' ? '⚙' : '•';
  const bg = kind === 'agent' ? 'var(--purple)' : 'var(--neutral-mid)';
  return (
    <span aria-hidden="true" style={{ flex: 'none', width: 22, height: 22, borderRadius: '50%', background: bg, color: 'var(--accent-fg)', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', fontSize: 12 }}>
      {mark}
    </span>
  );
};

/** An artifact by its ref and title, linked to it in the project. */
const ArtifactRef: React.FC<{ id: string; projectId?: string; names: ArtifactNames; fallback?: string }> = ({ id, projectId, names, fallback }) => {
  const a = names.get(id);
  const label = (
    <>
      {a?.ref && <span style={refStyle}>{a.ref}</span>}
      {a?.ref && ' '}
      <span style={{ fontWeight: 600 }}>{a?.title || fallback || 'an artifact'}</span>
    </>
  );
  return projectId && id ? (
    <Link to={`/projects/${projectId}/requirements?artifact=${encodeURIComponent(id)}`} style={{ ...plainLink, color: 'var(--text)' }}>
      {label}
    </Link>
  ) : (
    label
  );
};

/** The thing the event happened to, as the sentence names it. */
const Subject: React.FC<{ e: DomainEvent; names: ArtifactNames }> = ({ e, names }) => {
  const p = e.payload || {};
  const family = e.event_type.split('.')[0];
  if (!namesThing(e)) return null;
  if (family === 'link') {
    return (
      <>
        <ArtifactRef id={str(p.from_id)} projectId={e.project_id} names={names} />{' '}
        <span style={mutedStyle}>—{str(p.link_type) || 'link'}→</span>{' '}
        <ArtifactRef id={str(p.to_id)} projectId={e.project_id} names={names} />
      </>
    );
  }
  const artifactId = family === 'artifact' ? e.entity_id || '' : str(p.artifact_id) || str(p.test_case_id);
  const a = artifactId ? names.get(artifactId) : undefined;
  const type = family === 'artifact' ? str(p.artifact_type) || a?.type : '';
  const title = a?.title || e.entity_name || str(p.title) || str(p.name) || str(p.op);
  const to = eventTarget(e);
  const label = (
    <>
      {a?.ref && <span style={refStyle}>{a.ref}</span>}
      {a?.ref && ' '}
      {type && (
        <span style={{ ...mutedStyle, fontSize: 11 }}>
          {humanWord(type)}{' '}
        </span>
      )}
      <span style={{ fontWeight: 600 }}>{title}</span>
    </>
  );
  if (!title && !a?.ref) return null;
  return to ? (
    <Link to={to} style={{ ...plainLink, color: 'var(--text)' }}>
      {label}
    </Link>
  ) : (
    label
  );
};

const timeOf = (iso: string) => {
  const t = new Date(iso);
  return Number.isNaN(t.getTime()) ? '' : t.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
};

/** The event's sentence: mark, who (and what kind of actor), verb in its tone, the thing, the time. */
export const EventSentence: React.FC<{ e: DomainEvent; names: ArtifactNames }> = ({ e, names }) => {
  const kind = ACTOR_KIND[e.actor_kind || ''];
  return (
    <span style={{ display: 'flex', alignItems: 'flex-start', gap: 8, minWidth: 0, flex: 1 }}>
      <span aria-hidden="true" style={{ flex: 'none', width: 8, height: 8, marginTop: 7, borderRadius: '50%', background: TONE_COLOR[toneOf(e)] }} />
      <ActorMark e={e} />
      <span style={{ minWidth: 0, lineHeight: 1.6 }}>
        <b style={{ fontWeight: 600 }}>{whoOf(e)}</b>
        {kind && kind !== 'person' && <span style={{ ...mutedStyle, marginLeft: 4 }}>({kind})</span>}{' '}
        <span style={{ color: TONE_COLOR[toneOf(e)], fontWeight: 600 }}>{verbOf(e)}</span> <Subject e={e} names={names} />
        <span style={mutedStyle}> · {timeOf(e.created_at)}</span>
      </span>
    </span>
  );
};

const statusChip = (raw: unknown) => {
  const meta = STATUS_META[raw as keyof typeof STATUS_META];
  return <Chip tone={meta?.tone ?? toneFor(str(raw))}>{meta?.label ?? (humanWord(raw) || 'unknown')}</Chip>;
};

const Arrow = () => <span aria-label="to" style={{ color: 'var(--text-muted)', margin: '0 6px' }}>→</span>;

const rowStyle: React.CSSProperties = { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 6 };

/** What changed in an edit: the title and the text word by word, and each attribute before and after. */
const EditDetail: React.FC<{ e: DomainEvent }> = ({ e }) => {
  const version = Number(e.payload?.version) || 0;
  const [pair, setPair] = useState<{ before?: Artifact; after?: Artifact } | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    if (!e.entity_id) return;
    let live = true;
    artifactAPI
      .getVersions(e.entity_id)
      .then((r) => {
        if (!live) return;
        const all = Array.isArray(r.data) ? [...r.data].sort((a, b) => a.version - b.version) : [];
        const i = version ? all.findIndex((a) => a.version === version) : all.length - 1;
        setPair(i >= 0 ? { before: all[i - 1], after: all[i] } : {});
      })
      .catch((err) => live && setError(apiErrorMessage(err)));
    return () => {
      live = false;
    };
  }, [e.entity_id, version]);
  if (error) return <span style={mutedStyle}>The versions could not be read: {error}</span>;
  if (!pair) return <span style={mutedStyle} aria-busy="true">Reading the versions…</span>;
  const { before, after } = pair;
  if (!before || !after) return <span style={mutedStyle}>Version {version || '?'}: the version before it is not available to compare.</span>;
  const keys = [...new Set([...Object.keys(before.attributes || {}), ...Object.keys(after.attributes || {})])].filter(
    (k) => JSON.stringify(before.attributes?.[k] ?? null) !== JSON.stringify(after.attributes?.[k] ?? null)
  );
  const fmt = (v: unknown) => (v === undefined || v === null || v === '' ? '—' : typeof v === 'object' ? JSON.stringify(v) : String(v));
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <span style={mutedStyle}>Version {before.version} → {after.version}</span>
      {before.title !== after.title && (
        <div>
          <div style={{ ...mutedStyle, fontWeight: 600 }}>Title</div>
          <WordDiff before={before.title} after={after.title} fontSize={13} />
        </div>
      )}
      {before.body !== after.body && (
        <div>
          <div style={{ ...mutedStyle, fontWeight: 600 }}>Text</div>
          <WordDiff before={before.body || ''} after={after.body || ''} fontSize={13} />
        </div>
      )}
      {keys.map((k) => (
        <div key={k} style={rowStyle}>
          <span style={{ ...mutedStyle, fontWeight: 600, minWidth: 140 }}>{humanWord(k)}</span>
          <span>{fmt(before.attributes?.[k])}</span>
          <Arrow />
          <span>{fmt(after.attributes?.[k])}</span>
        </div>
      ))}
      {before.title === after.title && before.body === after.body && keys.length === 0 && <span style={mutedStyle}>No field changed in the text or attributes.</span>}
    </div>
  );
};

/** A finished agent run: its status, how long it took, what it cost, and the run. */
const RunDetail: React.FC<{ e: DomainEvent; to: string }> = ({ e, to }) => {
  const [run, setRun] = useState<AgentRun | null>(null);
  useEffect(() => {
    if (!e.entity_id) return;
    let live = true;
    agentRunsAPI.get(e.entity_id).then((r) => live && setRun(r.data)).catch(() => undefined);
    return () => {
      live = false;
    };
  }, [e.entity_id]);
  const secs = run?.started_at && run.finished_at ? Math.round((new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()) / 1000) : null;
  return (
    <div style={rowStyle}>
      <Chip>{humanWord(run?.status || e.payload?.status) || 'finished'}</Chip>
      {secs !== null && secs >= 0 && <span style={mutedStyle}>took {secs < 90 ? `${secs}s` : `${Math.round(secs / 60)} min`}</span>}
      {typeof run?.cost_usd === 'number' && <span style={mutedStyle}>· cost ${run.cost_usd.toFixed(2)}</span>}
      {to && (
        <Link to={to} style={plainLink}>
          Open the run
        </Link>
      )}
    </div>
  );
};

const LABELS: Record<string, string> = {
  artifact_type: 'Type',
  title: 'Title',
  name: 'Name',
  version: 'Version',
  restored_version: 'Restored from version',
  link_type: 'Link type',
  column: 'Column',
  assignee_type: 'Assigned to',
  status: 'Status',
  op: 'Change',
  role: 'Role',
  from: 'From',
  to: 'To',
  message: 'Message',
  entry_type: 'Kind',
  reason: 'Reason',
  successors: 'Skipped steps',
  moved: 'Sent for review',
  approved: 'Already approved',
  already_in_review: 'Already in review',
  types: 'Types',
};

// Payload keys that are ids: the sentence already names what they point at.
const ID_KEY = /(^|_)ids?$/;

/** Any other event: its payload as labelled values, ids left out. */
const Fields: React.FC<{ e: DomainEvent }> = ({ e }) => {
  const entries = Object.entries(e.payload || {}).filter(([k]) => !ID_KEY.test(k));
  if (entries.length === 0) return <span style={mutedStyle}>Nothing more was recorded with this event.</span>;
  return (
    <dl style={{ margin: 0, display: 'grid', gridTemplateColumns: 'max-content 1fr', gap: '4px 12px' }}>
      {entries.map(([k, v]) => (
        <React.Fragment key={k}>
          <dt style={{ ...mutedStyle, fontWeight: 600 }}>{LABELS[k] || humanWord(k)}</dt>
          <dd style={{ margin: 0, overflowWrap: 'anywhere' }}>{Array.isArray(v) ? v.join(', ') : typeof v === 'boolean' ? (v ? 'yes' : 'no') : humanWord(v) || '—'}</dd>
        </React.Fragment>
      ))}
    </dl>
  );
};

/** The detail drawn for the event's kind. */
const Detail: React.FC<{ e: DomainEvent; names: ArtifactNames }> = ({ e, names }) => {
  const p = e.payload || {};
  const to = eventTarget(e);
  switch (e.event_type) {
    case 'artifact.status_changed':
      return (
        <div style={rowStyle}>
          {statusChip(p.from)}
          <Arrow />
          {statusChip(p.to)}
          {p.version !== undefined && <span style={mutedStyle}>· version {String(p.version)}</span>}
          {p.review_round && <span style={mutedStyle}>· in a review round</span>}
        </div>
      );
    case 'artifact.updated':
      return <EditDetail e={e} />;
    case 'link.created':
    case 'link.updated':
    case 'link.deleted':
      return (
        <div style={rowStyle}>
          <ArtifactRef id={str(p.from_id)} projectId={e.project_id} names={names} />
          <span style={{ ...mutedStyle, fontFamily: MONO }}>—{str(p.link_type) || 'link'}→</span>
          <ArtifactRef id={str(p.to_id)} projectId={e.project_id} names={names} />
          {e.event_type === 'link.deleted' && <Chip tone="warning">removed</Chip>}
        </div>
      );
    case 'workitem.moved':
    case 'workitem.created':
      return (
        <div style={rowStyle}>
          {e.event_type === 'workitem.moved' && <span style={mutedStyle}>Moved</span>}
          <Arrow />
          <Chip tone="info">{humanWord(p.column) || 'a column'}</Chip>
          {str(p.assignee_type) && <span style={mutedStyle}>· assigned to {humanWord(p.assignee_type) === 'agent' ? 'an agent' : 'a person'}</span>}
        </div>
      );
    case 'testrun.recorded':
      return (
        <div style={rowStyle}>
          <Chip>{str(p.status) || 'result'}</Chip>
          <ArtifactRef id={str(p.test_case_id)} projectId={e.project_id} names={names} />
          {to && (
            <Link to={to} style={plainLink}>
              Open the test run
            </Link>
          )}
        </div>
      );
    case 'agentrun.finished':
      return <RunDetail e={e} to={to} />;
    case 'chatter.created':
      return (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          {str(p.message) && <blockquote style={{ margin: 0, paddingLeft: 10, borderLeft: '3px solid var(--border)', whiteSpace: 'pre-wrap' }}>{str(p.message)}</blockquote>}
          {to && (
            <Link to={to} style={plainLink}>
              Open the thread
            </Link>
          )}
        </div>
      );
    case 'artifact.restored':
      return (
        <span>
          Version {String(p.version ?? '?')} brings back version {String(p.restored_version ?? '?')}.
        </span>
      );
    case 'artifact.reinstated':
      return <span>Back from the archive as version {String(p.version ?? '?')}, as it was when archived.</span>;
    case 'project.member_role_changed':
      return (
        <div style={rowStyle}>
          <Chip>{humanWord(p.from)}</Chip>
          <Arrow />
          <Chip>{humanWord(p.to)}</Chip>
        </div>
      );
    default:
      return <Fields e={e} />;
  }
};

/** The open event: its detail, and the raw event behind "Show raw event". */
export const EventDetail: React.FC<{ e: DomainEvent; names: ArtifactNames }> = ({ e, names }) => {
  const [raw, setRaw] = useState(false);
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10, fontSize: 13 }}>
      <Detail e={e} names={names} />
      <div>
        <button
          type="button"
          aria-expanded={raw}
          onClick={() => setRaw((r) => !r)}
          style={{ padding: 0, border: 'none', background: 'none', color: 'var(--text-muted)', fontSize: 12, cursor: 'pointer', textDecoration: 'underline' }}
        >
          {raw ? 'Hide raw event' : 'Show raw event'}
        </button>
        {raw && (
          <pre
            style={{
              margin: '6px 0 0',
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
            {JSON.stringify(e, null, 2)}
          </pre>
        )}
      </div>
    </div>
  );
};

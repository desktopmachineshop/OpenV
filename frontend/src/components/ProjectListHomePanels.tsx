import React, { useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { linkAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { Chip, PanelHeader, ellipsis, mutedStyle, panelStyle, plainLink, refStyle, tapTarget, useConfirm, type ChipTone } from './ui';
import { TONE_COLOR, type ActivityItem } from './activityEvents';
import type { TodoItem, TodoKind } from './ProjectListHomeData';
import { MarkAllReadButton, MarkReadButton, ReadUndoRow, readGroupOf, type ReadGroup } from './NoteReadControls';

// The rows the two Homes share (the workspace Home on the Projects page and a
// project's Summary): what needs the member, and the audit trail. Every row
// opens the thing it names (#718, #719); a suspect link is confirmed or
// removed from its row, in place, and drops out of the list. With read state
// (comment-reads), a comment is marked read from its row, or every comment
// from the header, and leaves a line with Undo to keep it for later.

const KIND_TONE: Record<TodoKind, ChipTone> = {
  approve: 'info',
  suspect: 'warning',
  'test run': 'neutral',
  failing: 'danger',
  comment: 'neutral',
  'to-do': 'info',
};

/** An artifact reference (REQ-12), drawn in monospace; a name is not. */
const REF = /^[A-Z][A-Z0-9]*-\d+$/;

const iconButton = (tone: 'ok' | 'bad'): React.CSSProperties => ({
  ...tapTarget,
  justifyContent: 'center',
  padding: 0,
  border: '1px solid var(--border)',
  borderRadius: 6,
  background: 'var(--surface)',
  color: tone === 'ok' ? 'var(--success-text)' : 'var(--danger-strong)',
  fontSize: 15,
  fontWeight: 700,
  cursor: 'pointer',
});

interface TodoRowProps {
  t: TodoItem & { project?: string };
  /** Called once the row's link is confirmed or removed, so the list drops it. */
  onDone: (key: string) => void;
  /** A comment row's Mark read: what it marks, and what to do once it has. */
  read?: { group: ReadGroup; onRead: (g: ReadGroup) => void };
}

const SuspectActions: React.FC<TodoRowProps> = ({ t, onDone }) => {
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const name = `${t.ref ? `${t.ref} ` : ''}${t.title}`;
  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError('');
    try {
      await fn();
      onDone(t.key);
    } catch (err) {
      setError(apiErrorMessage(err));
      setBusy(false);
    }
  };
  const reject = async () => {
    const ok = await confirm({
      title: 'Remove this link?',
      message: `${name}. The link is deleted from the project; you can add it again from either artifact.`,
      confirmLabel: 'Remove link',
      danger: true,
    });
    if (ok) await act(() => linkAPI.delete(t.linkId!));
  };
  return (
    <span style={{ flex: 'none', display: 'flex', alignItems: 'center', gap: 6 }}>
      {error && (
        <span role="alert" style={{ ...mutedStyle, color: 'var(--danger-strong)', maxWidth: 160 }}>
          {error}
        </span>
      )}
      <button
        type="button"
        aria-label={`Confirm the link: ${name}`}
        title="Confirm: the link still holds"
        disabled={busy}
        onClick={() => act(() => linkAPI.confirm(t.linkId!))}
        style={iconButton('ok')}
      >
        ✓
      </button>
      <button
        type="button"
        aria-label={`Remove the link: ${name}`}
        title="Reject: remove the link"
        disabled={busy}
        onClick={reject}
        style={iconButton('bad')}
      >
        ✗
      </button>
    </span>
  );
};

const TodoRow: React.FC<TodoRowProps> = ({ t, onDone, read }) => (
  <li className="ov-home-row" style={{ display: 'flex', alignItems: 'center', gap: 10, minHeight: 40, padding: '4px 12px', borderBottom: '1px solid var(--border-soft)' }}>
    <Link to={t.open} className="ov-home-row-link" style={{ flex: 1, minWidth: 0, display: 'flex', alignItems: 'center', gap: 10, color: 'inherit', textDecoration: 'none' }}>
      <span style={{ flex: 'none', width: 76 }}>
        <Chip tone={KIND_TONE[t.kind]}>{t.kind}</Chip>
      </span>
      <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
        <span style={ellipsis}>
          {t.ref && <span style={refStyle}>{t.ref}</span>} {t.title}
        </span>
        <span style={{ ...mutedStyle, ...ellipsis }}>
          {t.project && <b style={{ fontWeight: 600 }}>{t.project} · </b>}
          {t.meta}
        </span>
      </span>
    </Link>
    {read && <MarkReadButton group={read.group} onRead={read.onRead} />}
    {t.kind === 'suspect' && t.linkId ? (
      <SuspectActions t={t} onDone={onDone} />
    ) : (
      <Link to={t.to} style={{ ...plainLink, ...tapTarget, flex: 'none', fontSize: 12, fontWeight: 600 }}>
        {t.action}
      </Link>
    )}
  </li>
);

/**
 * A panel's body under its header. In a panel laid out as a flex column with
 * a fixed height (fitPanel), it takes the space left and scrolls on its own;
 * anywhere else it is a plain block.
 */
export const panelScroll: React.CSSProperties = { flex: '1 1 auto', minHeight: 0, overflowY: 'auto' };

/** A panel sized by its place on the page, not its content: its body scrolls (panelScroll). */
export const fitPanel: React.CSSProperties = { display: 'flex', flexDirection: 'column', minHeight: 0, overflow: 'hidden' };

type NeedsItem = TodoItem & { project?: string; projectId?: string };

/**
 * The Needs you panel: its header with the count still waiting and the
 * header's action (Review all), then at most `limit` rows. A row confirmed or
 * removed here drops out, and the count with it.
 */
export const NeedsPanel: React.FC<{
  items: NeedsItem[] | null;
  limit: number;
  action?: React.ReactNode;
  emptyText: string;
  /** The line under a list longer than the limit, given how many are not shown. */
  more?: (n: number) => string;
  style?: React.CSSProperties;
}> = ({ items, limit, action, emptyText, more, style }) => {
  const [gone, setGone] = useState<Set<string>>(() => new Set());
  // Comments marked read here, newest first: their rows leave the list, and
  // each leaves a line with Undo until the page is next loaded.
  const [read, setRead] = useState<ReadGroup[]>([]);
  const marks = useRef(0);
  const keyOf = (t: NeedsItem) => `${t.projectId || ''}-${t.key}`;
  const readIds = new Set(read.flatMap((g) => g.artifacts.map((a) => a.artifactId)));
  const left = (items || []).filter((t) => !gone.has(keyOf(t)) && !(t.comment && readIds.has(t.comment.artifactId)));
  const comments = left.filter((t) => t.comment);
  const onRead = (g: ReadGroup) => {
    marks.current += 1;
    setRead((r) => [{ ...g, id: `${g.id}-${marks.current}` }, ...r]);
  };
  const onUndone = (g: ReadGroup) => setRead((r) => r.filter((x) => x.id !== g.id));
  const rowRead = (t: NeedsItem) => {
    if (!t.comment) return undefined;
    const id = t.comment.artifactId;
    const label = `${t.ref ? `${t.ref} ` : ''}${t.title}`;
    return { group: readGroupOf(keyOf(t), label, comments.filter((c) => c.comment?.artifactId === id)), onRead };
  };
  const markAll =
    comments.length > 0 ? (
      <MarkAllReadButton
        group={readGroupOf('all', comments.length === 1 ? '1 comment' : `${comments.length} comments`, comments)}
        onRead={onRead}
      />
    ) : null;
  const headerAction =
    markAll && action ? (
      <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        {markAll}
        {action}
      </span>
    ) : (
      markAll || action
    );
  return (
    <section aria-labelledby="home-needs" data-help-key="home.needs" style={{ ...panelStyle, ...style }}>
      <PanelHeader id="home-needs" title="Needs you" count={items ? left.length : undefined} action={headerAction} />
      <div style={panelScroll}>
        {!items && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>Gathering what waits on you…</p>}
        {items && left.length === 0 && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>{emptyText}</p>}
        <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
          {read.map((g) => (
            <ReadUndoRow key={g.id} group={g} onUndone={onUndone} />
          ))}
          {left.slice(0, limit).map((t) => (
            <TodoRow key={keyOf(t)} t={t} read={rowRead(t)} onDone={() => setGone((g) => new Set(g).add(keyOf(t)))} />
          ))}
        </ul>
        {more && left.length > limit && <p style={{ ...mutedStyle, margin: 0, padding: '8px 12px' }}>{more(left.length - limit)}</p>}
      </div>
    </section>
  );
};

/** The audit trail: one row per event, each a link to what it describes when there is one. */
export const ActivityRows: React.FC<{ items: (ActivityItem & { project?: string })[] }> = ({ items }) => (
  <ol style={{ listStyle: 'none', margin: 0, padding: items.length ? '6px 8px 10px' : 0 }}>
    {items.map((a) => {
      const body = (
        <>
          <span aria-hidden="true" style={{ flex: 'none', width: 8, height: 8, marginTop: 6, borderRadius: '50%', background: TONE_COLOR[a.tone] }} />
          <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
            <span>
              <b style={{ fontWeight: 600 }}>{a.who}</b> {a.what}{' '}
              {a.ref && <span style={REF.test(a.ref) ? refStyle : { fontWeight: 600 }}>{a.ref}</span>}
            </span>
            <span style={mutedStyle}>
              {a.when}
              {a.project ? ` · ${a.project}` : ''}
            </span>
          </span>
        </>
      );
      const row: React.CSSProperties = { display: 'flex', gap: 10, padding: '6px 4px', borderRadius: 4, color: 'inherit', textDecoration: 'none' };
      return (
        <li key={a.id}>
          {a.to ? (
            <Link to={a.to} className="ov-home-row-link" style={row}>
              {body}
            </Link>
          ) : (
            <div style={row}>{body}</div>
          )}
        </li>
      );
    })}
  </ol>
);

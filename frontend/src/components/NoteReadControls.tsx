import React, { useState } from 'react';
import { apiErrorMessage } from '../api/errors';
import type { ChatterEntry } from '../api/client';
import { Chip, mutedStyle, plainLink, tapTarget } from './ui';
import { markCommentsRead, markCommentsUnread, type useNoteReads } from './noteReadState';

// The Needs you panel's read state controls (comment-reads): Mark read on a
// comment's row, Mark all read in the panel's header, and the line a marked
// row leaves behind with Undo, which marks the comments unread again so they
// stay in the list for later.

/** Comments marked read from the panel: the line that offers Undo. */
export interface ReadGroup {
  id: string;
  /** What was marked read: "REQ-12 Answer in time", or "3 comments". */
  label: string;
  /** Each artifact marked, from its earliest comment the panel listed. */
  artifacts: { artifactId: string; at: string }[];
}

const textButton: React.CSSProperties = {
  ...plainLink,
  ...tapTarget,
  flex: 'none',
  padding: '0 4px',
  border: 'none',
  background: 'none',
  fontSize: 12,
  fontWeight: 600,
  cursor: 'pointer',
};

/** A button that runs a read state write, showing what went wrong in place. */
const ReadButton: React.FC<{
  label: string;
  title: string;
  ariaLabel?: string;
  run: () => Promise<unknown>;
  onDone: () => void;
}> = ({ label, title, ariaLabel, run, onDone }) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const click = async () => {
    setBusy(true);
    setError('');
    try {
      await run();
      onDone();
    } catch (err) {
      setError(apiErrorMessage(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      {error && (
        <span role="alert" style={{ ...mutedStyle, color: 'var(--danger-strong)', maxWidth: 160 }}>
          {error}
        </span>
      )}
      <button type="button" title={title} aria-label={ariaLabel} disabled={busy} onClick={click} style={textButton}>
        {label}
      </button>
    </>
  );
};

/** Mark read, on a comment's row: every comment on its artifact leaves the list. */
export const MarkReadButton: React.FC<{ group: ReadGroup; onRead: (g: ReadGroup) => void }> = ({ group, onRead }) => (
  <ReadButton
    label="Mark read"
    title="Mark this artifact's comments read: they leave Needs you"
    ariaLabel={`Mark read: ${group.label}`}
    run={() => markCommentsRead(group.artifacts.map((a) => a.artifactId))}
    onDone={() => onRead(group)}
  />
);

/** Mark all read, in the panel's header: every comment the list holds. */
export const MarkAllReadButton: React.FC<{ group: ReadGroup; onRead: (g: ReadGroup) => void }> = ({ group, onRead }) => (
  <ReadButton
    label="Mark all read"
    title="Mark every comment in Needs you read"
    run={() => markCommentsRead(group.artifacts.map((a) => a.artifactId))}
    onDone={() => onRead(group)}
  />
);

/** The line a marked row leaves: what was marked, and Undo to keep it for later. */
export const ReadUndoRow: React.FC<{ group: ReadGroup; onUndone: (g: ReadGroup) => void }> = ({ group, onUndone }) => (
  <li
    className="ov-home-row"
    style={{ display: 'flex', alignItems: 'center', gap: 10, minHeight: 40, padding: '4px 12px', borderBottom: '1px solid var(--border-soft)' }}
  >
    <span style={{ ...mutedStyle, flex: 1, minWidth: 0 }}>Marked read: {group.label}</span>
    <ReadButton
      label="Undo"
      title="Mark unread: keep it in Needs you for later"
      ariaLabel={`Mark unread: ${group.label}`}
      run={() => markCommentsUnread(group.artifacts)}
      onDone={() => onUndone(group)}
    />
  </li>
);

/** The artifacts of these comment rows, each from its earliest comment. */
export const readGroupOf = (
  id: string,
  label: string,
  rows: { comment?: { artifactId: string; at: string } }[],
): ReadGroup => {
  const earliest = new Map<string, string>();
  for (const r of rows) {
    if (!r.comment) continue;
    const seen = earliest.get(r.comment.artifactId);
    if (!seen || Date.parse(r.comment.at) < Date.parse(seen)) earliest.set(r.comment.artifactId, r.comment.at);
  }
  return { id, label, artifacts: [...earliest].map(([artifactId, at]) => ({ artifactId, at })) };
};

/**
 * On a comment in an artifact's history (comment-reads): New while the
 * member has not read it, and Mark unread on someone else's comment, which
 * puts it and every later one back in Needs you.
 */
export const NoteReadMark: React.FC<{
  entry: ChatterEntry;
  reads: ReturnType<typeof useNoteReads>;
}> = ({ entry, reads }) => {
  if (!reads.enabled || !reads.theirs(entry)) return null;
  const fresh = reads.isNew(entry);
  return (
    <span style={{ float: 'right', display: 'inline-flex', alignItems: 'center', gap: 6 }}>
      {fresh && <Chip tone="info">{reads.kept ? 'Unread' : 'New'}</Chip>}
      {!(fresh && reads.kept) && (
        <ReadButton
          label="Mark unread"
          title="Keep this comment, and any after it, in Needs you for later"
          run={() => reads.markUnread(entry)}
          onDone={() => undefined}
        />
      )}
    </span>
  );
};

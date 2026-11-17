// A task's History: what the board recorded about the card and what people
// wrote on it, drawn with the artifact History's own parts (NoteFeed), so
// the filter, the entries and the box to write in are the same on both.
import React, { useState } from 'react';
import type { WorkItemActivity } from '../../api/client';
import { ExpandableText } from '../ExpandableText';
import { HistoryEmpty, HistoryEntry, HistoryShell, filterHistory, type HistoryFilter, type HistoryVariant } from '../NoteFeed';

// An agent's entries and a failure keep the colours the card always gave them.
const TONES: Record<string, React.CSSProperties> = {
  agent: { backgroundColor: 'var(--tint-purple)', borderColor: 'var(--tint-purple-border)' },
  agent_run: { backgroundColor: 'var(--tint-purple)', borderColor: 'var(--tint-purple-border)' },
  error: { backgroundColor: 'var(--tint-red)', borderColor: 'var(--tint-red-border)' },
};

const EMPTY: Record<HistoryFilter, string> = {
  all: 'No activity yet.',
  changes: 'No changes recorded yet. Moving the card or changing it adds to this list.',
  comments: 'No comments yet. Add the first one below.',
};

const isComment = (a: WorkItemActivity) => a.kind === 'comment';

export interface WorkItemHistoryProps {
  activity: WorkItemActivity[];
  /** Saves the comment; resolves once it is on the card. */
  onComment: (text: string) => Promise<void> | void;
  /** `card` in the full view, where History is a card of its own; `section` in the drawer and the docked panel. */
  variant: Exclude<HistoryVariant, 'panel'>;
}

export const WorkItemHistory: React.FC<WorkItemHistoryProps> = ({ activity, onComment, variant }) => {
  // Everything, as the card has always shown: a task's changes are few, and they are its story.
  const [filter, setFilter] = useState<HistoryFilter>('all');
  const [comment, setComment] = useState('');
  const shown = filterHistory(activity, filter, (a) => !isComment(a));
  const add = async () => {
    const text = comment.trim();
    if (!text) return;
    setComment('');
    // A comment written while reading the changes would land out of sight.
    if (filter === 'changes') setFilter('comments');
    await onComment(text);
  };
  const composer = (
    <>
      <textarea
        value={comment}
        onChange={(e) => setComment(e.target.value)}
        placeholder="Write a comment…"
        aria-label="Add a comment"
        style={{ minHeight: 60, fontSize: 13 }}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && e.ctrlKey) {
            e.preventDefault();
            void add();
          }
        }}
      />
      <button className="button" style={{ marginTop: 6, padding: '6px 16px', fontSize: 13 }} onClick={() => void add()} disabled={!comment.trim()}>
        Comment
      </button>
    </>
  );
  return (
    <HistoryShell variant={variant} filter={filter} onFilter={setFilter} composer={composer} helpKey="board.card">
      {shown.length === 0 && <HistoryEmpty>{EMPTY[filter]}</HistoryEmpty>}
      {shown.map((a) => (
        <HistoryEntry key={a.id} auto={!isComment(a)} author={a.actor || 'system'} when={a.created_at} kind={isComment(a) ? undefined : a.kind} tone={TONES[a.kind]}>
          <ExpandableText text={a.content || ''} limit={600} />
        </HistoryEntry>
      ))}
    </HistoryShell>
  );
};

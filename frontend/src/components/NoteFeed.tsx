// The History of an artifact and of a task: one timeline of what the
// system recorded and what people wrote, a filter between the two, and the
// box to add a comment. An artifact's (ArtifactHistory) and a task's
// (kanban/WorkItemHistory) read their entries from different places and
// draw them with these, so the two read and behave alike.
import React from 'react';
import { SegmentedControl } from './ui/SegmentedControl';

// The feed carries two kinds of entry: what the system recorded when the
// thing changed and what a person wrote. The filter is that split.
export type HistoryFilter = 'all' | 'changes' | 'comments';

// Comments first: people come here to talk to each other, and the recorded
// changes are the backdrop to that.
export const HISTORY_FILTERS: { value: HistoryFilter; label: string; title: string }[] = [
  { value: 'comments', label: 'Comments', title: 'Only what people wrote' },
  { value: 'changes', label: 'Changes', title: 'Only what the system recorded' },
  { value: 'all', label: 'All', title: 'Changes and comments, newest first' },
];

/** The entries a filter leaves, given which of them the system recorded. */
export const filterHistory = <T,>(entries: T[], filter: HistoryFilter, isAuto: (entry: T) => boolean): T[] =>
  filter === 'all' ? entries : entries.filter((e) => isAuto(e) === (filter === 'changes'));

/**
 * `panel`: fills a column, the list scrolling between the filter and the
 * composer. `section`: a section at the foot of a pane, scrolling with it.
 * `card`: the same, as a card of its own, in a detail view.
 */
export type HistoryVariant = 'panel' | 'section' | 'card';

export interface HistoryShellProps {
  variant: HistoryVariant;
  filter: HistoryFilter;
  onFilter: (filter: HistoryFilter) => void;
  /** The box to write a comment in, with its button. */
  composer: React.ReactNode;
  /** The entries, or the line that says there are none. */
  children: React.ReactNode;
  sectionRef?: React.Ref<HTMLElement>;
  helpKey?: string;
}

export const HistoryShell: React.FC<HistoryShellProps> = ({ variant, filter, onFilter, composer, children, sectionRef, helpKey }) => {
  const panel = variant === 'panel';
  const edge = '1px solid var(--neutral-soft)';
  const filterBar = (
    <div style={panel ? { padding: '8px 10px', borderBottom: edge, backgroundColor: 'var(--surface-alt)' } : { padding: '0 0 8px' }}>
      <SegmentedControl aria-label="Filter history" options={HISTORY_FILTERS} value={filter} onChange={onFilter} style={{ width: '100%' }} />
    </div>
  );
  const list = <div style={panel ? { flex: 1, overflowY: 'auto', padding: '10px' } : { padding: '4px 0' }}>{children}</div>;
  const box = <div style={panel ? { padding: '10px', borderTop: edge, backgroundColor: 'var(--surface-alt)' } : { padding: '0 0 12px' }}>{composer}</div>;
  if (panel) {
    return (
      <>
        {filterBar}
        {list}
        {box}
      </>
    );
  }
  // The box to write in comes first, under the heading: the timeline below
  // it can run long, and the newest entries sit at its top.
  const card = variant === 'card';
  return (
    <section
      ref={sectionRef}
      aria-label="History"
      data-help-key={helpKey}
      data-help-pane={helpKey}
      data-history={variant}
      className={card ? 'card artifact-history-section' : 'artifact-history-section'}
      style={card ? { margin: 0, scrollMarginTop: 8 } : { marginTop: 16, paddingTop: 12, borderTop: '1px solid var(--border)', scrollMarginTop: 8 }}
    >
      <h3 style={{ margin: '0 0 8px', fontSize: 14 }}>History</h3>
      {filterBar}
      {box}
      {list}
    </section>
  );
};

export interface HistoryEntryProps {
  /** Recorded by the system, not written by a person. */
  auto: boolean;
  author: string;
  /** An ISO time. */
  when: string;
  /** What kind of change it was, where the feed says: a task's "moved", "status". */
  kind?: string;
  /** Drawn before the author: an artifact comment's unread mark. */
  mark?: React.ReactNode;
  /** Replaces the tint: a task's agent and error entries have their own. */
  tone?: React.CSSProperties;
  /** Under the text: a comment's to-do. */
  footer?: React.ReactNode;
  children: React.ReactNode;
}

export const HistoryEntry: React.FC<HistoryEntryProps> = ({ auto, author, when, kind, mark, tone, footer, children }) => (
  <div data-history-entry={auto ? 'change' : 'comment'} style={{ marginBottom: '12px', padding: '8px', backgroundColor: auto ? 'var(--tint-blue)' : 'var(--tint-yellow)', border: '1px solid var(--border)', borderRadius: '4px', ...tone }}>
    <div style={{ fontSize: '11px', color: 'var(--text-body)', marginBottom: '4px' }}>
      {mark}
      {auto ? '🔄' : '✏️'} <span style={{ fontWeight: 'bold' }}>{author}</span>
      {kind ? ` · ${kind}` : ''} · {new Date(when).toLocaleString()}
    </div>
    <div style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', lineHeight: '1.4', color: 'var(--text)' }}>{children}</div>
    {footer}
  </div>
);

/** The line a filtered feed with nothing in it shows. */
export const HistoryEmpty: React.FC<{ children: React.ReactNode }> = ({ children }) => <p style={{ fontSize: '12px', color: 'var(--text-muted)', lineHeight: 1.5 }}>{children}</p>;

import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ChatterEntry, chatterAPI } from '../api/client';
import { useFeature } from '../hooks/useFeature';
import { SegmentedControl } from './ui/SegmentedControl';
import { AddTodoControl, NoteTodoChip } from './NoteTodo';
import { NoteText } from './NoteText';
import { NOTE_TAGGING_FEATURE } from './noteTagging';
import { NoteComposer, postNote } from './NoteComposer';
import { TODO_LIST_FEATURE } from '../features';

// An artifact's history: what OpenV recorded each time it changed (edits,
// status moves, links, figures, results) and what people wrote about it, in
// one timeline, with the box to add a comment. It was the Notes panel's
// History tab; with the floating assistant (#725, #726) it is a section of
// the artifact's own pane, and the Notes panel keeps using it while that
// gate is closed.

// The feed has always carried two kinds of entry: what the system recorded
// when the artifact changed (is_auto_entry) and what a person wrote. The
// filter is that same split, so it needs nothing the feed does not already
// say.
type HistoryFilter = 'all' | 'changes' | 'comments';

// Comments first, and the default: people come here to talk to each other,
// and the recorded changes are the backdrop to that rather than the reason
// to open it.
const HISTORY_FILTERS: { value: HistoryFilter; label: string; title: string }[] = [
  { value: 'comments', label: 'Comments', title: 'Only what people wrote' },
  { value: 'changes', label: 'Changes', title: 'Only what the system recorded' },
  { value: 'all', label: 'All', title: 'Changes and comments, newest first' },
];

// Empty is a normal state once the feed can be filtered, and a blank list
// reads as broken. Each filter says what would appear here.
const EMPTY_HISTORY: Record<HistoryFilter, string> = {
  all: 'Nothing yet. Edits to this artifact are recorded here, and your notes join them.',
  changes: 'No changes recorded yet. Editing this artifact, its links or its figures adds to this list.',
  comments: 'No comments yet. Add the first one below.',
};

interface ArtifactHistoryProps {
  artifactId: string;
  projectId?: string;
  /** Reload when the artifact's version changes: a change is an entry. */
  artifactVersion?: number;
  onReferenceClick?: (ref: string) => void;
  /**
   * `panel`: fills a column, the list scrolling between the filter and the
   * composer (the Notes panel). `section`: a section at the foot of the
   * artifact's pane, scrolling with it, that comes into view when the URL
   * asks for the notes (?notes=1).
   */
  variant?: 'panel' | 'section';
}

export const ArtifactHistory: React.FC<ArtifactHistoryProps> = ({
  artifactId,
  projectId,
  artifactVersion,
  onReferenceClick,
  variant = 'panel',
}) => {
  const [historyFilter, setHistoryFilter] = useState<HistoryFilter>('comments');
  const [entries, setEntries] = useState<ChatterEntry[]>([]);
  const [newMessage, setNewMessage] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');
  const taggingEnabled = useFeature(NOTE_TAGGING_FEATURE);
  const todosEnabled = useFeature(TODO_LIST_FEATURE);
  const section = variant === 'section';
  const sectionRef = useRef<HTMLElement>(null);
  // Read from the location itself, so the timeline needs no router around it.
  const wantsNotes =
    typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('notes') === '1';

  const loadChatterEntries = useCallback(async () => {
    setIsLoading(true);
    setError('');
    try {
      const response = await chatterAPI.list(artifactId);
      setEntries(response.data || []);
    } catch (err: any) {
      console.error('Failed to load chatter entries:', err);
      setError(`Failed to load chatter: ${err.message}`);
    } finally {
      setIsLoading(false);
    }
  }, [artifactId]);

  useEffect(() => {
    loadChatterEntries();
  }, [loadChatterEntries, artifactVersion]);

  // A link to a comment (?artifact=…&notes=1, from Home or the activity
  // log) opens the artifact with its history in view.
  useEffect(() => {
    if (!section || !wantsNotes) return;
    sectionRef.current?.scrollIntoView?.({ block: 'start', behavior: 'smooth' });
  }, [section, wantsNotes, artifactId]);

  const visibleEntries = useMemo(() => {
    if (historyFilter === 'all') return entries;
    const wantAuto = historyFilter === 'changes';
    return entries.filter((entry) => entry.is_auto_entry === wantAuto);
  }, [entries, historyFilter]);

  /**
   * Raise a to-do for each person the note named with "@@".
   *
   * Runs after the note is saved, and never undoes it: the note is what the
   * writer came to do, so a board that refuses the card is reported rather
   * than allowed to swallow the comment.
   */
  const handleAddMessage = async () => {
    if (!newMessage.trim()) return;
    const message = newMessage;
    setError('');
    try {
      // A to-do that could not be raised is reported, and the reload below is
      // skipped when it happens: loadChatterEntries clears the error banner on
      // the way in, so reloading anyway would wipe the very message the writer
      // needs to see.
      let todoFailed = false;
      const entry = await postNote({
        artifactId,
        message,
        projectId,
        taggingEnabled,
        onTodoError: (msg) => {
          todoFailed = true;
          setError(msg);
        },
      });
      setEntries([entry, ...entries]);
      setNewMessage('');
      // Writing a comment while reading the changes would file it somewhere
      // the writer cannot see, which reads as the button having failed.
      if (historyFilter === 'changes') setHistoryFilter('comments');
      // Reload so a note that raised a to-do shows the chip it just gained.
      if (!todoFailed && message.includes('@@')) await loadChatterEntries();
    } catch (err: any) {
      console.error('Failed to add chatter entry:', err);
      setError(`Failed to add message: ${err.message}`);
    }
  };

  const filterBar = (
    <div
      style={{
        padding: section ? '0 0 8px' : '8px 10px',
        borderBottom: section ? 'none' : '1px solid var(--neutral-soft)',
        backgroundColor: section ? undefined : 'var(--surface-alt)',
      }}
    >
      <SegmentedControl
        aria-label="Filter history"
        options={HISTORY_FILTERS}
        value={historyFilter}
        onChange={setHistoryFilter}
        style={{ width: '100%' }}
      />
    </div>
  );

  const list = (
    <div style={section ? { padding: '4px 0' } : { flex: 1, overflowY: 'auto', padding: '10px' }}>
      {isLoading && <p style={{ fontSize: '12px', color: 'var(--text-body)' }}>Loading notes...</p>}
      {error && <p style={{ fontSize: '12px', color: 'var(--danger)' }}>{error}</p>}
      {!isLoading && !error && visibleEntries.length === 0 && (
        <p style={{ fontSize: '12px', color: 'var(--text-muted)', lineHeight: 1.5 }}>
          {EMPTY_HISTORY[historyFilter]}
        </p>
      )}
      {visibleEntries.map((entry) => (
        <div
          key={entry.id}
          style={{
            marginBottom: '12px',
            padding: '8px',
            backgroundColor: entry.is_auto_entry ? 'var(--tint-blue)' : 'var(--tint-yellow)',
            border: '1px solid var(--border)',
            borderRadius: '4px',
          }}
        >
          <div style={{ fontSize: '11px', color: 'var(--text-body)', marginBottom: '4px' }}>
            {entry.is_auto_entry ? '🔄' : '✏️'}{' '}
            <span style={{ fontWeight: 'bold' }}>
              {entry.author_name || (entry.is_auto_entry ? 'System' : 'Unknown')}
            </span>{' '}
            · {new Date(entry.created_at).toLocaleString()}
          </div>
          <div style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', lineHeight: '1.4', color: 'var(--text)' }}>
            <NoteText text={entry.message} onReferenceClick={onReferenceClick} />
          </div>
          {/* A note carries at most one to-do: once it has one it shows its
              live status, and until then it offers to raise one for whoever
              the note names. System and agent entries are not somebody asking
              for something, so they get neither. */}
          {todosEnabled && projectId && !entry.is_auto_entry && (
            entry.todo ? (
              <NoteTodoChip projectId={projectId} todo={entry.todo} />
            ) : (
              <AddTodoControl projectId={projectId} entry={entry} onCreated={loadChatterEntries} />
            )
          )}
        </div>
      ))}
    </div>
  );

  const composer = (
    <div
      style={{
        padding: section ? '0 0 12px' : '10px',
        borderTop: section ? 'none' : '1px solid var(--neutral-soft)',
        backgroundColor: section ? undefined : 'var(--surface-alt)',
      }}
    >
      <NoteComposer
        projectId={projectId}
        artifactId={artifactId}
        value={newMessage}
        onChange={setNewMessage}
        onSubmit={handleAddMessage}
        ariaLabel="Add a note"
      />
      <button
        onClick={handleAddMessage}
        disabled={!newMessage.trim()}
        title="Add note (Ctrl+Enter)"
        style={{
          width: section ? 'auto' : '100%',
          marginTop: '8px',
          padding: section ? '8px 16px' : '8px',
          backgroundColor: newMessage.trim() ? 'var(--accent)' : 'var(--neutral-mid)',
          color: 'var(--accent-fg)',
          border: 'none',
          borderRadius: '4px',
          cursor: newMessage.trim() ? 'pointer' : 'not-allowed',
          fontSize: '12px',
          fontWeight: 'bold',
        }}
      >
        Add Note
      </button>
    </div>
  );

  if (!section) {
    return (
      <>
        {filterBar}
        {list}
        {composer}
      </>
    );
  }

  // In the pane the box to write in comes first, under the heading: the
  // timeline below it can run long, and the newest entries sit at its top.
  return (
    <section
      ref={sectionRef}
      aria-label="History"
      data-help-key="requirements.history"
      data-help-pane="requirements.history"
      className="artifact-history-section"
      style={{ marginTop: 16, paddingTop: 12, borderTop: '1px solid var(--border)', scrollMarginTop: 8 }}
    >
      <h3 style={{ margin: '0 0 8px', fontSize: 14 }}>History</h3>
      {filterBar}
      {composer}
      {list}
    </section>
  );
};

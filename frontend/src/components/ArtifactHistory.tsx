import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ChatterEntry, chatterAPI } from '../api/client';
import { useFeature } from '../hooks/useFeature';
import { HistoryEmpty, HistoryEntry, HistoryShell, filterHistory, type HistoryFilter, type HistoryVariant } from './NoteFeed';
import { AddTodoControl, NoteTodoChip } from './NoteTodo';
import { NoteText } from './NoteText';
import { NOTE_TAGGING_FEATURE } from './noteTagging';
import { NoteComposer, postNote } from './NoteComposer';
import { TODO_LIST_FEATURE } from '../features';
import { useAppStore } from '../state/store';
import { COMMENT_READS_FEATURE, useNoteReads } from './noteReadState';
import { NoteReadMark } from './NoteReadControls';

// An artifact's history: what OpenV recorded each time it changed (edits,
// status moves, links, figures, results) and what people wrote about it, in
// one timeline, with the box to add a comment. It was the Notes panel's
// History tab; with the floating assistant (#725, #726) it is a section of
// the artifact's own pane, and the Notes panel keeps using it while that
// gate is closed.

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
  /** Where it is drawn (NoteFeed): a column, a section of the artifact's pane, or a card of the detail view. */
  variant?: HistoryVariant;
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
  const section = variant !== 'panel';
  const sectionRef = useRef<HTMLElement>(null);
  // Read from the location itself, so the timeline needs no router around it.
  const wantsNotes =
    typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('notes') === '1';
  // Comments read here leave Needs you (comment-reads): in the panel at once,
  // in the section when it is asked for or scrolled into view.
  const reads = useNoteReads({
    artifactId,
    entries,
    enabled: useFeature(COMMENT_READS_FEATURE),
    userId: useAppStore((s) => s.currentUser?.id || null),
    seenNow: !section || wantsNotes,
    observe: sectionRef,
  });

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

  const visibleEntries = useMemo(() => filterHistory(entries, historyFilter, (e) => e.is_auto_entry), [entries, historyFilter]);

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

  const list = (
    <>
      {isLoading && <p style={{ fontSize: '12px', color: 'var(--text-body)' }}>Loading notes...</p>}
      {error && <p style={{ fontSize: '12px', color: 'var(--danger)' }}>{error}</p>}
      {!isLoading && !error && visibleEntries.length === 0 && <HistoryEmpty>{EMPTY_HISTORY[historyFilter]}</HistoryEmpty>}
      {visibleEntries.map((entry) => (
        <HistoryEntry
          key={entry.id}
          auto={entry.is_auto_entry}
          author={entry.author_name || (entry.is_auto_entry ? 'System' : 'Unknown')}
          when={entry.created_at}
          mark={<NoteReadMark entry={entry} reads={reads} />}
          // A note carries at most one to-do: once it has one it shows its
          // live status, and until then it offers to raise one for whoever
          // the note names. System and agent entries are not somebody asking
          // for something, so they get neither.
          footer={
            todosEnabled && projectId && !entry.is_auto_entry ? (
              entry.todo ? (
                <NoteTodoChip projectId={projectId} todo={entry.todo} />
              ) : (
                <AddTodoControl projectId={projectId} entry={entry} onCreated={loadChatterEntries} />
              )
            ) : undefined
          }
        >
          <NoteText text={entry.message} onReferenceClick={onReferenceClick} />
        </HistoryEntry>
      ))}
    </>
  );

  const composer = (
    <>
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
    </>
  );

  return (
    <HistoryShell variant={variant} filter={historyFilter} onFilter={setHistoryFilter} composer={composer} sectionRef={sectionRef} helpKey="requirements.history">
      {list}
    </HistoryShell>
  );
};

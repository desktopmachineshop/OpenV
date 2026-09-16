import React, { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import {
  Artifact,
  Attachment,
  ChatterEntry,
  Link,
  ProjectMember,
  artifactAPI,
  attachmentAPI,
  chatterAPI,
  linkAPI,
  membersAPI,
} from '../api/client';
import { GuidedChatPanel } from './wizard/GuidedChatPanel';
import { resolveAssistantSessionId } from './wizard/assistantSession';
import {
  ASSISTANT_EDITS_FEATURE,
  GATED_REASON,
  applySuggestionsToProject,
  isProjectEditKind,
} from './wizard/applySuggestion';
import { useFeature } from '../hooks/useFeature';
import { SegmentedControl } from './ui/SegmentedControl';
import { AddTodoControl, NoteTodoChip, createNoteTodo } from './NoteTodo';
import { TokenMenu } from './ui/TokenMenu';
import { NoteText } from './NoteText';
import { NOTE_TAGGING_FEATURE } from './noteTagging';
import {
  MentionCandidate,
  MentionQuery,
  activeMentionQuery,
  applyMention,
  matchMentions,
  mentionCandidates,
  todoTargets,
} from './noteMentions';
import {
  ReferenceCandidate,
  ReferenceQuery,
  activeReferenceQuery,
  applyReference,
  matchReferences,
  referenceCandidates,
} from './artifactReferences';
import { TODO_LIST_FEATURE } from '../views/TodoList';

interface ChatterPanelProps {
  /** The artifact whose notes these are; absent when nothing is selected. */
  artifactId?: string;
  /** Project the notes belong to — the assistant tab needs it with or without an artifact. */
  projectId?: string;
  isOpen: boolean;
  /** Cycle the panel's visibility: pinned → auto-hide → hidden. */
  onToggle: () => void;
  /** The panel's current visibility, for the control's label. */
  modeLabel?: string;
  /** What clicking the control would switch to. */
  nextModeLabel?: string;
  /**
   * Refresh the project after the assistant adds something. Without it an
   * applied suggestion is saved but invisible until the reader navigates,
   * which reads as the button having done nothing.
   */
  onArtifactsChanged?: () => void;
  /**
   * Follow a "#REQ-12" a note cites. Without it the citations still render as
   * marked text rather than dead links.
   */
  onReferenceClick?: (ref: string) => void;
}

type Tab = 'history' | 'assistant';

// The feed has always carried two kinds of entry: what the system recorded
// when the artifact changed (is_auto_entry) and what a person wrote. The
// filter is that same split, so it needs nothing the feed does not already
// say.
type HistoryFilter = 'all' | 'changes' | 'comments';

// Comments first, and the default: the panel is where people talk to each
// other, and the recorded changes are the backdrop to that rather than the
// reason to open it.
const HISTORY_FILTERS: { value: HistoryFilter; label: string; title: string }[] = [
  { value: 'comments', label: 'Comments', title: 'Only what people wrote' },
  { value: 'changes', label: 'Changes', title: 'Only what the system recorded' },
  { value: 'all', label: 'All', title: 'Changes and comments, newest first' },
];

// Empty is a normal state once the feed can be filtered, and a blank panel
// reads as broken. Each filter says what would appear here.
const EMPTY_HISTORY: Record<HistoryFilter, string> = {
  all: 'Nothing yet. Edits to this artifact are recorded here, and your notes join them.',
  changes: 'No changes recorded yet. Editing this artifact, its links or its figures adds to this list.',
  comments: 'No comments yet. Add the first one below.',
};

export const ChatterPanel: React.FC<ChatterPanelProps> = ({
  artifactId,
  projectId,
  isOpen,
  onToggle,
  modeLabel,
  nextModeLabel,
  onArtifactsChanged,
  onReferenceClick,
}) => {
  // The history belongs to an artifact; the assistant does not, so with
  // nothing selected the panel opens on the tab that still has something to
  // show.
  const [tab, setTab] = useState<Tab>(artifactId ? 'history' : 'assistant');
  const [historyFilter, setHistoryFilter] = useState<HistoryFilter>('comments');
  // Resolved lazily and only for the assistant tab: finding the conversation
  // can create a guided session, which should not happen just because someone
  // opened an artifact.
  const [assistantSessionId, setAssistantSessionId] = useState('');
  const [assistantError, setAssistantError] = useState('');
  const [entries, setEntries] = useState<ChatterEntry[]>([]);
  const [newMessage, setNewMessage] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');
  // Which suggestions this panel has applied. The wizard persists its own in
  // the session answers; here the project is the record, so remembering
  // for the life of the panel is what stops a second click adding twice.
  const [applied, setApplied] = useState<Record<string, boolean>>({});
  const taggingEnabled = useFeature(NOTE_TAGGING_FEATURE);
  // What the composer's menus offer. Fetched on the first "@" or "#" rather
  // than when the panel opens: most notes are prose, and nobody should pay
  // four requests for a menu they never summon.
  const composerRef = useRef<HTMLTextAreaElement>(null);
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [projectArtifacts, setProjectArtifacts] = useState<Artifact[]>([]);
  const [artifactLinks, setArtifactLinks] = useState<Link[]>([]);
  const [projectAttachments, setProjectAttachments] = useState<Attachment[]>([]);
  const [contextLoaded, setContextLoaded] = useState(false);
  const [mentionQuery, setMentionQuery] = useState<MentionQuery | null>(null);
  const [refQuery, setRefQuery] = useState<ReferenceQuery | null>(null);
  const [highlight, setHighlight] = useState(0);
  const editsEnabled = useFeature(ASSISTANT_EDITS_FEATURE);
  const todosEnabled = useFeature(TODO_LIST_FEATURE);

  const loadChatterEntries = useCallback(async () => {
    if (!artifactId) return;

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
    if (artifactId && isOpen) {
      loadChatterEntries();
    }
  }, [artifactId, isOpen, loadChatterEntries]);

  // With no artifact there is no history to show, so keep the panel on the
  // assistant rather than an empty tab.
  useEffect(() => {
    if (!artifactId) setTab('assistant');
  }, [artifactId]);

  useEffect(() => {
    if (!isOpen || tab !== 'assistant' || !projectId || assistantSessionId) return;
    let cancelled = false;
    (async () => {
      const id = await resolveAssistantSessionId(projectId);
      if (cancelled) return;
      if (id) {
        setAssistantSessionId(id);
        setAssistantError('');
      } else {
        setAssistantError('The assistant conversation could not be opened. Try again in a moment.');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [isOpen, tab, projectId, assistantSessionId]);

  /**
   * Add what the assistant suggested to the project.
   *
   * The wizard applies a suggestion into the form somebody is filling in;
   * here there is no form, so it goes straight into the project as a draft
   * artifact under the heading the wizard would have used. The project's
   * artifacts are read at apply time rather than held in state: a heading
   * created a minute ago by somebody else should be found, not duplicated.
   */
  const applySuggestions = useCallback(
    async (items: { suggestion: any; key: string }[]): Promise<(string | null)[]> => {
      if (!projectId) return items.map(() => 'No project is open.');
      let artifacts: Artifact[] = [];
      try {
        const res = await artifactAPI.list(projectId);
        artifacts = res.data || [];
      } catch {
        // Without the list, headings cannot be matched and would be created
        // again. Refusing is better than quietly growing a second set.
        return items.map(() => 'The project could not be read, so nothing was added.');
      }
      // A gated kind is answered, not applied: the server does not offer
      // these shapes to a gated workspace, but a transcript can carry one
      // from before the gate closed, or from another member's channel.
      const allowed = editsEnabled ? items : items.filter((i) => !isProjectEditKind(i.suggestion?.kind));
      const applyResults = await applySuggestionsToProject(
        { projectId, artifacts, onChanged: onArtifactsChanged },
        allowed
      );
      const byKey = new Map(allowed.map((i, n) => [i.key, applyResults[n]]));
      const results = items.map((i) => (byKey.has(i.key) ? byKey.get(i.key)! : GATED_REASON));
      setApplied((prev) => {
        const next = { ...prev };
        items.forEach((i, n) => {
          if (results[n] === null) next[i.key] = true;
        });
        return next;
      });
      return results;
    },
    [projectId, onArtifactsChanged, editsEnabled]
  );

  // One fetch, the first time a menu is summoned. A failure leaves the menus
  // empty rather than breaking the composer: the note is the point, and it can
  // still be typed and posted with the tokens written by hand.
  const loadComposerContext = useCallback(async () => {
    if (contextLoaded || !projectId || !artifactId) return;
    setContextLoaded(true);
    const settle = <T,>(p: Promise<{ data: T[] }>): Promise<T[]> =>
      p.then((r) => r.data || []).catch(() => [] as T[]);
    const [mem, arts, links, atts] = await Promise.all([
      settle<ProjectMember>(membersAPI.list(projectId)),
      settle<Artifact>(artifactAPI.list(projectId)),
      settle<Link>(linkAPI.listForArtifact(artifactId)),
      settle<Attachment>(attachmentAPI.listByProject(projectId)),
    ]);
    setMembers(mem);
    setProjectArtifacts(arts);
    setArtifactLinks(links);
    setProjectAttachments(atts);
  }, [contextLoaded, projectId, artifactId]);

  const people = useMemo(() => mentionCandidates(members), [members]);
  const currentArtifact = useMemo(
    () => projectArtifacts.find((a) => a.id === artifactId) || (artifactId ? { id: artifactId } : undefined),
    [projectArtifacts, artifactId]
  );
  const localRefs = useMemo(
    () => referenceCandidates(currentArtifact, artifactLinks, projectArtifacts, projectAttachments),
    [currentArtifact, artifactLinks, projectArtifacts, projectAttachments]
  );
  const projectRefs = useMemo(
    () =>
      referenceCandidates(
        currentArtifact,
        artifactLinks,
        projectArtifacts,
        projectAttachments,
        'project'
      ),
    [currentArtifact, artifactLinks, projectArtifacts, projectAttachments]
  );

  const mentionMatches = useMemo(
    () => (mentionQuery ? matchMentions(people, mentionQuery.query).slice(0, 8) : []),
    [people, mentionQuery]
  );
  const refMatches = useMemo(
    () =>
      refQuery
        ? matchReferences(refQuery.scope === 'project' ? projectRefs : localRefs, refQuery.query).slice(0, 8)
        : [],
    [localRefs, projectRefs, refQuery]
  );

  // Recompute from the caret after every keystroke or cursor move. Only one
  // menu can be open: the caret is inside at most one token.
  const syncMenus = () => {
    if (!taggingEnabled) return;
    const el = composerRef.current;
    if (!el) return;
    const caret = el.selectionStart ?? 0;
    const mention = activeMentionQuery(el.value, caret);
    const reference = mention ? null : activeReferenceQuery(el.value, caret);
    setMentionQuery(mention);
    setRefQuery(reference);
    setHighlight(0);
    if (mention || reference) void loadComposerContext();
  };

  const insertToken = (next: { text: string; caret: number }) => {
    setNewMessage(next.text);
    setMentionQuery(null);
    setRefQuery(null);
    // The value lands via React, so the caret is restored once it has.
    requestAnimationFrame(() => {
      const el = composerRef.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(next.caret, next.caret);
    });
  };

  const chooseMention = (candidate: MentionCandidate) => {
    const el = composerRef.current;
    if (!el || !mentionQuery) return;
    insertToken(applyMention(el.value, mentionQuery, el.selectionStart ?? 0, candidate.handle));
  };

  const chooseReference = (candidate: ReferenceCandidate) => {
    const el = composerRef.current;
    if (!el || !refQuery) return;
    insertToken(applyReference(el.value, refQuery, el.selectionStart ?? 0, candidate.ref));
  };

  const menuOpen = (mentionQuery && mentionMatches.length > 0) || (refQuery && refMatches.length > 0);

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
  const raiseTodosFor = async (entry: ChatterEntry, message: string) => {
    if (!taggingEnabled || !projectId || !message.includes('@@')) return;
    // The menus may never have been summoned — someone can type "@@dana" by
    // hand — so the people are resolved here rather than assumed loaded.
    let candidates = people;
    if (candidates.length === 0) {
      const loaded = await membersAPI.list(projectId).catch(() => null);
      candidates = mentionCandidates(loaded?.data || []);
    }
    const targets = todoTargets(message, candidates);
    if (targets.length === 0) return;
    try {
      for (const target of targets) {
        await createNoteTodo(projectId, entry, { assigneeId: target.userId });
      }
      // Reload so each note shows the to-do chip it just gained.
      await loadChatterEntries();
    } catch (err: any) {
      setError(`The note was posted, but its to-do could not be raised: ${err.message}`);
    }
  };

  const handleAddMessage = async () => {
    if (!newMessage.trim() || !artifactId) {
      return;
    }

    const message = newMessage;
    setError('');
    try {
      const response = await chatterAPI.create({
        artifact_id: artifactId,
        message,
      });

      // Add the new entry to the top of the list
      setEntries([response.data, ...entries]);
      setNewMessage('');
      setMentionQuery(null);
      setRefQuery(null);
      // Writing a comment while reading the changes would file it somewhere
      // the writer cannot see, which reads as the button having failed.
      if (historyFilter === 'changes') setHistoryFilter('comments');
      await raiseTodosFor(response.data, message);
    } catch (err: any) {
      console.error('Failed to add chatter entry:', err);
      setError(`Failed to add message: ${err.message}`);
    }
  };

  if (!isOpen) {
    return null;
  }

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        width: '100%',
        backgroundColor: 'var(--surface)',
        border: '1px solid var(--border)',
        borderRadius: '4px',
        height: '100%',
        boxShadow: '0 2px 4px rgba(0,0,0,0.05)',
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: '15px',
          borderBottom: '1px solid var(--neutral-soft)',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          backgroundColor: 'var(--surface-alt)',
        }}
      >
        <h3 style={{ margin: 0, fontSize: '14px', fontWeight: 'bold' }}>
          💬 Notes
        </h3>
        {/* One control for how much room the panel takes, rather than a
            close button that gave no way back. */}
        <button
          onClick={onToggle}
          style={{
            background: 'none',
            border: '1px solid var(--border)',
            borderRadius: 4,
            cursor: 'pointer',
            fontSize: 12,
            padding: '3px 8px',
            minHeight: 24,
            color: 'var(--text-muted)',
          }}
          title={
            modeLabel && nextModeLabel
              ? `Notes: ${modeLabel} — click for ${nextModeLabel}`
              : 'Hide notes'
          }
        >
          {modeLabel || 'Hide'}
        </button>
      </div>

      {/* Tabs: the artifact's own history, and the project's assistant. */}
      <div style={{ display: 'flex', borderBottom: '1px solid var(--neutral-soft)' }}>
        {([
          { id: 'history' as Tab, label: 'History' },
          { id: 'assistant' as Tab, label: 'V&V Assistant' },
        ]).map((t) => {
          const active = tab === t.id;
          const disabled = t.id === 'history' && !artifactId;
          return (
            <button
              key={t.id}
              onClick={() => !disabled && setTab(t.id)}
              disabled={disabled}
              title={disabled ? 'Select an artifact to read its history and add comments' : undefined}
              style={{
                flex: 1,
                padding: '8px 6px',
                background: 'none',
                border: 'none',
                borderBottom: active ? '2px solid var(--accent)' : '2px solid transparent',
                color: disabled ? 'var(--neutral)' : active ? 'var(--text)' : 'var(--text-muted)',
                fontWeight: active ? 700 : 400,
                fontSize: 12,
                cursor: disabled ? 'not-allowed' : 'pointer',
              }}
            >
              {t.label}
            </button>
          );
        })}
      </div>

      {tab === 'history' ? (
        <>
      {/* What to show: everything, only recorded changes, or only what
          people wrote. */}
      <div
        style={{
          padding: '8px 10px',
          borderBottom: '1px solid var(--neutral-soft)',
          backgroundColor: 'var(--surface-alt)',
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

      {/* Entries list */}
      <div
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: '10px',
        }}
      >
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
            <div
              style={{
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                lineHeight: '1.4',
                color: 'var(--text)',
              }}
            >
              <NoteText text={entry.message} onReferenceClick={onReferenceClick} />
            </div>
            {/* A note carries at most one to-do: once it has one it shows
                its live status, and until then it offers to raise one for
                whoever the note names. System and agent entries are not
                somebody asking for something, so they get neither. */}
            {todosEnabled && projectId && !entry.is_auto_entry && (
              entry.todo ? (
                <NoteTodoChip projectId={projectId} todo={entry.todo} />
              ) : (
                <AddTodoControl
                  projectId={projectId}
                  entry={entry}
                  onCreated={loadChatterEntries}
                />
              )
            )}
          </div>
        ))}
      </div>

      {/* Input form */}
      <div
        style={{
          padding: '10px',
          borderTop: '1px solid var(--neutral-soft)',
          backgroundColor: 'var(--surface-alt)',
        }}
      >
        <div style={{ position: 'relative' }}>
        <textarea
          ref={composerRef}
          value={newMessage}
          onChange={(e) => {
            setNewMessage(e.target.value);
            syncMenus();
          }}
          onClick={syncMenus}
          onKeyUp={(e) => {
            // Arrow keys walk the menu when one is open; otherwise they move
            // the caret, which can move into or out of a token.
            if (menuOpen && ['ArrowUp', 'ArrowDown'].includes(e.key)) return;
            syncMenus();
          }}
          onBlur={() => {
            setMentionQuery(null);
            setRefQuery(null);
          }}
          onKeyDown={(e) => {
            if (menuOpen) {
              const length = mentionQuery ? mentionMatches.length : refMatches.length;
              if (e.key === 'ArrowDown') {
                e.preventDefault();
                setHighlight((i) => (i + 1) % length);
                return;
              }
              if (e.key === 'ArrowUp') {
                e.preventDefault();
                setHighlight((i) => (i - 1 + length) % length);
                return;
              }
              if (e.key === 'Enter' || e.key === 'Tab') {
                e.preventDefault();
                const i = Math.min(highlight, length - 1);
                if (mentionQuery) chooseMention(mentionMatches[i]);
                else chooseReference(refMatches[i]);
                return;
              }
              if (e.key === 'Escape') {
                e.preventDefault();
                setMentionQuery(null);
                setRefQuery(null);
                return;
              }
            }
            if (e.key === 'Enter' && e.ctrlKey) {
              e.preventDefault();
              handleAddMessage();
            }
          }}
          placeholder={
            taggingEnabled ? 'Add a note… @name, @@name for a to-do, #REQ-12' : 'Add a note...'
          }
          enterKeyHint="enter"
          style={{
            width: '100%',
            minHeight: '60px',
            padding: '8px',
            border: '1px solid var(--neutral-mid)',
            borderRadius: '4px',
            fontFamily: 'inherit',
            fontSize: '12px',
            resize: 'vertical',
            boxSizing: 'border-box',
          }}
        />
        {mentionQuery && (
          <TokenMenu
            aria-label="People suggestions"
            rows={mentionMatches.map((c) => ({
              key: c.userId,
              primary: `${mentionQuery.scope === 'todo' ? '@@' : '@'}${c.handle}`,
              secondary:
                mentionQuery.scope === 'todo' ? `${c.label} · raises a to-do` : c.label,
            }))}
            highlight={highlight}
            onHighlight={setHighlight}
            onChoose={(i) => chooseMention(mentionMatches[i])}
          />
        )}
        {refQuery && (
          <TokenMenu
            aria-label="Reference suggestions"
            rows={refMatches.map((c) => ({
              key: c.ref,
              primary: c.ref,
              secondary:
                `${c.kind === 'figure' ? 'figure' : c.relation || 'artifact'} · ${c.label}` +
                (c.owner ? ` · on ${c.owner}` : ''),
            }))}
            highlight={highlight}
            onHighlight={setHighlight}
            onChoose={(i) => chooseReference(refMatches[i])}
          />
        )}
        </div>
        <button
          onClick={handleAddMessage}
          disabled={!newMessage.trim()}
          title="Add note (Ctrl+Enter)"
          style={{
            width: '100%',
            marginTop: '8px',
            padding: '8px',
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
        </>
      ) : (
        // One conversation per project: the same transcript the wizard shows,
        // with the artifact on screen passed as this turn's context.
        <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
          {assistantError ? (
            <div style={{ padding: 12, fontSize: 12, color: 'var(--danger)' }}>{assistantError}</div>
          ) : !projectId ? (
            <div style={{ padding: 12, fontSize: 12, color: 'var(--text-muted)' }}>
              Open a project to chat with the V&amp;V Assistant.
            </div>
          ) : !assistantSessionId ? (
            <div style={{ padding: 12, fontSize: 12, color: 'var(--text-muted)' }}>
              Opening the conversation…
            </div>
          ) : (
            <GuidedChatPanel
              sessionId={assistantSessionId}
              artifactId={artifactId}
              embedded
              applied={applied}
              applyTarget="project"
              onApplySuggestions={applySuggestions}
              subtitle={
                artifactId
                  ? 'Answers about the artifact on screen — same conversation as the wizard.'
                  : 'Same conversation as the guided wizard, for the project as a whole.'
              }
            />
          )}
        </div>
      )}
    </div>
  );
};

export default ChatterPanel;

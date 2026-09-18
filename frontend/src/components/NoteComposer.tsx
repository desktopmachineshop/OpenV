import React, { useCallback, useMemo, useRef, useState } from 'react';
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
import { TokenMenu } from './ui/TokenMenu';
import { useFeature } from '../hooks/useFeature';
import { NOTE_TAGGING_FEATURE } from './noteTagging';
import { createNoteTodo } from './NoteTodo';
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

/**
 * The note-writing surface, shared by every place a person writes a note.
 *
 * It exists because a note is a note wherever it is written: the notes panel
 * and the review queue's rejection dialog must both offer "@" for people,
 * "@@" to raise a to-do and "#" / "##" to cite, and must both produce an entry
 * the panel, the mention notifications and the to-do board all recognise.
 * Two implementations of that would drift, and the second one would be the
 * one that quietly lacked mentions.
 */

interface NoteComposerProps {
  /** Project the note belongs to; the menus are scoped to it. */
  projectId?: string;
  /** Artifact the note is about — its links decide what "#" offers. */
  artifactId?: string;
  value: string;
  onChange: (value: string) => void;
  /** Ctrl+Enter, when the caller has somewhere to submit to. */
  onSubmit?: () => void;
  placeholder?: string;
  minHeight?: number;
  autoFocus?: boolean;
  /** Labels the textarea for assistive tech and for tests. */
  ariaLabel?: string;
}

export const NoteComposer: React.FC<NoteComposerProps> = ({
  projectId,
  artifactId,
  value,
  onChange,
  onSubmit,
  placeholder,
  minHeight = 60,
  autoFocus,
  ariaLabel,
}) => {
  const taggingEnabled = useFeature(NOTE_TAGGING_FEATURE);
  const composerRef = useRef<HTMLTextAreaElement>(null);

  // What the menus offer. Fetched on the first "@" or "#" rather than on
  // mount: most notes are prose, and nobody should pay four requests for a
  // menu they never summon.
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [projectArtifacts, setProjectArtifacts] = useState<Artifact[]>([]);
  const [artifactLinks, setArtifactLinks] = useState<Link[]>([]);
  const [projectAttachments, setProjectAttachments] = useState<Attachment[]>([]);
  const [contextLoaded, setContextLoaded] = useState(false);
  const [mentionQuery, setMentionQuery] = useState<MentionQuery | null>(null);
  const [refQuery, setRefQuery] = useState<ReferenceQuery | null>(null);
  const [highlight, setHighlight] = useState(0);

  const loadContext = useCallback(async () => {
    if (contextLoaded || !projectId) return;
    setContextLoaded(true);
    // One slow or forbidden list must not cost the others their menu.
    const settle = async <T,>(p: Promise<{ data: T[] }>): Promise<T[]> =>
      p.then((r) => r.data || []).catch(() => []);
    const [mem, arts, links, atts] = await Promise.all([
      settle<ProjectMember>(membersAPI.list(projectId)),
      settle<Artifact>(artifactAPI.list(projectId)),
      artifactId ? settle<Link>(linkAPI.listForArtifact(artifactId)) : Promise.resolve([]),
      settle<Attachment>(attachmentAPI.listByProject(projectId)),
    ]);
    setMembers(mem);
    setProjectArtifacts(arts);
    setArtifactLinks(links);
    setProjectAttachments(atts);
  }, [contextLoaded, projectId, artifactId]);

  const people = useMemo(() => mentionCandidates(members), [members]);
  const currentArtifact = useMemo(
    () =>
      projectArtifacts.find((a) => a.id === artifactId) ||
      (artifactId ? ({ id: artifactId } as Artifact) : undefined),
    [projectArtifacts, artifactId]
  );
  const localRefs = useMemo(
    () => referenceCandidates(currentArtifact, artifactLinks, projectArtifacts, projectAttachments),
    [currentArtifact, artifactLinks, projectArtifacts, projectAttachments]
  );
  const projectRefs = useMemo(
    () =>
      referenceCandidates(currentArtifact, artifactLinks, projectArtifacts, projectAttachments, 'project'),
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
    if (mention || reference) void loadContext();
  };

  const insertToken = (next: { text: string; caret: number }) => {
    onChange(next.text);
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

  const menuOpen = Boolean(
    (mentionQuery && mentionMatches.length > 0) || (refQuery && refMatches.length > 0)
  );

  return (
    <div style={{ position: 'relative' }}>
      <textarea
        ref={composerRef}
        value={value}
        aria-label={ariaLabel}
        autoFocus={autoFocus}
        onChange={(e) => {
          onChange(e.target.value);
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
          if (e.key === 'Enter' && e.ctrlKey && onSubmit) {
            e.preventDefault();
            onSubmit();
          }
        }}
        placeholder={
          placeholder ??
          (taggingEnabled ? 'Add a note… @name, @@name for a to-do, #REQ-12' : 'Add a note...')
        }
        enterKeyHint="enter"
        style={{
          width: '100%',
          minHeight,
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
            secondary: mentionQuery.scope === 'todo' ? `${c.label} · raises a to-do` : c.label,
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
  );
};

/**
 * Post a note and raise a to-do for each person it named with "@@".
 *
 * This is the write half of "a note is a note wherever it is written": the
 * server resolves "@" mentions into notifications from the entry itself, and
 * the "@@" to-dos are raised here, so anywhere that calls this gets both.
 *
 * The to-do pass runs after the note is saved and never undoes it — the note
 * is what the writer came to do, so a board that refuses the card is reported
 * through `onTodoError` rather than allowed to swallow the comment.
 */
export const postNote = async (opts: {
  artifactId: string;
  message: string;
  projectId?: string;
  /** False when the workspace has not received note tagging yet. */
  taggingEnabled?: boolean;
  onTodoError?: (message: string) => void;
}): Promise<ChatterEntry> => {
  const { artifactId, message, projectId, taggingEnabled = true, onTodoError } = opts;
  const response = await chatterAPI.create({ artifact_id: artifactId, message });
  const entry = response.data;

  if (!taggingEnabled || !projectId || !message.includes('@@')) return entry;
  try {
    // The menus may never have been summoned — someone can type "@@dana" by
    // hand — so the people are resolved here rather than assumed loaded.
    const loaded = await membersAPI.list(projectId).catch(() => null);
    const targets = todoTargets(message, mentionCandidates(loaded?.data || []));
    for (const target of targets) {
      await createNoteTodo(projectId, entry, { assigneeId: target.userId });
    }
  } catch (err: any) {
    onTodoError?.(`The note was posted, but its to-do could not be raised: ${err.message}`);
  }
  return entry;
};

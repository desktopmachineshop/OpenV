// Typing "@" in a note offers the people on the project, and inserts one as a
// mention the server will resolve.
//
// "@@" is the same menu with a second meaning: the note raises a to-do for
// whoever it names, as it is posted. The doubled marker is deliberate — an
// "@mention" is how people talk to each other, and turning every one of them
// into a card would fill the board with "thanks @dave". Doubling it is the
// writer saying they mean work, not conversation. Raising one afterwards from
// the note still works and is unchanged; this is the shortcut, not a
// replacement for it.
//
// The logic here is pure so it can be tested without a textarea.
import { ProjectMember } from '../api/client';

/** One person a note can name. */
export interface MentionCandidate {
  userId: string;
  /** The token written into the text, without its marker. */
  handle: string;
  /** What they are called, for the menu row. */
  label: string;
  /** Shown under the name to tell two people with one name apart. */
  email?: string;
}

/**
 * What a mention being typed means.
 *
 * "mention" is a single "@": name someone. "todo" is "@@": name someone and
 * raise a to-do for them from this note.
 */
export type MentionScope = 'mention' | 'todo';

/** The marker a scope is written with. */
export const mentionMarker = (scope: MentionScope): string => (scope === 'todo' ? '@@' : '@');

/**
 * Characters the server's mention pattern captures (`@([\w.-]+)` in
 * internal/domain/mentions). A handle with anything else in it would be cut
 * short when the note is read back, so it is never offered.
 */
const capturable = /^[\w.-]+$/;

/**
 * The tokens a member answers to, in the same order and by the same rules as
 * mentions.Handles on the server: their name with the spaces removed, their
 * first name, then the local part of their email.
 *
 * This list has to agree with the server's. A handle the menu inserts that
 * the server does not recognise is a mention that silently names nobody.
 */
export const mentionHandles = (name?: string, email?: string): string[] => {
  const handles: string[] = [];
  const trimmed = (name || '').trim();
  if (trimmed) {
    const lower = trimmed.toLowerCase();
    handles.push(lower.replace(/\s+/g, ''));
    const [first] = lower.split(/\s+/);
    if (first) handles.push(first);
  }
  const at = (email || '').indexOf('@');
  if (at > 0) handles.push((email as string).slice(0, at).toLowerCase());
  return handles;
};

/**
 * The handle to write for a member: the first of their handles the server's
 * pattern can capture whole.
 *
 * A name like "O'Brien" lowercases to a handle with an apostrophe in it, which
 * the pattern stops at — writing it would name nobody. Their email local part
 * usually can be captured, so it is used instead. Nothing capturable at all
 * means they cannot be mentioned, and they are left out of the menu.
 */
export const mentionHandle = (name?: string, email?: string): string =>
  mentionHandles(name, email).find((h) => capturable.test(h)) || '';

/**
 * The people a note may name. A member with no usable handle is left out
 * rather than offered and then silently dropped when the note is read.
 */
export const mentionCandidates = (members: ProjectMember[]): MentionCandidate[] =>
  members
    .map((m) => ({
      userId: m.user_id,
      handle: mentionHandle(m.user_name, m.user_email),
      label: m.user_name || m.user_email || 'Member',
      email: m.user_email,
    }))
    .filter((c) => !!c.handle);

/** A mention being typed: where its marker starts, and what follows it. */
export interface MentionQuery {
  /** Index of the first character of the marker ("@" or "@@"). */
  start: number;
  /** Text between the marker and the caret, which may be empty. */
  query: string;
  /** What the marker means. */
  scope: MentionScope;
}

/**
 * The mention the caret is inside, if any.
 *
 * A marker only opens the menu at a word boundary, so the "@" in an email
 * address someone is typing does not. Whitespace ends it: once the writer
 * moves on, the menu should not still be following them. One "@" or two are
 * markers; three or more are not.
 */
export const activeMentionQuery = (text: string, caret: number): MentionQuery | null => {
  if (caret < 0 || caret > text.length) return null;
  for (let i = caret - 1; i >= 0; i--) {
    const ch = text[i];
    if (ch === '@') {
      let start = i;
      while (start > 0 && text[start - 1] === '@') start--;
      const markerLength = i - start + 1;
      if (markerLength > 2) return null;
      const before = start > 0 ? text[start - 1] : '';
      if (before && !/\s/.test(before)) return null;
      return {
        start,
        query: text.slice(i + 1, caret),
        scope: markerLength === 2 ? 'todo' : 'mention',
      };
    }
    // A mention is one token: whitespace before an "@" means there is none.
    if (/\s/.test(ch)) return null;
  }
  return null;
};

/** Candidates whose handle or name matches what has been typed so far. */
export const matchMentions = (
  candidates: MentionCandidate[],
  query: string
): MentionCandidate[] => {
  const q = query.trim().toLowerCase();
  if (!q) return candidates;
  return candidates.filter(
    (c) => c.handle.toLowerCase().includes(q) || c.label.toLowerCase().includes(q)
  );
};

/**
 * Replace the mention being typed with the chosen handle, keeping the marker:
 * it is what tells the server, the reader and the to-do rule apart.
 */
export const applyMention = (
  text: string,
  query: MentionQuery,
  caret: number,
  handle: string
): { text: string; caret: number } => {
  const inserted = `${mentionMarker(query.scope)}${handle} `;
  const next = text.slice(0, query.start) + inserted + text.slice(caret);
  return { text: next, caret: query.start + inserted.length };
};

// "@@handle" at a word boundary. The server resolves the inner "@handle" as an
// ordinary mention, so a doubled marker notifies the person as well as raising
// their to-do — which is what the writer meant by naming them.
const todoPattern = /(^|\s)@@([\w.-]+)/g;

/**
 * The handles a note asks for a to-do for, lowercased and deduplicated in the
 * order they appear. Empty for a note with no "@@" in it.
 */
export const todoHandles = (message: string): string[] => {
  if (!message.includes('@@')) return [];
  const out: string[] = [];
  const seen = new Set<string>();
  for (const m of message.matchAll(todoPattern)) {
    const handle = m[2].toLowerCase();
    if (seen.has(handle)) continue;
    seen.add(handle);
    out.push(handle);
  }
  return out;
};

/** The candidates a note's "@@" markers name, in the order they appear. */
export const todoTargets = (
  message: string,
  candidates: MentionCandidate[]
): MentionCandidate[] => {
  const wanted = todoHandles(message);
  if (!wanted.length) return [];
  const out: MentionCandidate[] = [];
  for (const handle of wanted) {
    // Match against every handle the person answers to, not only the one the
    // menu would have written: the note may have been typed by hand.
    const hit = candidates.find((c) =>
      mentionHandles(c.label, c.email).concat(c.handle).some((h) => h.toLowerCase() === handle)
    );
    if (hit && !out.includes(hit)) out.push(hit);
  }
  return out;
};

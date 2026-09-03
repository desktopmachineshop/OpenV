// Typing "#" in a description offers the things this artifact can already
// point at, and inserts one as a citable reference.
//
// The offer is deliberately narrow: an artifact's own figures, and the
// artifacts it is already linked to. A description that cites REQ-12 without a
// traceability link to it is a claim the matrix cannot see, so the menu never
// invites one — it only names what the artifact is already connected to.
//
// The logic here is pure so it can be tested without a textarea: what to
// offer, whether the caret sits in a reference being typed, and what the text
// becomes once one is chosen.
import { Artifact, Attachment, Link } from '../api/client';

/** One thing a description can reference. */
export interface ReferenceCandidate {
  /** The reference itself, as it is cited: "REQ-12", "REQ-17-FIG-1". */
  ref: string;
  /** What it is called, for the menu row. */
  label: string;
  kind: 'figure' | 'artifact';
  /** Link type for a linked artifact ("verifies"), blank for a figure. */
  relation?: string;
}

/**
 * What "#" may offer while editing `artifact`: its own figures first — they
 * belong to the text being written — then the artifacts it is linked to.
 *
 * An artifact with no reference cannot be cited and is left out; so is a link
 * pointing at something not in `artifacts` (a different project, or not
 * loaded), because there is no reference to insert for it.
 */
export const referenceCandidates = (
  artifact: Pick<Artifact, 'id'> | undefined,
  links: Link[],
  artifacts: Artifact[],
  attachments: Attachment[]
): ReferenceCandidate[] => {
  if (!artifact?.id) return [];

  const figures: ReferenceCandidate[] = attachments
    .filter((a) => a.artifact_id === artifact.id && !!a.figure_ref)
    .map((a) => ({
      ref: a.figure_ref as string,
      label: a.original_filename || a.filename,
      kind: 'figure' as const,
    }));

  const byId = new Map(artifacts.map((a) => [a.id, a]));
  const seen = new Set<string>();
  const linked: ReferenceCandidate[] = [];
  for (const link of links) {
    const otherId =
      link.from_id === artifact.id ? link.to_id : link.to_id === artifact.id ? link.from_id : '';
    if (!otherId || seen.has(otherId)) continue;
    const other = byId.get(otherId);
    if (!other?.ref) continue;
    seen.add(otherId);
    linked.push({ ref: other.ref, label: other.title, kind: 'artifact', relation: link.type });
  }

  return [...figures, ...linked];
};

/** A reference being typed: where its "#" is, and what follows it so far. */
export interface ReferenceQuery {
  /** Index of the "#". */
  start: number;
  /** Text between the "#" and the caret, which may be empty. */
  query: string;
}

/**
 * The reference the caret is inside, if any.
 *
 * A "#" only opens the menu at a word boundary, so a "#" mid-word (a URL
 * fragment, an issue number written as "PR#12") is left alone. Whitespace ends
 * it: once the writer moves on, the menu should not still be following them.
 */
export const activeReferenceQuery = (text: string, caret: number): ReferenceQuery | null => {
  if (caret < 0 || caret > text.length) return null;
  for (let i = caret - 1; i >= 0; i--) {
    const ch = text[i];
    if (ch === '#') {
      const before = i > 0 ? text[i - 1] : '';
      if (before && !/\s/.test(before)) return null;
      return { start: i, query: text.slice(i + 1, caret) };
    }
    // A reference is one token: whitespace before a "#" means there is none.
    if (/\s/.test(ch)) return null;
  }
  return null;
};

/** Candidates whose reference or name matches what has been typed so far. */
export const matchReferences = (
  candidates: ReferenceCandidate[],
  query: string
): ReferenceCandidate[] => {
  const q = query.trim().toLowerCase();
  if (!q) return candidates;
  return candidates.filter(
    (c) => c.ref.toLowerCase().includes(q) || c.label.toLowerCase().includes(q)
  );
};

/**
 * Replace the reference being typed with the chosen one, and report where the
 * caret belongs afterwards.
 *
 * The "#" is kept: it is what marks the reference in the text, so a reader —
 * and later a renderer — can tell "#REQ-12" from a requirement that merely
 * mentions those characters. A trailing space follows, because the writer is
 * mid-sentence and would type one anyway.
 */
export const applyReference = (
  text: string,
  query: ReferenceQuery,
  caret: number,
  ref: string
): { text: string; caret: number } => {
  const inserted = `#${ref} `;
  const next = text.slice(0, query.start) + inserted + text.slice(caret);
  return { text: next, caret: query.start + inserted.length };
};

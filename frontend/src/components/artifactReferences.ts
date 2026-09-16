// Typing "#" in a description offers the things this artifact can already
// point at, and inserts one as a citable reference.
//
// The offer is deliberately narrow: an artifact's own figures, and the
// artifacts it is already linked to. A description that cites REQ-12 without a
// traceability link to it is a claim the matrix cannot see, so the menu never
// invites one — it only names what the artifact is already connected to.
//
// "##" widens that to the whole project: every figure, and every other
// artifact, whether or not this one is linked to it.
//
// The doubled marker is what makes that safe to offer. A single "#" stays the
// narrow, link-backed menu, so a citation written with one marker is still a
// claim the traceability matrix can see. "##" says out loud, in the text and
// in the menu, that the citation reaches outside what this artifact is
// connected to — a reader can tell the two apart at a glance, and so can a
// reviewer reading the rendered body.
//
// This is a deliberate widening of an earlier rule that kept artifacts out of
// the project-wide menu, on the grounds that a citation without a link is an
// untraceable claim. That concern is real and is now carried by the marker
// rather than by refusing the citation: people were writing the reference by
// hand anyway, where nothing marked it at all.
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
  /**
   * Title of the artifact a figure hangs on, set only for a figure that
   * belongs to a different artifact. It is what tells the writer which
   * drawing they are about to cite when the menu spans the whole project.
   */
  owner?: string;
}

/**
 * How wide a reference being typed reaches.
 *
 * "local" is a single "#": this artifact's own figures and the artifacts it is
 * linked to. "project" is "##": every figure in the project, whichever
 * artifact holds it.
 */
export type ReferenceScope = 'local' | 'project';

/** The marker a scope is written with. */
export const scopeMarker = (scope: ReferenceScope): string => (scope === 'project' ? '##' : '#');

/**
 * What "#" may offer while editing `artifact`: its own figures first — they
 * belong to the text being written — then the artifacts it is linked to.
 *
 * Under the "project" scope the offer widens to the whole project: every
 * figure in `attachments`, this artifact's own first so the nearest drawings
 * stay nearest, then every other artifact that has a reference. The artifact
 * being edited is left out of its own menu — a body citing itself says
 * nothing.
 *
 * An artifact with no reference cannot be cited and is left out; so is a link
 * pointing at something not in `artifacts` (a different project, or not
 * loaded), because there is no reference to insert for it.
 */
export const referenceCandidates = (
  artifact: Pick<Artifact, 'id'> | undefined,
  links: Link[],
  artifacts: Artifact[],
  attachments: Attachment[],
  scope: ReferenceScope = 'local'
): ReferenceCandidate[] => {
  if (!artifact?.id) return [];

  const byId = new Map(artifacts.map((a) => [a.id, a]));

  if (scope === 'project') {
    const cited = attachments.filter((a) => !!a.figure_ref);
    const own = cited.filter((a) => a.artifact_id === artifact.id);
    const elsewhere = cited.filter((a) => a.artifact_id !== artifact.id);
    const figures: ReferenceCandidate[] = [...own, ...elsewhere].map((a) => ({
      ref: a.figure_ref as string,
      label: a.original_filename || a.filename,
      kind: 'figure' as const,
      owner: a.artifact_id === artifact.id ? undefined : byId.get(a.artifact_id)?.title,
    }));
    // Every other artifact with a reference, of any type. No relation is set:
    // unlike the "#" menu there is no link to name, and claiming one would be
    // worse than saying nothing.
    const others: ReferenceCandidate[] = artifacts
      .filter((a) => a.id !== artifact.id && !!a.ref)
      .map((a) => ({
        ref: a.ref as string,
        label: a.title,
        kind: 'artifact' as const,
      }));
    return [...figures, ...others];
  }

  const figures: ReferenceCandidate[] = attachments
    .filter((a) => a.artifact_id === artifact.id && !!a.figure_ref)
    .map((a) => ({
      ref: a.figure_ref as string,
      label: a.original_filename || a.filename,
      kind: 'figure' as const,
    }));

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

/** A reference being typed: where its marker starts, and what follows it. */
export interface ReferenceQuery {
  /** Index of the first character of the marker ("#" or "##"). */
  start: number;
  /** Text between the marker and the caret, which may be empty. */
  query: string;
  /** How wide the marker reaches. */
  scope: ReferenceScope;
}

/**
 * The reference the caret is inside, if any.
 *
 * A marker only opens the menu at a word boundary, so a "#" mid-word (a URL
 * fragment, an issue number written as "PR#12") is left alone. Whitespace ends
 * it: once the writer moves on, the menu should not still be following them.
 *
 * One "#" or two are markers; three or more are not, so the "###" of a
 * third-level heading never opens a menu.
 */
export const activeReferenceQuery = (text: string, caret: number): ReferenceQuery | null => {
  if (caret < 0 || caret > text.length) return null;
  for (let i = caret - 1; i >= 0; i--) {
    const ch = text[i];
    if (ch === '#') {
      // The whole run of "#" is the marker, however far back it goes.
      let start = i;
      while (start > 0 && text[start - 1] === '#') start--;
      const markerLength = i - start + 1;
      if (markerLength > 2) return null;
      const before = start > 0 ? text[start - 1] : '';
      if (before && !/\s/.test(before)) return null;
      return {
        start,
        query: text.slice(i + 1, caret),
        scope: markerLength === 2 ? 'project' : 'local',
      };
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
 * The marker is kept: it is what marks the reference in the text, so a reader —
 * and later a renderer — can tell "#REQ-12" from a requirement that merely
 * mentions those characters, and "##REQ-17-FIG-1" from a figure of this
 * artifact's own. A trailing space follows, because the writer is mid-sentence
 * and would type one anyway.
 */
export const applyReference = (
  text: string,
  query: ReferenceQuery,
  caret: number,
  ref: string
): { text: string; caret: number } => {
  const inserted = `${scopeMarker(query.scope)}${ref} `;
  const next = text.slice(0, query.start) + inserted + text.slice(caret);
  return { text: next, caret: query.start + inserted.length };
};

/**
 * URL scheme a rendered reference links to. It is not a real protocol: the
 * renderer intercepts it and hands the reference back to the app, so a
 * citation navigates within the project instead of leaving it.
 */
export const REFERENCE_SCHEME = 'openv-ref:';

// A reference in prose: one "#" or two at a word boundary, then the reference
// itself. Trailing punctuation is left out of the match so "see #REQ-12."
// links the reference and keeps the full stop as text.
const inlineReferencePattern = /(^|\s)(#{1,2})([A-Za-z][A-Za-z0-9]*-\d+(?:-FIG-\d+)?)/g;

/**
 * Rewrite the references in a body as markdown links, so the markdown
 * renderer produces anchors the app can intercept.
 *
 * The marker stays in the link's text and out of its target: what the reader
 * sees still says how far the citation reaches, while the app is handed the
 * bare reference to resolve.
 *
 * Only text that already looks like a reference is touched — a bare "#" or a
 * markdown heading is left exactly as written, because turning "# Heading"
 * into a link would break every document that uses headings.
 */
export const linkifyReferences = (body: string): string =>
  body.replace(inlineReferencePattern, (_m, before: string, marker: string, ref: string) =>
    `${before}[${marker}${ref}](${REFERENCE_SCHEME}${ref})`
  );

/** Whether a reference names a figure ("REQ-17-FIG-1") rather than an artifact. */
export const isFigureRef = (ref: string): boolean => /-FIG-\d+$/.test(ref);

/** The artifact a figure hangs on: "REQ-17-FIG-1" is held by "REQ-17". */
export const artifactRefOfFigure = (ref: string): string =>
  ref.replace(/-FIG-\d+$/, '');

/** The reference a rendered link points at, or '' when it is an ordinary link. */
export const referenceFromHref = (href: string | undefined): string =>
  href && href.startsWith(REFERENCE_SCHEME) ? href.slice(REFERENCE_SCHEME.length) : '';

/**
 * Keep reference links intact through the markdown renderer's URL sanitiser.
 *
 * react-markdown blanks the href of any scheme it does not recognise as safe,
 * and `openv-ref:` is not one of them — which left every citation rendering as
 * an anchor with an empty href, so following one re-opened the page the reader
 * was already on instead of going to what it named. References are generated
 * here from text that has already matched the reference pattern, never from
 * anything a writer can type into an anchor, so passing them through is safe;
 * every other URL still goes to the renderer's own sanitiser.
 */
export const referenceUrlTransform = (
  url: string,
  key: string,
  node: unknown,
  fallback: (url: string, key: string, node: any) => string
): string => (url.startsWith(REFERENCE_SCHEME) ? url : fallback(url, key, node));

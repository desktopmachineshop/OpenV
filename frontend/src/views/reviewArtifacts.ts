import { Artifact, Attachment } from '../api/client';

/**
 * Helpers behind the review queue's artifact table, kept out of the view so
 * they can be tested without mounting React.
 */

/** Attachments grouped by the artifact they hang off. */
export const groupAttachments = (attachments: Attachment[]): Record<string, Attachment[]> => {
  const byArtifact: Record<string, Attachment[]> = {};
  for (const a of attachments) {
    (byArtifact[a.artifact_id] ||= []).push(a);
  }
  // Figure order is what a reader sees on the artifact: FIG-1 before FIG-2.
  for (const list of Object.values(byArtifact)) {
    list.sort((x, y) => (x.figure_num ?? 0) - (y.figure_num ?? 0));
  }
  return byArtifact;
};

/**
 * A one-glance preview of an artifact's body.
 *
 * The body is markdown, and a reviewer scanning a table wants the sentence,
 * not the syntax — so the markers that only carry formatting are dropped and
 * the text is collapsed onto one run. This is deliberately not a renderer:
 * the full text is one click away on the artifact itself, and a table that
 * reflowed around headings and lists would be unreadable at a glance.
 */
export const previewText = (body: string, limit = 220): string => {
  const flat = (body || '')
    .replace(/```[\s\S]*?```/g, ' ')          // fenced code
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')    // images
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')  // links keep their text
    .replace(/^\s{0,3}#{1,6}\s+/gm, '')       // heading markers
    .replace(/^\s{0,3}[-*+]\s+/gm, '')        // bullets
    .replace(/^\s{0,3}>\s?/gm, '')            // quotes
    .replace(/[*_`~]/g, '')                   // emphasis
    .replace(/\s+/g, ' ')
    .trim();
  if (flat.length <= limit) return flat;
  // Cut on a word boundary so the preview does not end mid-word.
  const cut = flat.slice(0, limit);
  const lastSpace = cut.lastIndexOf(' ');
  return `${(lastSpace > limit * 0.6 ? cut.slice(0, lastSpace) : cut).trimEnd()}…`;
};

/** What a figure chip says for a file that cannot be shown as a thumbnail. */
export const attachmentLabel = (a: Attachment): string =>
  a.figure_ref || a.title || a.original_filename || a.filename || 'figure';

/** The label a reviewer reads for one artifact, ref included when it has one. */
export const artifactLabel = (a: Artifact): string => a.title || '(untitled)';

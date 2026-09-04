// What a project download contains, and how that reaches the API.
//
// Taking a project away used to be four buttons — JSON, CSV, PDF, Word — each
// meaning "all of it". A reader who wanted the requirements without the
// verification section, or the specification with its figures beside it, had
// to take everything and cut it down by hand.
//
// One selection now describes what a download holds, and the same selection is
// sent whichever format is chosen, so "the requirements sections, no headings,
// with figures" means the same thing as a PDF and as a CSV.
//
// Two vocabularies meet here, and keeping them apart is most of this file:
//
//   in the form  — every list is what the reader has TICKED. Nothing ticked
//                  means nothing, which is why it is refused rather than sent.
//   on the wire  — an omitted list means "everything", so a download that
//                  narrows nothing sends nothing and the server's defaults
//                  produce what the old export always did.
//
// toWire is the translation, and it is why unticking one of two sections sends
// a filter while ticking both sends none.
import { DownloadFormat, DownloadOptions, DownloadSelection } from '../api/client';

/** The formats a project can be taken away as, in the order they are offered. */
export interface FormatChoice {
  format: DownloadFormat;
  label: string;
  /** What this format is for, in the words of someone deciding. */
  description: string;
}

export const DOWNLOAD_FORMATS: FormatChoice[] = [
  {
    format: 'pdf',
    label: 'PDF specification',
    description: 'The document as a reader sees it: sections, artifacts, figures and traceability.',
  },
  {
    format: 'docx',
    label: 'Word document',
    description: 'The same specification as a .docx, for editing or review outside OpenV.',
  },
  {
    format: 'json',
    label: 'JSON data',
    description: 'The complete project, including everything an OpenV import can restore.',
  },
  {
    format: 'csv',
    label: 'CSV table',
    description: 'One row per artifact for a spreadsheet. Links fold into a single column.',
  },
  {
    format: 'reqif',
    label: 'ReqIF interchange',
    description: 'The OMG format read by DOORS and Polarion.',
  },
];

/** What each attachment category is called where a reader chooses it. */
const ATTACHMENT_LABELS: Record<string, string> = {
  figures: 'Figures',
  images: 'Unnumbered images',
  documents: 'Documents',
  data: 'Data files',
  other: 'Other files',
};

export const attachmentLabel = (category: string): string =>
  ATTACHMENT_LABELS[category] || category;

/** A ticked-box selection, before it is translated for the wire. */
export interface FormSelection {
  /** Ticked section ids. */
  sections: string[];
  /** Ticked artifact types. */
  types: string[];
  includeHeadings: boolean;
  /** Ticked attachment categories. Untouched, no files travel. */
  attachments: string[];
}

/**
 * Everything ticked but the attachments: the form a download opens on, which
 * produces exactly what the old export buttons did.
 */
export const selectAll = (options: DownloadOptions | null): FormSelection => ({
  sections: (options?.sections || []).map((s) => s.id),
  types: (options?.types || []).map((t) => t.type),
  includeHeadings: true,
  attachments: [],
});

/** Add or remove one value from a ticked list. */
export const toggle = (list: string[], value: string): string[] =>
  list.includes(value) ? list.filter((v) => v !== value) : [...list, value];

/**
 * Whether the form would produce a document with nothing in it. A reader who
 * has unticked every section, or every type with headings off, is about to
 * download an empty file and should be told before they do.
 */
export const selectsNothing = (
  selection: FormSelection,
  options: DownloadOptions | null
): boolean => {
  if (!options) return false;
  if (options.sections.length > 0 && selection.sections.length === 0) return true;
  if (options.types.length > 0 && selection.types.length === 0 && !selection.includeHeadings) {
    return true;
  }
  return false;
};

/**
 * The wire form of a selection: only what was actually narrowed.
 *
 * A list with everything ticked is left out, because "all of them" and "no
 * filter" are the same download and the shorter request is the one that keeps
 * working when the project gains a section.
 */
export const toWire = (
  selection: FormSelection,
  options: DownloadOptions | null
): DownloadSelection => {
  const allSections = (options?.sections || []).map((s) => s.id);
  const allTypes = (options?.types || []).map((t) => t.type);
  const complete = (ticked: string[], all: string[]) =>
    all.length === 0 || ticked.length >= all.length;
  return {
    sections: complete(selection.sections, allSections) ? [] : selection.sections,
    types: complete(selection.types, allTypes) ? [] : selection.types,
    includeHeadings: selection.includeHeadings,
    attachments: selection.attachments,
  };
};

/**
 * The query string for a download. Only the narrowing travels; the server's
 * defaults are the whole project with its headings and no files.
 */
export const downloadQuery = (selection: DownloadSelection, baselineId?: string): string => {
  const params = new URLSearchParams();
  if (selection.sections.length > 0) params.set('sections', selection.sections.join(','));
  if (selection.types.length > 0) params.set('types', selection.types.join(','));
  if (!selection.includeHeadings) params.set('headings', '0');
  if (selection.attachments.length > 0) params.set('attachments', selection.attachments.join(','));
  if (baselineId && baselineId !== 'live') params.set('baseline_id', baselineId);
  return params.toString();
};

/** Whether files travel with the document, which makes the download an archive. */
export const isArchive = (selection: FormSelection): boolean => selection.attachments.length > 0;

/**
 * The selection in a sentence, for the button that acts on it — what a reader
 * checks before committing, because a download missing the section they needed
 * is discovered far too late.
 */
export const describeSelection = (
  selection: FormSelection,
  options: DownloadOptions | null
): string => {
  const parts: string[] = [];
  const sections = options?.sections.length ?? 0;
  const types = options?.types.length ?? 0;

  if (sections > 0 && selection.sections.length < sections) {
    parts.push(`${selection.sections.length} of ${sections} sections`);
  } else {
    parts.push('the whole project');
  }
  if (types > 0 && selection.types.length < types) {
    parts.push(`${selection.types.join(', ')} only`);
  }
  if (!selection.includeHeadings) parts.push('no headings');
  if (selection.attachments.length > 0) {
    parts.push(`with ${selection.attachments.map(attachmentLabel).join(' and ').toLowerCase()}`);
  }

  const sentence = parts.join(', ');
  return sentence.charAt(0).toUpperCase() + sentence.slice(1);
};

/** A file size a person can read: "2.4 MB". */
export const formatBytes = (bytes: number): string => {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  // One decimal, but never a bare ".0": "2 KB" reads better than "2.0 KB".
  const rounded = value >= 10 || unit === 0 ? Math.round(value) : Math.round(value * 10) / 10;
  return `${rounded} ${units[unit]}`;
};

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
import {
  DownloadContent,
  DownloadFormat,
  DownloadOptions,
  DownloadSelection,
  DownloadTemplate,
} from '../api/client';

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
    format: 'excel',
    label: 'Excel workbook (.xlsx)',
    description: 'A sheet per artifact type, one for the links, and a cover naming the snapshot.',
  },
  {
    format: 'reqif',
    label: 'ReqIF interchange',
    description: 'The OMG format read by DOORS and Polarion.',
  },
];

/**
 * The extension a format's file is saved under. It is not always the format's
 * own name: `excel` is what the wire calls the workbook, and `.xlsx` is what a
 * spreadsheet opens — a file named `.excel` opens as nothing at all.
 *
 * Only used for the fallback filename, when the server sent no
 * Content-Disposition to take a name from.
 */
const DOWNLOAD_EXTENSIONS: Partial<Record<DownloadFormat, string>> = {
  excel: 'xlsx',
};

export const downloadExtension = (format: DownloadFormat): string =>
  DOWNLOAD_EXTENSIONS[format] || format;

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

/** The switches that decide what a PDF or Word document holds beyond its artifacts. */
export interface ContentSwitches {
  traceability: boolean;
  figures: boolean;
  toc: boolean;
  testResults: boolean;
  vvStatus: boolean;
}

/** A ticked-box selection, before it is translated for the wire. */
export interface FormSelection {
  /** Ticked section ids. */
  sections: string[];
  /** Ticked artifact types. */
  types: string[];
  includeHeadings: boolean;
  /** Ticked attachment categories. Untouched, no files travel. */
  attachments: string[];
  /**
   * The preset the reader started from, or '' for none. It stays as it was
   * when a switch is later changed by hand: the cover records the starting
   * point, not a promise that nothing moved since.
   */
  template: string;
  content: ContentSwitches;
  /** Every attribute shows, whatever `fields` holds. */
  allFields: boolean;
  /** Ticked attribute keys, honoured when `allFields` is off. */
  fields: string[];
}

/** What the server puts in a document when it is told nothing. */
export const SERVER_CONTENT_DEFAULTS: DownloadContent = {
  traceability: true,
  figures: true,
  toc: true,
  all_fields: true,
  test_results: false,
  vv_status: false,
};

/** The preset a wizard opens on, when the server offers presets at all. */
const OPENING_TEMPLATE = 'standard';

const contentSwitches = (content: DownloadContent): ContentSwitches => ({
  traceability: content.traceability,
  figures: content.figures,
  toc: content.toc,
  testResults: content.test_results,
  vvStatus: content.vv_status,
});

/**
 * Everything ticked but the attachments, with the document switches at the
 * server's defaults: the form a download opens on, which produces exactly what
 * the old export buttons did.
 */
export const selectAll = (options: DownloadOptions | null): FormSelection => {
  const defaults = options?.defaults || SERVER_CONTENT_DEFAULTS;
  const templates = options?.templates || [];
  const opening = templates.find((t) => t.key === OPENING_TEMPLATE) || templates[0];
  return {
    sections: (options?.sections || []).map((s) => s.id),
    types: (options?.types || []).map((t) => t.type),
    includeHeadings: true,
    attachments: [],
    template: opening ? opening.key : '',
    content: contentSwitches(defaults),
    allFields: true,
    fields: (options?.fields || []).map((f) => f.key),
  };
};

/**
 * Start a selection from a preset. The preset says which types and which
 * document contents it means; the sections and attachments are the reader's
 * own and stay as they are. A preset can name a type or a field the project
 * does not have, so both lists are cut down to what the form can actually
 * offer.
 */
export const applyTemplate = (
  selection: FormSelection,
  template: DownloadTemplate,
  options: DownloadOptions | null
): FormSelection => {
  const allTypes = (options?.types || []).map((t) => t.type);
  const allFields = (options?.fields || []).map((f) => f.key);
  const content = template.content;
  return {
    ...selection,
    template: template.key,
    types: template.types ? allTypes.filter((t) => template.types!.includes(t)) : allTypes,
    content: contentSwitches(content),
    allFields: content.all_fields,
    fields: content.all_fields ? allFields : allFields.filter((k) => (content.fields || []).includes(k)),
  };
};

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
    template: selection.template,
    traceability: selection.content.traceability,
    figures: selection.content.figures,
    toc: selection.content.toc,
    testResults: selection.content.testResults,
    vvStatus: selection.content.vvStatus,
    fields: selection.allFields ? undefined : selection.fields,
  };
};

/**
 * The query string for a download. Only the narrowing travels; the server's
 * defaults are the whole project with its headings, its traceability, figures
 * and contents, every field, no results, no V&V rollup and no files.
 *
 * The template travels too, but only as a name for the cover: every switch it
 * implies is sent as itself, so what the reader saw ticked is what the server
 * builds even if the preset changes underneath them.
 */
export const downloadQuery = (selection: DownloadSelection, baselineId?: string): string => {
  const defaults = SERVER_CONTENT_DEFAULTS;
  const params = new URLSearchParams();
  if (selection.sections.length > 0) params.set('sections', selection.sections.join(','));
  if (selection.types.length > 0) params.set('types', selection.types.join(','));
  if (!selection.includeHeadings) params.set('headings', '0');
  if (selection.attachments.length > 0) params.set('attachments', selection.attachments.join(','));
  if (baselineId && baselineId !== 'live') params.set('baseline_id', baselineId);

  if (selection.template) params.set('template', selection.template);
  const flag = (name: string, value: boolean, fallback: boolean) => {
    if (value !== fallback) params.set(name, value ? '1' : '0');
  };
  flag('traceability', selection.traceability, defaults.traceability);
  flag('figures', selection.figures, defaults.figures);
  flag('toc', selection.toc, defaults.toc);
  flag('results', selection.testResults, defaults.test_results);
  flag('vv', selection.vvStatus, defaults.vv_status);
  if (selection.fields) {
    params.set('fields', selection.fields.length === 0 ? 'none' : selection.fields.join(','));
  }
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

  // The document switches are worth a word only where they left the defaults:
  // what was added reads as "with …", what was taken away as "without …".
  const defaults = options?.defaults || SERVER_CONTENT_DEFAULTS;
  const added: string[] = [];
  const removed: string[] = [];
  const note = (label: string, value: boolean, fallback: boolean) => {
    if (value === fallback) return;
    (value ? added : removed).push(label);
  };
  note('a table of contents', selection.content.toc, defaults.toc);
  note('traceability', selection.content.traceability, defaults.traceability);
  note('figures', selection.content.figures, defaults.figures);
  note('V&V status', selection.content.vvStatus, defaults.vv_status);
  note('test results', selection.content.testResults, defaults.test_results);
  if (added.length > 0) parts.push(`with ${added.join(' and ')}`);
  if (removed.length > 0) parts.push(`without ${removed.join(' or ')}`);
  if (!selection.allFields) {
    const fields = options?.fields?.length ?? 0;
    if (selection.fields.length === 0) parts.push('no fields');
    else if (fields > 0 && selection.fields.length < fields) {
      parts.push(`${selection.fields.length} of ${fields} fields`);
    }
  }

  const template = options?.templates?.find((t) => t.key === selection.template);
  const sentence = parts.join(', ');
  if (template) return `${template.name}: ${sentence}`;
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

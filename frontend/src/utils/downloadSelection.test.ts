import { DownloadOptions, DownloadTemplate } from '../api/client';
import {
  DOWNLOAD_FORMATS,
  FormSelection,
  SERVER_CONTENT_DEFAULTS,
  applyTemplate,
  attachmentLabel,
  describeSelection,
  downloadExtension,
  downloadQuery,
  formatBytes,
  isArchive,
  selectAll,
  selectsNothing,
  toWire,
  toggle,
} from './downloadSelection';

const options: DownloadOptions = {
  sections: [
    { id: 's1', ref: 'HDG-1', number: '1', title: 'Requirements', artifacts: 12 },
    { id: 's2', ref: 'HDG-2', number: '2', title: 'Verification', artifacts: 4 },
  ],
  types: [
    { type: 'requirement', count: 12 },
    { type: 'test-case', count: 4 },
  ],
  attachments: [
    { category: 'figures', count: 3, bytes: 2048 },
    { category: 'data', count: 1, bytes: 512 },
  ],
};

// The presets the server offers, as the wizard sees them. A project with
// presets also names its fields and says what a document holds by default.
const standard: DownloadTemplate = {
  key: 'standard',
  name: 'Specification',
  description: 'The whole document.',
  content: { ...SERVER_CONTENT_DEFAULTS, template: 'standard' },
};
const vv: DownloadTemplate = {
  key: 'vv',
  name: 'Verification & Validation',
  description: 'Requirements and test cases with their verification state.',
  // A type the project does not hold, to check it is cut down.
  types: ['requirement', 'test-case', 'test-procedure'],
  content: {
    template: 'vv',
    traceability: true,
    figures: false,
    toc: true,
    all_fields: false,
    fields: ['priority', 'verification_status', 'not_here'],
    test_results: true,
    vv_status: true,
  },
};
const withTemplates: DownloadOptions = {
  ...options,
  fields: [
    { key: 'priority', label: 'Priority', count: 12, custom: false },
    { key: 'status', label: 'Status', count: 16, custom: false },
    { key: 'verification_status', label: 'Verification status', count: 12, custom: false },
    { key: 'owner', label: 'Owner', count: 3, custom: true },
  ],
  templates: [standard, vv],
  defaults: SERVER_CONTENT_DEFAULTS,
};

describe('selectAll', () => {
  it('opens on the whole project, with no files', () => {
    const got = selectAll(options);
    expect(got.sections).toEqual(['s1', 's2']);
    expect(got.types).toEqual(['requirement', 'test-case']);
    expect(got.includeHeadings).toBe(true);
    // Files turn a download into an archive, so they are opt-in.
    expect(got.attachments).toEqual([]);
  });

  it('opens on the server defaults with every field, and no preset when none is offered', () => {
    const got = selectAll(options);
    expect(got.template).toBe('');
    expect(got.content).toEqual({
      traceability: true,
      figures: true,
      toc: true,
      testResults: false,
      vvStatus: false,
    });
    expect(got.allFields).toBe(true);
    expect(got.fields).toEqual([]);
  });

  it('starts from the standard preset when the server offers presets', () => {
    const got = selectAll(withTemplates);
    expect(got.template).toBe('standard');
    expect(got.fields).toEqual(['priority', 'status', 'verification_status', 'owner']);
    expect(got.allFields).toBe(true);
  });

  it('survives a project whose options have not loaded', () => {
    expect(selectAll(null)).toEqual({
      sections: [],
      types: [],
      includeHeadings: true,
      attachments: [],
      template: '',
      content: { traceability: true, figures: true, toc: true, testResults: false, vvStatus: false },
      allFields: true,
      fields: [],
    });
  });
});

describe('applyTemplate', () => {
  it('narrows the types to the ones the project actually has', () => {
    const got = applyTemplate(selectAll(withTemplates), vv, withTemplates);
    expect(got.template).toBe('vv');
    expect(got.types).toEqual(['requirement', 'test-case']);
  });

  it('opens every type for a preset that names none', () => {
    const narrowed = { ...selectAll(withTemplates), types: ['requirement'] };
    expect(applyTemplate(narrowed, standard, withTemplates).types).toEqual(['requirement', 'test-case']);
  });

  it('takes the content switches and fields from the preset', () => {
    const got = applyTemplate(selectAll(withTemplates), vv, withTemplates);
    expect(got.content).toEqual({
      traceability: true,
      figures: false,
      toc: true,
      testResults: true,
      vvStatus: true,
    });
    expect(got.allFields).toBe(false);
    // A key the project does not hold is not ticked, since it cannot be shown.
    expect(got.fields).toEqual(['priority', 'verification_status']);
  });

  it('leaves the sections and attachments as the reader had them', () => {
    const before = { ...selectAll(withTemplates), sections: ['s1'], attachments: ['figures'] };
    const got = applyTemplate(before, vv, withTemplates);
    expect(got.sections).toEqual(['s1']);
    expect(got.attachments).toEqual(['figures']);
  });
});

describe('toggle', () => {
  it('adds what is missing and removes what is there', () => {
    expect(toggle(['a'], 'b')).toEqual(['a', 'b']);
    expect(toggle(['a', 'b'], 'a')).toEqual(['b']);
  });
});

// The form speaks in ticked boxes; the wire speaks in narrowing. Everything
// ticked is the same download as no filter at all, and the shorter request is
// the one that still means "everything" after a section is added.
describe('toWire', () => {
  it('sends no filter when everything is ticked', () => {
    const wire = toWire(selectAll(options), options);
    expect(wire.sections).toEqual([]);
    expect(wire.types).toEqual([]);
    expect(wire.includeHeadings).toBe(true);
  });

  it('sends the narrowing when something is unticked', () => {
    const wire = toWire({ ...selectAll(options), sections: ['s1'] }, options);
    expect(wire.sections).toEqual(['s1']);
    expect(wire.types).toEqual([]);
  });

  it('carries the attachment categories through untouched', () => {
    const wire = toWire({ ...selectAll(options), attachments: ['figures'] }, options);
    expect(wire.attachments).toEqual(['figures']);
  });

  it('sends the heading choice either way', () => {
    expect(toWire({ ...selectAll(options), includeHeadings: false }, options).includeHeadings).toBe(false);
  });

  it('carries the preset and the document switches', () => {
    const wire = toWire(applyTemplate(selectAll(withTemplates), vv, withTemplates), withTemplates);
    expect(wire.template).toBe('vv');
    expect(wire.figures).toBe(false);
    expect(wire.vvStatus).toBe(true);
    expect(wire.testResults).toBe(true);
    expect(wire.fields).toEqual(['priority', 'verification_status']);
  });

  it('sends no field filter while every field shows', () => {
    expect(toWire(selectAll(withTemplates), withTemplates).fields).toBeUndefined();
  });
});

describe('downloadQuery', () => {
  it('is empty for a download that narrows nothing', () => {
    expect(downloadQuery(toWire(selectAll(options), options))).toBe('');
  });

  it('spells out only what was narrowed', () => {
    const query = downloadQuery(
      toWire(
        {
          ...selectAll(options),
          sections: ['s1'],
          types: ['requirement'],
          includeHeadings: false,
          attachments: ['figures', 'data'],
        },
        options
      )
    );
    const params = new URLSearchParams(query);
    expect(params.get('sections')).toBe('s1');
    expect(params.get('types')).toBe('requirement');
    expect(params.get('headings')).toBe('0');
    expect(params.get('attachments')).toBe('figures,data');
    expect(params.has('traceability')).toBe(false);
    expect(params.has('fields')).toBe(false);
  });

  it('names the preset but sends its switches as themselves', () => {
    const query = downloadQuery(toWire(selectAll(withTemplates), withTemplates));
    // The standard preset is the server's own default, so nothing but its
    // name travels.
    expect(query).toBe('template=standard');
  });

  it('spells out the V&V preset as what it switches on', () => {
    const wire = toWire(applyTemplate(selectAll(withTemplates), vv, withTemplates), withTemplates);
    const params = new URLSearchParams(downloadQuery(wire));
    expect(params.get('template')).toBe('vv');
    expect(params.get('vv')).toBe('1');
    expect(params.get('results')).toBe('1');
    expect(params.get('figures')).toBe('0');
    // Still the default, so not worth a parameter.
    expect(params.has('traceability')).toBe(false);
    expect(params.has('toc')).toBe(false);
    expect(params.get('fields')).toBe('priority,verification_status');
  });

  it('sends the switches a reader turned off', () => {
    const selection: FormSelection = {
      ...selectAll(withTemplates),
      content: { traceability: false, figures: true, toc: false, testResults: false, vvStatus: false },
    };
    const params = new URLSearchParams(downloadQuery(toWire(selection, withTemplates)));
    expect(params.get('traceability')).toBe('0');
    expect(params.get('toc')).toBe('0');
    expect(params.has('figures')).toBe(false);
  });

  it('says which fields show, and when none do', () => {
    const some: FormSelection = {
      ...selectAll(withTemplates),
      allFields: false,
      fields: ['priority', 'status'],
    };
    expect(new URLSearchParams(downloadQuery(toWire(some, withTemplates))).get('fields')).toBe(
      'priority,status'
    );
    const none: FormSelection = { ...selectAll(withTemplates), allFields: false, fields: [] };
    expect(new URLSearchParams(downloadQuery(toWire(none, withTemplates))).get('fields')).toBe('none');
  });

  it('carries a baseline but not the live project', () => {
    const wire = toWire(selectAll(options), options);
    expect(new URLSearchParams(downloadQuery(wire, 'b7')).get('baseline_id')).toBe('b7');
    expect(downloadQuery(wire, 'live')).toBe('');
    expect(downloadQuery(wire, undefined)).toBe('');
  });
});

describe('selectsNothing', () => {
  it('is quiet while everything is ticked', () => {
    expect(selectsNothing(selectAll(options), options)).toBe(false);
  });

  it('catches every section unticked', () => {
    expect(selectsNothing({ ...selectAll(options), sections: [] }, options)).toBe(true);
  });

  it('catches every type unticked with no headings to fall back on', () => {
    expect(
      selectsNothing({ ...selectAll(options), types: [], includeHeadings: false }, options)
    ).toBe(true);
  });

  it('allows a headings-only download', () => {
    expect(selectsNothing({ ...selectAll(options), types: [] }, options)).toBe(false);
  });

  it('says nothing before the options have loaded', () => {
    expect(selectsNothing(selectAll(null), null)).toBe(false);
  });
});

describe('describeSelection', () => {
  it('names the whole project when nothing is narrowed', () => {
    expect(describeSelection(selectAll(options), options)).toBe('The whole project');
  });

  it('counts the sections a reader kept', () => {
    expect(describeSelection({ ...selectAll(options), sections: ['s1'] }, options)).toBe(
      '1 of 2 sections'
    );
  });

  it('says what was left out and what was added', () => {
    const got = describeSelection(
      {
        ...selectAll(options),
        sections: ['s1'],
        types: ['requirement'],
        includeHeadings: false,
        attachments: ['figures'],
      },
      options
    );
    expect(got).toBe('1 of 2 sections, requirement only, no headings, with figures');
  });

  it('names the preset it started from', () => {
    expect(describeSelection(selectAll(withTemplates), withTemplates)).toBe(
      'Specification: the whole project'
    );
  });

  it('names what a preset switched on beyond the defaults', () => {
    const got = describeSelection(
      applyTemplate(selectAll(withTemplates), vv, withTemplates),
      withTemplates
    );
    expect(got).toBe(
      'Verification & Validation: the whole project, with V&V status and test results, without figures, 2 of 4 fields'
    );
  });

  it('names what a reader switched off', () => {
    const got = describeSelection(
      {
        ...selectAll(withTemplates),
        template: '',
        content: { traceability: false, figures: true, toc: true, testResults: false, vvStatus: false },
        allFields: false,
        fields: [],
      },
      withTemplates
    );
    expect(got).toBe('The whole project, without traceability, no fields');
  });
});

describe('isArchive', () => {
  it('is an archive exactly when files travel with the document', () => {
    expect(isArchive(selectAll(options))).toBe(false);
    expect(isArchive({ ...selectAll(options), attachments: ['figures'] })).toBe(true);
  });
});

describe('formatBytes', () => {
  it('reads like a file size', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(2048)).toBe('2 KB');
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(5 * 1024 * 1024)).toBe('5 MB');
  });

  it('does not trip over nonsense', () => {
    expect(formatBytes(-1)).toBe('0 B');
    expect(formatBytes(NaN)).toBe('0 B');
  });
});

describe('the offered formats', () => {
  it('covers every output the server renders', () => {
    expect(DOWNLOAD_FORMATS.map((f) => f.format)).toEqual([
      'pdf',
      'docx',
      'json',
      'csv',
      'excel',
      'reqif',
    ]);
  });

  it('names the Excel workbook by its extension, since that is what a reader looks for', () => {
    const excel = DOWNLOAD_FORMATS.find((f) => f.format === 'excel');
    expect(excel?.label).toBe('Excel workbook (.xlsx)');
  });

  it('saves each format under the extension its file actually has', () => {
    // The wire name is not always the extension: a workbook asked for as
    // "excel" is an .xlsx file, and naming it .excel opens as nothing.
    expect(downloadExtension('excel')).toBe('xlsx');
    for (const format of ['pdf', 'docx', 'json', 'csv', 'reqif'] as const) {
      expect(downloadExtension(format)).toBe(format);
    }
  });

  it('names each attachment category in words', () => {
    expect(attachmentLabel('figures')).toBe('Figures');
    expect(attachmentLabel('data')).toBe('Data files');
    // An unknown category still shows as itself rather than vanishing.
    expect(attachmentLabel('holograms')).toBe('holograms');
  });
});

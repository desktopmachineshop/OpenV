import { DownloadOptions } from '../api/client';
import {
  DOWNLOAD_FORMATS,
  attachmentLabel,
  describeSelection,
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

describe('selectAll', () => {
  it('opens on the whole project, with no files', () => {
    const got = selectAll(options);
    expect(got.sections).toEqual(['s1', 's2']);
    expect(got.types).toEqual(['requirement', 'test-case']);
    expect(got.includeHeadings).toBe(true);
    // Files turn a download into an archive, so they are opt-in.
    expect(got.attachments).toEqual([]);
  });

  it('survives a project whose options have not loaded', () => {
    expect(selectAll(null)).toEqual({
      sections: [],
      types: [],
      includeHeadings: true,
      attachments: [],
    });
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
});

describe('downloadQuery', () => {
  it('is empty for a download that narrows nothing', () => {
    expect(downloadQuery(toWire(selectAll(options), options))).toBe('');
  });

  it('spells out only what was narrowed', () => {
    const query = downloadQuery({
      sections: ['s1'],
      types: ['requirement'],
      includeHeadings: false,
      attachments: ['figures', 'data'],
    });
    const params = new URLSearchParams(query);
    expect(params.get('sections')).toBe('s1');
    expect(params.get('types')).toBe('requirement');
    expect(params.get('headings')).toBe('0');
    expect(params.get('attachments')).toBe('figures,data');
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
      { sections: ['s1'], types: ['requirement'], includeHeadings: false, attachments: ['figures'] },
      options
    );
    expect(got).toBe('1 of 2 sections, requirement only, no headings, with figures');
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
    expect(DOWNLOAD_FORMATS.map((f) => f.format)).toEqual(['pdf', 'docx', 'json', 'csv', 'reqif']);
  });

  it('names each attachment category in words', () => {
    expect(attachmentLabel('figures')).toBe('Figures');
    expect(attachmentLabel('data')).toBe('Data files');
    // An unknown category still shows as itself rather than vanishing.
    expect(attachmentLabel('holograms')).toBe('holograms');
  });
});

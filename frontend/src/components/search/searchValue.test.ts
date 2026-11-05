import {
  EMPTY_SEARCH,
  SearchSchema,
  SearchValue,
  addChip,
  chipParts,
  chipValues,
  filterItems,
  groupItems,
  groupKeys,
  readSearchParams,
  removeChipAt,
  suggestChips,
  toggleFieldValue,
  toggleGroup,
  toggleQuick,
  writeSearchParams,
} from './searchValue';

const schema: SearchSchema = {
  fields: [
    {
      key: 'status',
      label: 'Status',
      type: 'enum',
      options: [
        { value: 'draft', label: 'Draft' },
        { value: 'in_review', label: 'In review' },
        { value: 'approved', label: 'Approved' },
      ],
    },
    { key: 'owner', label: 'Owner', type: 'person', options: [{ value: 'Dave', label: 'Dave' }] },
    { key: 'title', label: 'Title', type: 'text' },
    { key: 'updated', label: 'Updated', type: 'date' },
  ],
  quickFilters: [{ key: 'mine', label: 'Mine' }],
  groupBy: [
    { key: 'status', label: 'Status' },
    { key: 'owner', label: 'Owner' },
  ],
  savedScope: 'test',
};

interface Row {
  id: string;
  status: string;
  owner: string;
  title: string;
  updated: string;
}

const rows: Row[] = [
  { id: 'a', status: 'draft', owner: 'Dave', title: 'Spindle speed', updated: '2026-11-04T10:00:00Z' },
  { id: 'b', status: 'in_review', owner: 'Sam', title: 'Coolant flow', updated: '2026-10-01T10:00:00Z' },
  { id: 'c', status: 'approved', owner: '', title: 'Spindle torque', updated: '2025-01-01T10:00:00Z' },
];

const acc = {
  fieldValue: (r: Row, f: string) => (r as unknown as Record<string, string>)[f],
  quickMatch: (r: Row, k: string) => (k === 'mine' ? r.owner === 'Dave' : true),
  now: new Date('2026-11-05T12:00:00Z'),
};

describe('chip editing', () => {
  it('merges a second value of the same facet into one chip, and never adds a duplicate', () => {
    let v = addChip(EMPTY_SEARCH, { kind: 'filter', field: 'status', values: ['draft'], match: 'is' });
    v = addChip(v, { kind: 'filter', field: 'status', values: ['in_review'], match: 'is' });
    v = addChip(v, { kind: 'filter', field: 'status', values: ['draft'], match: 'is' });
    expect(v.chips).toEqual([{ kind: 'filter', field: 'status', values: ['draft', 'in_review'], match: 'is' }]);
    expect(chipParts(schema, v.chips[0])).toEqual({ facet: 'Status', label: 'Draft or In review' });
    // A substring on the same field is its own chip.
    v = addChip(v, { kind: 'filter', field: 'status', values: ['rev'], match: 'contains' });
    expect(v.chips).toHaveLength(2);
  });

  it('toggles a value, a quick filter and a grouping on and off', () => {
    const status = schema.fields[0];
    let v = toggleFieldValue(EMPTY_SEARCH, status, 'draft');
    v = toggleFieldValue(v, status, 'approved');
    v = toggleFieldValue(v, status, 'draft');
    expect(chipValues(v, 'status')).toEqual(['approved']);
    v = toggleFieldValue(v, status, 'approved');
    expect(v.chips).toEqual([]);
    v = toggleGroup(toggleQuick(EMPTY_SEARCH, 'mine'), 'status');
    expect(v.chips.map((c) => c.kind)).toEqual(['quick', 'group']);
    expect(removeChipAt(toggleQuick(v, 'mine'), 0).chips).toEqual([]);
  });
});

describe('the URL', () => {
  it('writes every kind of chip and reads the same value back', () => {
    const value: SearchValue = {
      chips: [
        { kind: 'filter', field: 'status', values: ['draft', 'in_review'], match: 'is' },
        { kind: 'filter', field: 'owner', values: ['da|v e'], match: 'contains' },
        { kind: 'quick', key: 'mine' },
        { kind: 'group', key: 'owner' },
        { kind: 'saved', id: 's1', name: 'Release 1.4', value: { chips: [{ kind: 'quick', key: 'mine' }], text: 'spindle' } },
      ],
      text: 'torque',
    };
    const params = writeSearchParams(new URLSearchParams('view=tree&artifact=x'), value);
    expect(params.get('view')).toBe('tree');
    expect(params.get('artifact')).toBe('x');
    expect(params.getAll('f')).toEqual(['status:draft|in_review', 'owner~da%7Cv%20e']);
    const back = readSearchParams(new URLSearchParams(params.toString()), schema);
    expect(back).toEqual(value);
  });

  it('clears its own keys and leaves out what the schema does not offer', () => {
    const params = writeSearchParams(new URLSearchParams('q=old&f=status:draft&g=status'), EMPTY_SEARCH);
    expect(params.toString()).toBe('');
    const back = readSearchParams(new URLSearchParams('f=colour:red&f=status:&qf=nope&g=type&g=status&sv=notjson'), schema);
    expect(back).toEqual({ chips: [{ kind: 'group', key: 'status' }], text: '' });
  });

  it('keeps two bars apart by prefix', () => {
    const params = writeSearchParams(new URLSearchParams(), { chips: [{ kind: 'quick', key: 'mine' }], text: 'x' }, 'g_');
    expect(params.get('g_qf')).toBe('mine');
    expect(readSearchParams(params).chips).toEqual([]);
    expect(readSearchParams(params, schema, 'g_').text).toBe('x');
  });
});

describe('suggestions', () => {
  it('offers matching values and a substring search of each text field', () => {
    const labels = suggestChips(schema, 'dav').map((s) => s.label);
    expect(labels).toEqual(['Owner: Dave', 'Search Owner for: dav', 'Search Title for: dav']);
    expect(suggestChips(schema, '  ')).toEqual([]);
    expect(suggestChips(schema, 'review').map((s) => s.label)).toContain('Status: In review');
  });
});

describe('filtering and grouping', () => {
  it('ANDs chips, ORs the values of one chip, and matches text and dates', () => {
    const ids = (v: SearchValue) => filterItems(rows, v, schema, acc).map((r) => r.id);
    expect(ids({ chips: [{ kind: 'filter', field: 'status', values: ['draft', 'approved'], match: 'is' }], text: '' })).toEqual(['a', 'c']);
    expect(ids({ chips: [{ kind: 'filter', field: 'title', values: ['SPINDLE'], match: 'contains' }, { kind: 'quick', key: 'mine' }], text: '' })).toEqual(['a']);
    expect(ids({ chips: [], text: 'coolant' })).toEqual(['b']);
    expect(ids({ chips: [{ kind: 'filter', field: 'updated', values: ['month'], match: 'is' }], text: '' })).toEqual(['a']);
    expect(ids({ chips: [{ kind: 'filter', field: 'updated', values: ['year'], match: 'is' }], text: '' })).toEqual(['a', 'b']);
    expect(ids({ chips: [{ kind: 'group', key: 'status' }], text: '' })).toEqual(['a', 'b', 'c']);
  });

  it('applies a saved search as its own chips', () => {
    const saved: SearchValue = {
      chips: [{ kind: 'saved', id: 's', name: 'Spindle', value: { chips: [{ kind: 'group', key: 'owner' }], text: 'spindle' } }],
      text: '',
    };
    expect(filterItems(rows, saved, schema, acc).map((r) => r.id)).toEqual(['a', 'c']);
    expect(groupKeys(saved)).toEqual(['owner']);
  });

  it('groups in option order, with the rows that have no value last', () => {
    const shuffled = [rows[2], rows[0], rows[1]];
    const byStatus = groupItems(shuffled, ['status'], schema, (r, k) => acc.fieldValue(r, k));
    expect(byStatus.map((g) => g.label)).toEqual(['Draft', 'In review', 'Approved']);
    const byOwner = groupItems(rows, ['owner'], schema, (r, k) => acc.fieldValue(r, k));
    expect(byOwner.map((g) => [g.label, g.rows.length])).toEqual([
      ['Dave', 1],
      ['Sam', 1],
      ['No owner', 1],
    ]);
    const both = groupItems(rows, ['owner', 'status'], schema, (r, k) => acc.fieldValue(r, k));
    expect(both[0].label).toBe('Dave · Draft');
    expect(groupItems(rows, [], schema, () => '')).toEqual([]);
  });
});

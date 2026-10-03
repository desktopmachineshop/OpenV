// Pins the ModuleView filter engine as it behaves today (refactor plan F4),
// quirks included: the search box (substring or whole-word) and the filter
// panel's rows (trimmed, lowercased, gt/lt tried as Number, then Date.parse,
// then as strings, joined by AND or OR).
import { Artifact } from '../api/client';
import { FilterRow, applyComparator, matchesFieldFilters, matchesSearch } from './artifactFilter';

const artifact = (over: Partial<Artifact> = {}): Artifact => ({
  id: 'req-1',
  project_id: 'p1',
  parent_id: null,
  type: 'requirement',
  title: 'The system shall log every login',
  body: '',
  attributes: {},
  version: 3,
  valid_from: '',
  valid_to: null,
  created_at: '2024-01-10T09:00:00Z',
  updated_at: '2024-03-01T12:00:00Z',
  ...over,
});

// Reads a field straight off the artifact; ModuleView passes its own getFieldValue.
const fieldOf = (a: Artifact, field: string): string => String((a as unknown as Record<string, unknown>)[field] ?? '');

const row = (field: string, comparator: string, value: string): FilterRow => ({ field, comparator, value });

describe('matchesSearch', () => {
  it('matches everything for an empty or whitespace-only query, in either mode', () => {
    for (const exact of [false, true]) {
      expect(matchesSearch(artifact(), '', exact)).toBe(true);
      expect(matchesSearch(artifact(), '   ', exact)).toBe(true);
    }
  });

  it('trims the query and ignores case', () => {
    expect(matchesSearch(artifact(), '  SHALL Log  ', false)).toBe(true);
    expect(matchesSearch(artifact(), 'EVERY', false)).toBe(true);
    expect(matchesSearch(artifact(), '  SHALL Log  ', true)).toBe(true);
    expect(matchesSearch(artifact(), 'shall not', false)).toBe(false);
  });

  it('searches the title, body, type, id, parent id and attributes JSON, joined by spaces', () => {
    const a = artifact({ title: 'Alpha', body: 'Beta', parent_id: 'sec-9', attributes: { owner: 'dave' } });
    expect(matchesSearch(a, 'beta', false)).toBe(true);
    expect(matchesSearch(a, 'requirement', false)).toBe(true);
    expect(matchesSearch(a, 'req-1', false)).toBe(true);
    expect(matchesSearch(a, 'sec-9', false)).toBe(true);
    expect(matchesSearch(a, 'dave', false)).toBe(true);
    // The JSON's keys and punctuation are searched too.
    expect(matchesSearch(a, '"owner":"dave"', false)).toBe(true);
    // A query can run across two fields.
    expect(matchesSearch(a, 'alpha beta', false)).toBe(true);
    // An empty body is left out, so the title runs straight into the type.
    expect(matchesSearch(artifact({ title: 'Alpha', body: '' }), 'alpha requirement', false)).toBe(true);
    // project_id, version and the dates are not searched.
    expect(matchesSearch(a, 'p1', false)).toBe(false);
    expect(matchesSearch(a, '2024', false)).toBe(false);
  });

  it('finds a substring inside a longer word unless the search is exact', () => {
    const a = artifact({ title: 'Parts catalog' });
    expect(matchesSearch(a, 'log', false)).toBe(true);
    expect(matchesSearch(a, 'log', true)).toBe(false);
    expect(matchesSearch(a, 'catalog', true)).toBe(true);
    expect(matchesSearch(artifact({ title: 'The LOG entry' }), 'log', true)).toBe(true);
  });

  it('treats ASCII letters, digits and underscores as word characters in an exact search, and nothing else', () => {
    expect(matchesSearch(artifact({ title: 'log_file' }), 'log', true)).toBe(false);
    expect(matchesSearch(artifact({ title: 'log2' }), 'log', true)).toBe(false);
    expect(matchesSearch(artifact({ title: 'see the log.' }), 'log', true)).toBe(true);
    expect(matchesSearch(artifact({ title: 'pre-log' }), 'log', true)).toBe(true);
    // A non-ASCII letter counts as a boundary.
    expect(matchesSearch(artifact({ title: 'café' }), 'caf', true)).toBe(true);
    // The attributes JSON's quotes are boundaries too.
    expect(matchesSearch(artifact({ attributes: { owner: 'dave' } }), 'owner', true)).toBe(true);
  });

  it('takes an exact query literally, regex characters and spaces included', () => {
    expect(matchesSearch(artifact({ title: 'written in c++ code' }), 'c++', true)).toBe(true);
    expect(matchesSearch(artifact({ title: 'abc' }), 'a.c', true)).toBe(false);
    expect(matchesSearch(artifact(), 'shall log', true)).toBe(true);
    expect(matchesSearch(artifact(), 'shall  log', true)).toBe(false);
  });
});

describe('applyComparator', () => {
  it('compares equals and not-equals without case, and without trimming the field', () => {
    expect(applyComparator('Draft', 'equals', 'dRAFT')).toBe(true);
    expect(applyComparator('Draft', 'not-equals', 'draft')).toBe(false);
    expect(applyComparator(' draft', 'equals', 'draft')).toBe(false);
    expect(applyComparator(' draft', 'not-equals', 'draft')).toBe(true);
  });

  it('compares starts-with and ends-with without case', () => {
    expect(applyComparator('Requirement', 'starts-with', 'REQ')).toBe(true);
    expect(applyComparator('Requirement', 'starts-with', 'ment')).toBe(false);
    expect(applyComparator('Requirement', 'ends-with', 'MENT')).toBe(true);
    expect(applyComparator('Requirement', 'ends-with', 'req')).toBe(false);
  });

  it('treats contains, and any comparator it does not know, as a substring test; not-contains as its negation', () => {
    expect(applyComparator('Requirement', 'contains', 'QUIRE')).toBe(true);
    expect(applyComparator('Requirement', 'contains', 'test')).toBe(false);
    expect(applyComparator('Requirement', 'no-such-comparator', 'quire')).toBe(true);
    expect(applyComparator('Requirement', 'not-contains', 'QUIRE')).toBe(false);
    expect(applyComparator('Requirement', 'not-contains', 'test')).toBe(true);
    expect(applyComparator('Requirement', 'contains', '')).toBe(true);
  });

  it('compares gt and lt as numbers when both sides are numbers', () => {
    // As strings, "10" < "9".
    expect(applyComparator('10', 'gt', '9')).toBe(true);
    expect(applyComparator('10', 'lt', '9')).toBe(false);
    expect(applyComparator('9', 'lt', '10')).toBe(true);
    expect(applyComparator('3', 'gt', '3')).toBe(false);
    expect(applyComparator('3', 'lt', '3')).toBe(false);
    // Number() trims and reads hex, and reads an empty field as 0.
    expect(applyComparator(' 12 ', 'gt', '11')).toBe(true);
    expect(applyComparator('0x10', 'gt', '15')).toBe(true);
    expect(applyComparator('', 'gt', '-1')).toBe(true);
    expect(applyComparator('', 'lt', '1')).toBe(true);
  });

  it('tries numbers before dates', () => {
    // Date.parse reads "12" as December 2001, after "2000"; as numbers 12 < 2000.
    expect(Date.parse('12')).toBeGreaterThan(Date.parse('2000'));
    expect(applyComparator('12', 'lt', '2000')).toBe(true);
    expect(applyComparator('12', 'gt', '2000')).toBe(false);
  });

  it('compares gt and lt as dates when both sides parse as dates', () => {
    expect(applyComparator('2024-03-01T00:00:00Z', 'gt', '2024-01-01')).toBe(true);
    expect(applyComparator('2024-03-01T00:00:00Z', 'lt', '2024-01-01')).toBe(false);
    // The panel hands over a lowercased value; Date.parse still reads it.
    expect(applyComparator('2024-03-01T00:00:00Z', 'lt', '2024-06-01t00:00:00z')).toBe(true);
  });

  it('tries dates before strings', () => {
    // 10:00+05:00 is 05:00Z, before 08:00Z, though "10" sorts after "08" as text.
    expect(applyComparator('2024-03-01T10:00:00+05:00', 'gt', '2024-03-01T08:00:00Z')).toBe(false);
    expect(applyComparator('2024-03-01T10:00:00+05:00', 'lt', '2024-03-01T08:00:00Z')).toBe(true);
    // Date.parse reads "5" as May 2001.
    expect(applyComparator('5', 'gt', '2024-01-01')).toBe(false);
  });

  it('falls back to comparing lowercased strings', () => {
    // "B" < "a" in code units, "b" > "a".
    expect(applyComparator('Beta', 'gt', 'alpha')).toBe(true);
    expect(applyComparator('alpha', 'lt', 'BETA')).toBe(true);
    // Lexicographic, so "v10" is not after "v9".
    expect(applyComparator('v10', 'gt', 'v9')).toBe(false);
    expect(applyComparator('The Plan', 'gt', 'the p')).toBe(true);
    // An empty field is before every date.
    expect(applyComparator('', 'lt', '2024-01-01')).toBe(true);
    expect(applyComparator('', 'gt', '2024-01-01')).toBe(false);
  });
});

describe('matchesFieldFilters', () => {
  it('matches everything when no row has a value, under AND and OR, and reads no field', () => {
    const getFieldValue = vi.fn(fieldOf);
    for (const logic of ['and', 'or'] as const) {
      expect(matchesFieldFilters(artifact(), [], logic, getFieldValue)).toBe(true);
      expect(matchesFieldFilters(artifact(), [row('type', 'equals', ''), row('title', 'equals', '   ')], logic, getFieldValue)).toBe(true);
    }
    expect(getFieldValue).not.toHaveBeenCalled();
  });

  it('ignores blank rows among active ones', () => {
    const rows = [row('type', 'equals', 'requirement'), row('title', 'equals', '  ')];
    expect(matchesFieldFilters(artifact(), rows, 'and', fieldOf)).toBe(true);
    expect(matchesFieldFilters(artifact(), [row('type', 'equals', 'heading'), row('title', 'equals', '  ')], 'or', fieldOf)).toBe(false);
  });

  it('trims and lowercases each row value', () => {
    expect(matchesFieldFilters(artifact(), [row('type', 'equals', '  REQUIREMENT  ')], 'and', fieldOf)).toBe(true);
    expect(matchesFieldFilters(artifact(), [row('type', 'ends-with', 'MENT ')], 'and', fieldOf)).toBe(true);
    expect(matchesFieldFilters(artifact(), [row('version', 'gt', ' 2 ')], 'and', fieldOf)).toBe(true);
  });

  it('needs every active row under AND and any under OR', () => {
    const yes = row('type', 'equals', 'requirement');
    const no = row('type', 'equals', 'heading');
    expect(matchesFieldFilters(artifact(), [yes, no], 'and', fieldOf)).toBe(false);
    expect(matchesFieldFilters(artifact(), [yes, no], 'or', fieldOf)).toBe(true);
    expect(matchesFieldFilters(artifact(), [yes, yes], 'and', fieldOf)).toBe(true);
    expect(matchesFieldFilters(artifact(), [no, no], 'or', fieldOf)).toBe(false);
  });

  it('reads every active row through getFieldValue, in order, even after a failing row', () => {
    const getFieldValue = vi.fn(fieldOf);
    const a = artifact();
    const rows = [row('type', 'equals', 'heading'), row('title', 'equals', ''), row('version', 'gt', '1')];
    expect(matchesFieldFilters(a, rows, 'and', getFieldValue)).toBe(false);
    expect(getFieldValue.mock.calls).toEqual([
      [a, 'type'],
      [a, 'version'],
    ]);
  });

  it('applies the number, date and string orderings the snapshot test types in', () => {
    const rows = [
      row('version', 'gt', '1'),
      row('updated_at', 'lt', '2024-06-01T00:00:00Z'),
      row('title', 'gt', 'The P'),
    ];
    expect(matchesFieldFilters(artifact({ title: 'The system' }), rows, 'and', fieldOf)).toBe(true);
    expect(matchesFieldFilters(artifact({ title: 'The system', version: 1 }), rows, 'and', fieldOf)).toBe(false);
    expect(matchesFieldFilters(artifact({ title: 'The system', updated_at: '2024-07-01T00:00:00Z' }), rows, 'and', fieldOf)).toBe(false);
    expect(matchesFieldFilters(artifact({ title: 'The Index' }), rows, 'and', fieldOf)).toBe(false);
  });
});

import { enumFacet } from './GlobalSearchChips';
import type { SearchSchema, SearchValue } from './search/searchValue';

// The top-bar search sends project, type and status as exact values; a
// "contains" chip or one naming a label is turned into those values first.
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
  ],
};
const value = (match: 'is' | 'contains', ...values: string[]): SearchValue => ({
  text: '',
  chips: [{ kind: 'filter', field: 'status', values, match }],
});

describe('enumFacet', () => {
  it('keeps an exact value', () => {
    expect(enumFacet(schema, value('is', 'draft'), 'status')).toEqual(['draft']);
  });
  it('reads a label as its value', () => {
    expect(enumFacet(schema, value('is', 'in review'), 'status')).toEqual(['in_review']);
  });
  it('turns contains into every value whose label or value holds the text', () => {
    expect(enumFacet(schema, value('contains', 'r'), 'status').sort()).toEqual(['approved', 'draft', 'in_review']);
    expect(enumFacet(schema, value('contains', 'REV'), 'status')).toEqual(['in_review']);
  });
  it('sends text that matches no value as it is, which finds nothing', () => {
    expect(enumFacet(schema, value('contains', 'allow'), 'status')).toEqual(['allow']);
  });
});

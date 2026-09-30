import { describe, expect, it } from 'vitest';
import type { EvidenceCitation } from '../api/client';
import { moveCitations } from './testRunCitations';

const citation = (id: string, resultId: string): EvidenceCitation => ({
  id,
  bundle_id: `bundle-${id}`,
  test_result_id: resultId,
  note: '',
  created_at: '2026-09-30T08:00:00Z',
  bundle_ref: `EVD-${id}`,
});

describe('moveCitations', () => {
  it('moves the citations of a superseded result to the result that replaced it', () => {
    const before = { 'result-1': [citation('1', 'result-1')], 'result-2': [citation('2', 'result-2')] };
    const after = moveCitations(before, 'result-1', 'result-3');
    expect(after['result-1']).toBeUndefined();
    expect(after['result-3']).toEqual([{ ...citation('1', 'result-1'), test_result_id: 'result-3' }]);
    // Another case's evidence is left where it is.
    expect(after['result-2']).toBe(before['result-2']);
  });

  it('leaves the map as it is when there is nothing to move', () => {
    const before = { 'result-2': [citation('2', 'result-2')] };
    expect(moveCitations(before, 'result-1', 'result-3')).toBe(before);
    expect(moveCitations(before, 'result-2', 'result-2')).toBe(before);
  });
});

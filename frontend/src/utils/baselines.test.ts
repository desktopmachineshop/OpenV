import { baselineAuthor, baselineDate, baselineLabel, runBaselineLabel } from './baselines';
import { Baseline } from '../api/client';

const baseline = (over: Partial<Baseline> = {}): Baseline => ({
  id: 'b1',
  project_id: 'p1',
  name: 'Design freeze — rev A',
  created_at: '2026-09-12T12:54:32.680Z',
  ...over,
});

describe('reading a baseline in a list', () => {
  it('names it, dates it and says who took it', () => {
    const label = baselineLabel(baseline({ created_by: 'u1', created_by_name: 'Ada Lovelace' }));
    expect(label).toContain('Design freeze — rev A');
    expect(label).toContain('2026');
    expect(label).toContain('Ada Lovelace');
  });

  // Every baseline captured before authorship was recorded has no author, and
  // neither does one taken by an automation. Showing "Unknown" would read as a
  // person whose name failed to load.
  it('says nothing at all when nobody is recorded', () => {
    const label = baselineLabel(baseline());
    expect(label).not.toMatch(/unknown|null|undefined/i);
    expect(label.endsWith('·')).toBe(false);
    expect(baselineAuthor(baseline())).toBe('');
    expect(baselineAuthor(baseline({ created_by_name: '   ' }))).toBe('');
  });

  it('keeps the name first, so the list is scannable by name', () => {
    expect(baselineLabel(baseline({ created_by_name: 'Ada' })).indexOf('Design freeze')).toBe(0);
  });

  // A malformed timestamp must not put "Invalid Date" in front of a reader.
  it('drops a date it cannot read rather than printing rubbish', () => {
    expect(baselineDate('not a date')).toBe('');
    const label = baselineLabel(baseline({ created_at: 'not a date', created_by_name: 'Ada' }));
    expect(label).toBe('Design freeze — rev A · Ada');
  });
});

// #379's question 23: deleting a baseline leaves the runs that named it with
// its id, as history (REQ-13), marked baseline_deleted by the server. The
// runs list said the bare id, which leads nowhere; it now says the baseline
// was deleted.
describe("reading a test run's baseline", () => {
  const baselines = [baseline()];

  it('says Live for a run on no baseline', () => {
    expect(runBaselineLabel({}, baselines)).toBe('Live');
    expect(runBaselineLabel({ baseline_id: null }, baselines)).toBe('Live');
  });

  it("names a baseline the project still has", () => {
    expect(runBaselineLabel({ baseline_id: 'b1' }, baselines)).toBe('Design freeze — rev A');
  });

  it('says the baseline was deleted, not its id', () => {
    const label = runBaselineLabel({ baseline_id: 'b-gone', baseline_deleted: true }, baselines);
    expect(label).toBe('Baseline deleted');
    expect(label).not.toContain('b-gone');
  });
});

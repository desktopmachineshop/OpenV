import { GAP_LABELS, gapLabel } from './vvGapLabels';

describe('gapLabel', () => {
  it('titles the unverified bucket by the methods it covers', () => {
    // REQ-12: a requirement verified by demonstration, analysis or
    // inspection and not yet attested to has no test case to be missing,
    // so the gaps view needs its own section for it.
    expect(gapLabel('requirements_unverified')).toBe(
      'Unverified (demonstration, analysis, inspection)'
    );
  });

  it('titles every bucket the gaps endpoint returns', () => {
    const buckets = [
      'requirements_without_method',
      'requirements_without_test_case',
      'requirements_unverified',
      'requirements_failing',
      'orphan_test_cases',
      'needs_without_requirement',
      'hazards_unmitigated',
    ];
    buckets.forEach((bucket) => {
      expect(GAP_LABELS[bucket]).toBeTruthy();
      expect(gapLabel(bucket)).toBe(GAP_LABELS[bucket]);
    });
  });

  it('falls back to a readable heading for an unknown bucket', () => {
    expect(gapLabel('some_new_bucket')).toBe('Some new bucket');
    expect(gapLabel('some-new-bucket')).toBe('Some new bucket');
    expect(gapLabel('')).toBe('');
  });
});

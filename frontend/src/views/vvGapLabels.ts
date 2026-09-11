// Human titles for the buckets of GET /api/v1/projects/{id}/vv/gaps.
//
// The dashboard renders whatever buckets the server sends, so an unknown key
// still gets a readable heading — but the known ones are spelled out here so
// the wording matches the PDF V&V report rather than being derived from the
// JSON key. `requirements_unverified` is the bucket for requirements whose
// verification method is demonstration, analysis or inspection and which
// nothing has yet attested to: no test case can ever cover them, so they
// would otherwise be invisible in this view while the coverage rollup
// already counts them as uncovered.
export const GAP_LABELS: Record<string, string> = {
  requirements_without_method: 'Requirements without a verification method',
  requirements_without_test_case: 'Requirements without a test case',
  requirements_unverified: 'Unverified (demonstration, analysis, inspection)',
  requirements_failing: 'Requirements with failing tests',
  orphan_test_cases: 'Orphan test cases (verify nothing)',
  needs_without_requirement: 'User needs without a derived requirement',
  hazards_unmitigated: 'Unmitigated hazards',
};

// gapLabel titles one gap bucket. Unknown keys — a bucket added server-side
// before this table catches up — fall back to the key with its separators
// turned into spaces and the first letter capitalized.
export const gapLabel = (key: string): string => {
  const known = GAP_LABELS[key];
  if (known) return known;
  const words = key.replace(/[-_]/g, ' ').trim();
  if (!words) return key;
  return words.charAt(0).toUpperCase() + words.slice(1);
};

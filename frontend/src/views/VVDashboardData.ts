// What the coverage page (VVDashboard) derives from the coverage report,
// the gap report and the project's artifacts: the rollup counts, the
// requirements grouped by the document section they sit in, and the gaps
// in the order worth fixing them.
import type { Artifact, CoverageEntry } from '../api/client';
import { gapLabel } from './vvGapLabels';

export interface RollupCounts {
  total: number;
  /** pass and verified-manually */
  pass: number;
  fail: number;
  blocked: number;
  unrun: number;
  uncovered: number;
  methodMissing: number;
  /** Anything else the server sends. */
  other: number;
}

export const countRollups = (entries: CoverageEntry[]): RollupCounts => {
  const c: RollupCounts = { total: 0, pass: 0, fail: 0, blocked: 0, unrun: 0, uncovered: 0, methodMissing: 0, other: 0 };
  entries.forEach((e) => {
    c.total++;
    switch (e.rollup) {
      case 'pass':
      case 'verified-manually':
        c.pass++;
        break;
      case 'fail':
        c.fail++;
        break;
      case 'blocked':
        c.blocked++;
        break;
      case 'unrun':
        c.unrun++;
        break;
      case 'uncovered':
        c.uncovered++;
        break;
      case 'method-missing':
        c.methodMissing++;
        break;
      default:
        c.other++;
    }
  });
  return c;
};

/** A requirement is traced once it has a way to be verified: anything but uncovered or no method. */
export const tracedCount = (c: RollupCounts): number => c.total - c.uncovered - c.methodMissing;

export const percent = (part: number, whole: number): number => (whole > 0 ? Math.round((part / whole) * 100) : 0);

export interface Section {
  key: string;
  name: string;
  entries: CoverageEntry[];
  counts: RollupCounts;
}

export const NO_SECTION = 'No section';

/**
 * The section a requirement sits in: its heading ancestors, outermost
 * first, the first two of them joined ("System · Braking"). A requirement
 * under no heading, or one the live project no longer has (a baseline
 * view), is in no section.
 */
export const sectionOf = (
  requirementId: string,
  artifacts: Record<string, Artifact>
): { key: string; name: string } => {
  const headings: Artifact[] = [];
  let cur = artifacts[requirementId];
  const seen = new Set<string>();
  while (cur && cur.parent_id && !seen.has(cur.id) && seen.size < 64) {
    seen.add(cur.id);
    const parent: Artifact | undefined = artifacts[cur.parent_id];
    if (!parent) break;
    if (parent.type === 'heading') headings.unshift(parent);
    cur = parent;
  }
  if (headings.length === 0) return { key: '', name: NO_SECTION };
  const top = headings.slice(0, 2);
  return {
    key: top[top.length - 1].id,
    name: top.map((h) => h.title || h.ref || h.id).join(' · '),
  };
};

/** The report's requirements grouped by section, in the order the report lists them. */
export const groupBySection = (entries: CoverageEntry[], artifacts: Record<string, Artifact>): Section[] => {
  const order: string[] = [];
  const byKey: Record<string, { name: string; entries: CoverageEntry[] }> = {};
  entries.forEach((e) => {
    const { key, name } = sectionOf(e.requirement_id, artifacts);
    if (!byKey[key]) {
      byKey[key] = { name, entries: [] };
      order.push(key);
    }
    byKey[key].entries.push(e);
  });
  // Requirements under no heading go last.
  const sorted = order.filter((k) => k !== '').concat(order.includes('') ? [''] : []);
  return sorted.map((key) => ({
    key,
    name: byKey[key].name,
    entries: byKey[key].entries,
    counts: countRollups(byKey[key].entries),
  }));
};

// The gap buckets in the order a person should work through them, each
// with the short reason a row of the top-gaps list gives.
const GAP_ORDER: { key: string; why: string }[] = [
  { key: 'requirements_failing', why: 'failing' },
  { key: 'requirements_without_method', why: 'no method' },
  { key: 'requirements_without_test_case', why: 'no test' },
  { key: 'requirements_unverified', why: 'unverified' },
  { key: 'hazards_unmitigated', why: 'unmitigated' },
  { key: 'needs_without_requirement', why: 'no requirement' },
  { key: 'orphan_test_cases', why: 'verifies nothing' },
];

export interface GapItem {
  id: string;
  bucket: string;
  why: string;
}

export interface GapGroup {
  key: string;
  label: string;
  why: string;
  ids: string[];
}

/** The non-empty gap buckets, known ones in working order and unknown ones after. */
export const gapGroups = (gaps: Record<string, string[]>): GapGroup[] => {
  const known = GAP_ORDER.map((g) => g.key);
  const keys = [
    ...known.filter((k) => (gaps[k] || []).length > 0),
    ...Object.keys(gaps).filter((k) => !known.includes(k) && (gaps[k] || []).length > 0),
  ];
  return keys.map((key) => ({
    key,
    label: gapLabel(key),
    why: GAP_ORDER.find((g) => g.key === key)?.why || gapLabel(key).toLowerCase(),
    ids: gaps[key] || [],
  }));
};

/** One row per artifact, in the first bucket that names it. */
export const flattenGaps = (groups: GapGroup[]): GapItem[] => {
  const seen = new Set<string>();
  const out: GapItem[] = [];
  groups.forEach((g) =>
    g.ids.forEach((id) => {
      if (seen.has(id)) return;
      seen.add(id);
      out.push({ id, bucket: g.key, why: g.why });
    })
  );
  return out;
};

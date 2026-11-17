// The Design page's model: the project's design items as a tree, and what one
// item is linked to. Pure functions over what the page loads, so they are
// tested without a router.
import type { Artifact, CoverageReport, Link } from '../api/client';

export const DESIGN_TYPE = 'design-item';

export interface TreeNode {
  item: Artifact;
  depth: number;
  /** Design items beneath it, at any depth. */
  descendants: number;
  hasChildren: boolean;
}

/**
 * The children of each design item, by its id; '' holds the roots: the items
 * whose parent is not a design item (a heading, or nothing). Siblings keep
 * the order the API serves, which is the document's.
 */
export const childrenOf = (items: Artifact[]): Map<string, Artifact[]> => {
  const ids = new Set(items.map((i) => i.id));
  const out = new Map<string, Artifact[]>();
  for (const item of items) {
    const parent = item.parent_id && ids.has(item.parent_id) ? item.parent_id : '';
    out.set(parent, [...(out.get(parent) || []), item]);
  }
  return out;
};

const countBelow = (id: string, kids: Map<string, Artifact[]>): number =>
  (kids.get(id) || []).reduce((n, c) => n + 1 + countBelow(c.id, kids), 0);

/** The ids on the way from a root down to this item, the item last. */
export const pathTo = (id: string, items: Artifact[]): string[] => {
  const byId = new Map(items.map((i) => [i.id, i]));
  const path: string[] = [];
  let cur = byId.get(id);
  const seen = new Set<string>();
  while (cur && !seen.has(cur.id)) {
    seen.add(cur.id);
    path.unshift(cur.id);
    cur = cur.parent_id ? byId.get(cur.parent_id) : undefined;
  }
  return path;
};

const matches = (a: Artifact, q: string) => `${a.ref || ''} ${a.title}`.toLowerCase().includes(q);

/**
 * The rows the tree draws: depth-first, skipping what is under a collapsed
 * item. With a filter, only the items that match and the items above them,
 * all expanded.
 */
export const visibleRows = (items: Artifact[], collapsed: Set<string>, filter: string): TreeNode[] => {
  const kids = childrenOf(items);
  const q = filter.trim().toLowerCase();
  let keep: Set<string> | null = null;
  if (q) {
    keep = new Set();
    for (const a of items) if (matches(a, q)) for (const id of pathTo(a.id, items)) keep.add(id);
  }
  const rows: TreeNode[] = [];
  const walk = (parent: string, depth: number) => {
    for (const item of kids.get(parent) || []) {
      if (keep && !keep.has(item.id)) continue;
      const below = kids.get(item.id) || [];
      rows.push({ item, depth, descendants: countBelow(item.id, kids), hasChildren: below.length > 0 });
      if (below.length && (keep || !collapsed.has(item.id))) walk(item.id, depth + 1);
    }
  };
  walk('', 0);
  return rows;
};

export interface Traced {
  artifact: Artifact;
  /** The link's type, as the item reads it: satisfies, mitigates, … */
  type: string;
  suspect: boolean;
}

export interface Verification {
  testCase: Artifact;
  requirement: Artifact;
  result: string;
}

/**
 * What a design item traces to (its outgoing links: the requirements it
 * satisfies, the hazards it mitigates), and the test cases that verify those
 * requirements with each one's latest result. Tests verify requirements, not
 * design items, so this is the evidence that reaches the item through them.
 */
export const traceOf = (
  id: string,
  artifacts: Artifact[],
  links: Link[],
  coverage: Pick<CoverageReport, 'entries'> | null
): { traces: Traced[]; tracedFrom: Traced[]; verifications: Verification[] } => {
  const byId = new Map(artifacts.map((a) => [a.id, a]));
  const traces: Traced[] = [];
  const tracedFrom: Traced[] = [];
  for (const l of links) {
    if (l.from_id === id && byId.has(l.to_id)) traces.push({ artifact: byId.get(l.to_id)!, type: l.type, suspect: !!l.suspect });
    else if (l.to_id === id && byId.has(l.from_id))
      tracedFrom.push({ artifact: byId.get(l.from_id)!, type: l.type, suspect: !!l.suspect });
  }
  const latest = new Map<string, Record<string, string>>();
  for (const e of coverage?.entries || []) latest.set(e.requirement_id, e.latest_results || {});
  const verifications: Verification[] = [];
  const seen = new Set<string>();
  for (const t of traces) {
    if (t.artifact.type !== 'requirement') continue;
    for (const l of links) {
      if (l.type !== 'verifies' || l.to_id !== t.artifact.id) continue;
      const tc = byId.get(l.from_id);
      if (!tc || seen.has(tc.id)) continue;
      seen.add(tc.id);
      verifications.push({ testCase: tc, requirement: t.artifact, result: latest.get(t.artifact.id)?.[tc.id] || 'not-run' });
    }
  }
  return { traces, tracedFrom, verifications };
};

/** The attributes worth a row: everything but the deprecated status mirror and empty values. */
export const attributeRows = (a: Artifact): [string, string][] =>
  Object.entries(a.attributes || {})
    .filter(([k, v]) => k !== 'status' && v !== null && v !== undefined && v !== '')
    .map(([k, v]) => [k.replace(/_/g, ' '), typeof v === 'object' ? JSON.stringify(v) : String(v)]);

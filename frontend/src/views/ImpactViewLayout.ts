import type { ImpactGroup, ImpactNode, ImpactReport } from '../api/client';

// The impact tree: the selected requirement in the middle, what it depends
// on fanning out to the left (upstream) and what depends on it fanning out
// to the right (downstream). Each node hangs off the artifact before it on
// its shortest path, so the tree is the breadth-first walk the server made.

export type ImpactDepth = 'one' | 'all';
export type ImpactSide = 'upstream' | 'downstream';

export interface TreeNode {
  id: string;
  title: string;
  type: string;
  via: string;
  side: ImpactSide;
  /** Hops from the seed: 1 is a direct link. */
  depth: number;
  /** Row in the tree, centre-aligned; fractional for a parent between children. */
  row: number;
  /** The node this one hangs off: the seed for a direct link. */
  parentId: string;
}

export interface ImpactTreeLayout {
  seedRow: number;
  rows: number;
  upstreamDepth: number;
  downstreamDepth: number;
  nodes: TreeNode[];
}

// Spine order, as the server groups them: needs, requirements, design,
// tests, hazards, personas, then the rest.
const TYPE_RANK: Record<string, number> = {
  'user-need': 0,
  requirement: 1,
  'design-item': 2,
  'test-case': 3,
  hazard: 4,
  persona: 5,
};
const rank = (t: string) => TYPE_RANK[t] ?? 100;

const flatten = (groups: ImpactGroup[], depth: ImpactDepth): ImpactNode[] =>
  groups
    .flatMap((g) => g.nodes)
    .filter((n) => depth === 'all' || n.distance === 1);

// layoutSide places one side's nodes on rows by a depth-first walk: each
// leaf takes the next row and a parent sits midway between its first and
// last child, so every branch reads as a block.
function layoutSide(
  seedId: string,
  side: ImpactSide,
  source: ImpactNode[],
  titleOf: (id: string) => string
): { nodes: TreeNode[]; rows: number; depth: number } {
  const children: Record<string, ImpactNode[]> = {};
  const present = new Set(source.map((n) => n.artifact_id));
  source.forEach((n) => {
    const parent = n.path.length >= 2 ? n.path[n.path.length - 2] : seedId;
    // A parent outside the shown set (never for a breadth-first walk) falls
    // back to the seed so no node is lost.
    const key = parent === seedId || present.has(parent) ? parent : seedId;
    (children[key] ||= []).push(n);
  });
  Object.values(children).forEach((list) =>
    list.sort(
      (a, b) =>
        rank(a.type) - rank(b.type) ||
        (a.title || titleOf(a.artifact_id)).localeCompare(b.title || titleOf(b.artifact_id)) ||
        a.artifact_id.localeCompare(b.artifact_id)
    )
  );

  const out: TreeNode[] = [];
  let next = 0;
  let maxDepth = 0;
  const visit = (n: ImpactNode, parentId: string, depth: number): number => {
    maxDepth = Math.max(maxDepth, depth);
    const node: TreeNode = {
      id: n.artifact_id,
      title: n.title || titleOf(n.artifact_id),
      type: n.type,
      via: n.via,
      side,
      depth,
      row: 0,
      parentId,
    };
    out.push(node);
    const kids = children[n.artifact_id] || [];
    if (kids.length === 0) {
      node.row = next++;
    } else {
      const rows = kids.map((k) => visit(k, n.artifact_id, depth + 1));
      node.row = (rows[0] + rows[rows.length - 1]) / 2;
    }
    return node.row;
  };
  (children[seedId] || []).forEach((n) => visit(n, seedId, 1));
  return { nodes: out, rows: next, depth: maxDepth };
}

export function layoutImpactTree(
  report: ImpactReport,
  depth: ImpactDepth,
  titleOf: (id: string) => string = (id) => id
): ImpactTreeLayout {
  const seedId = report.artifact_id;
  const up = layoutSide(seedId, 'upstream', flatten(report.upstream, depth), titleOf);
  const down = layoutSide(seedId, 'downstream', flatten(report.downstream, depth), titleOf);
  const rows = Math.max(up.rows, down.rows, 1);
  // Centre the shorter side against the taller one.
  const shift = (nodes: TreeNode[], sideRows: number) =>
    nodes.map((n) => ({ ...n, row: n.row + (rows - sideRows) / 2 }));
  return {
    seedRow: (rows - 1) / 2,
    rows,
    upstreamDepth: up.depth,
    downstreamDepth: down.depth,
    nodes: [...shift(up.nodes, up.rows), ...shift(down.nodes, down.rows)],
  };
}

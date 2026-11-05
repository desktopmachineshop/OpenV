// The tree diagram's layout: boxes on a grid, one column per level, and
// elbow connectors from each parent to its children.
//
// Two shapes share it. A forest flows left to right, roots in the first
// column (the requirements Tree view). A two-sided tree puts one root in
// the middle, with one forest branching to its left and another to its
// right (impact analysis: upstream on the left, downstream on the right).
//
// Only what is unfolded is laid out: a folded node is a leaf here, so a
// project of hundreds of artifacts opened to two levels places only those
// two levels. Every leaf takes the next row and a parent sits midway between
// its first and last child, so each branch reads as a block. Pure: the same
// shape, folds and metrics always give the same layout.

export interface DiagramEdgeStyle {
  /** A label drawn on the connector, near the child (a link type). */
  label?: string;
  /** The connector's colour, a CSS colour or token (var(--purple)). */
  color?: string;
  dashed?: boolean;
}

export interface DiagramNode<T = unknown> {
  /** Unique in the diagram; the same artifact may appear twice under two keys. */
  key: string;
  data: T;
  children: DiagramNode<T>[];
  /** How the connector from this node's parent to it is drawn. */
  edge?: DiagramEdgeStyle;
}

export type DiagramShape<T = unknown> =
  | { kind: 'forest'; roots: DiagramNode<T>[] }
  | { kind: 'two-sided'; root: DiagramNode<T>; left: DiagramNode<T>[]; right: DiagramNode<T>[] };

export type DiagramSide = 'root' | 'left' | 'right';

export interface DiagramMetrics {
  boxWidth: number;
  boxHeight: number;
  /** Between columns; the connector's elbow sits in the middle of it. */
  gapX: number;
  /** Between rows. */
  gapY: number;
  /** Around the whole diagram. */
  pad: number;
}

export const DEFAULT_METRICS: DiagramMetrics = { boxWidth: 220, boxHeight: 64, gapX: 56, gapY: 14, pad: 24 };

export interface PlacedNode<T = unknown> {
  node: DiagramNode<T>;
  key: string;
  x: number;
  y: number;
  /** 1 for a root (or the centre of a two-sided tree), as aria-level. */
  level: number;
  side: DiagramSide;
  parentKey: string | null;
  /** The keys of the children laid out (none while folded). */
  childKeys: string[];
  hasChildren: boolean;
  folded: boolean;
  /** Everything below a folded node, for its ⊕ badge. */
  hiddenCount: number;
  /** Position among its siblings, 1-based, and how many there are. */
  posInSet: number;
  setSize: number;
}

export interface PlacedEdge {
  key: string;
  from: string;
  to: string;
  /** An SVG path: out of the parent, along to the elbow, down or up, into the child. */
  path: string;
  style?: DiagramEdgeStyle;
  labelX: number;
  labelY: number;
  /** The connector's bounds, for culling. */
  box: { x: number; y: number; w: number; h: number };
}

export interface DiagramLayout<T = unknown> {
  /** In tree order: depth first, a parent before its children. */
  nodes: PlacedNode<T>[];
  edges: PlacedEdge[];
  byKey: Map<string, PlacedNode<T>>;
  width: number;
  height: number;
}

const countBelow = (node: DiagramNode<unknown>): number =>
  node.children.reduce((n, child) => n + 1 + countBelow(child), 0);

interface SidePlacement<T> {
  placed: Array<{ p: PlacedNode<T>; row: number; depth: number }>;
  rows: number;
  maxDepth: number;
  /** The midpoint of the top-level nodes' rows, which the centre aligns to. */
  centre: number;
  topKeys: string[];
}

// One forest's rows and depths. Depth starts at `firstDepth` (0 for a plain
// forest, 1 for a side of a two-sided tree) and levels at firstDepth + 1.
function placeForest<T>(
  roots: DiagramNode<T>[],
  side: DiagramSide,
  firstDepth: number,
  parentKey: string | null,
  folded: ReadonlySet<string>
): SidePlacement<T> {
  const placed: SidePlacement<T>['placed'] = [];
  let next = 0;
  let maxDepth = 0;
  const seen = new Set<string>();
  const visit = (node: DiagramNode<T>, depth: number, parent: string | null, pos: number, size: number): number => {
    const isFolded = node.children.length > 0 && folded.has(node.key);
    // A key seen twice would draw two boxes on one id; the second is skipped.
    const open = isFolded ? [] : node.children.filter((c) => !seen.has(c.key));
    open.forEach((c) => seen.add(c.key));
    const p: PlacedNode<T> = {
      node,
      key: node.key,
      x: 0,
      y: 0,
      level: depth + 1,
      side,
      parentKey: parent,
      childKeys: open.map((c) => c.key),
      hasChildren: node.children.length > 0,
      folded: isFolded,
      hiddenCount: isFolded ? countBelow(node) : 0,
      posInSet: pos,
      setSize: size,
    };
    const entry = { p, row: 0, depth };
    placed.push(entry);
    maxDepth = Math.max(maxDepth, depth);
    if (open.length === 0) {
      entry.row = next++;
    } else {
      const rows = open.map((child, i) => visit(child, depth + 1, node.key, i + 1, open.length));
      entry.row = (rows[0] + rows[rows.length - 1]) / 2;
    }
    return entry.row;
  };
  const tops = roots.filter((r) => !seen.has(r.key));
  tops.forEach((r) => seen.add(r.key));
  const topRows = tops.map((r, i) => visit(r, firstDepth, parentKey, i + 1, tops.length));
  return {
    placed,
    rows: next,
    maxDepth,
    centre: topRows.length ? (topRows[0] + topRows[topRows.length - 1]) / 2 : 0,
    topKeys: tops.map((t) => t.key),
  };
}

function elbow(parent: PlacedNode, child: PlacedNode, m: DiagramMetrics): Omit<PlacedEdge, 'key' | 'from' | 'to' | 'style'> {
  const leftward = child.side === 'left';
  const y1 = parent.y + m.boxHeight / 2;
  const y2 = child.y + m.boxHeight / 2;
  const x1 = leftward ? parent.x : parent.x + m.boxWidth;
  const x2 = leftward ? child.x + m.boxWidth : child.x;
  const mx = (x1 + x2) / 2;
  return {
    path: `M${x1} ${y1}H${mx}V${y2}H${x2}`,
    labelX: (mx + x2) / 2,
    labelY: y2 - 4,
    box: { x: Math.min(x1, x2), y: Math.min(y1, y2), w: Math.abs(x2 - x1), h: Math.abs(y2 - y1) },
  };
}

/** Lays out what is unfolded of `shape`. */
export function layoutDiagram<T>(
  shape: DiagramShape<T>,
  folded: ReadonlySet<string>,
  metrics: DiagramMetrics = DEFAULT_METRICS
): DiagramLayout<T> {
  const m = metrics;
  const colW = m.boxWidth + m.gapX;
  const rowH = m.boxHeight + m.gapY;
  const nodes: PlacedNode<T>[] = [];
  let columns = 0;
  let rows = 0;

  if (shape.kind === 'forest') {
    const f = placeForest(shape.roots, 'right', 0, null, folded);
    f.placed.forEach(({ p, row, depth }) => {
      p.x = m.pad + depth * colW;
      p.y = m.pad + row * rowH;
      nodes.push(p);
    });
    columns = f.placed.length ? f.maxDepth + 1 : 0;
    rows = f.rows;
  } else {
    const { root } = shape;
    const rootFolded = folded.has(root.key);
    const left = placeForest(rootFolded ? [] : shape.left, 'left', 1, root.key, folded);
    const right = placeForest(rootFolded ? [] : shape.right, 'right', 1, root.key, folded);
    const leftCols = left.placed.length ? left.maxDepth : 0;
    const rightCols = right.placed.length ? right.maxDepth : 0;
    // Line the centre up with the middle of both sides' top-level nodes.
    const centre = Math.max(left.placed.length ? left.centre : 0, right.placed.length ? right.centre : 0);
    const shiftL = centre - left.centre;
    const shiftR = centre - right.centre;
    const rootNode: PlacedNode<T> = {
      node: root,
      key: root.key,
      x: m.pad + leftCols * colW,
      y: m.pad + centre * rowH,
      level: 1,
      side: 'root',
      parentKey: null,
      childKeys: [...left.topKeys, ...right.topKeys],
      hasChildren: shape.left.length + shape.right.length > 0,
      folded: rootFolded && shape.left.length + shape.right.length > 0,
      hiddenCount: rootFolded ? [...shape.left, ...shape.right].reduce((n, c) => n + 1 + countBelow(c), 0) : 0,
      posInSet: 1,
      setSize: 1,
    };
    nodes.push(rootNode);
    // Siblings of the centre's two sides form one set for a screen reader.
    const topCount = left.topKeys.length + right.topKeys.length;
    left.placed.forEach(({ p, row, depth }) => {
      p.x = m.pad + (leftCols - depth) * colW;
      p.y = m.pad + (row + shiftL) * rowH;
      if (depth === 1) p.setSize = topCount;
      nodes.push(p);
    });
    right.placed.forEach(({ p, row, depth }) => {
      p.x = m.pad + (leftCols + depth) * colW;
      p.y = m.pad + (row + shiftR) * rowH;
      if (depth === 1) {
        p.setSize = topCount;
        p.posInSet += left.topKeys.length;
      }
      nodes.push(p);
    });
    columns = leftCols + rightCols + 1;
    rows = Math.max(1, left.rows + shiftL, right.rows + shiftR);
  }

  const byKey = new Map(nodes.map((n) => [n.key, n]));
  const edges: PlacedEdge[] = [];
  nodes.forEach((child) => {
    const parent = child.parentKey ? byKey.get(child.parentKey) : undefined;
    if (!parent) return;
    edges.push({ key: `${parent.key}>${child.key}`, from: parent.key, to: child.key, style: child.node.edge, ...elbow(parent, child, m) });
  });

  return {
    nodes,
    edges,
    byKey,
    width: columns ? m.pad * 2 + columns * colW - m.gapX : 0,
    height: rows ? m.pad * 2 + rows * rowH - m.gapY : 0,
  };
}

/** The keys of every node with children down to `levels` levels (1 = the roots), for "open N levels". */
export function parentKeys<T>(roots: DiagramNode<T>[], fromDepth = 0, depth = 0, out: string[] = []): string[] {
  roots.forEach((n) => {
    if (n.children.length === 0) return;
    if (depth >= fromDepth) out.push(n.key);
    parentKeys(n.children, fromDepth, depth + 1, out);
  });
  return out;
}

export type DiagramMove = 'up' | 'down' | 'in' | 'out' | 'first' | 'last';

/**
 * Where a key press moves the focus, as the WAI-ARIA tree pattern has it:
 * up and down through the visible nodes in tree order, in to the first child
 * (or out to the parent), home and end. Folding and unfolding on in / out is
 * the caller's: a folded node's "in" unfolds it instead of moving.
 */
export function moveFocus(layout: DiagramLayout<unknown>, from: string | null, move: DiagramMove): string | null {
  const order = layout.nodes;
  if (order.length === 0) return null;
  const at = from ? order.findIndex((n) => n.key === from) : -1;
  if (at === -1 || move === 'first') return order[0].key;
  if (move === 'last') return order[order.length - 1].key;
  const here = order[at];
  if (move === 'up') return order[Math.max(0, at - 1)].key;
  if (move === 'down') return order[Math.min(order.length - 1, at + 1)].key;
  if (move === 'in') return here.childKeys[0] ?? here.key;
  return here.parentKey ?? here.key;
}

import type { ImpactGroup, ImpactNode, ImpactReport } from '../api/client';
import type { DiagramNode, DiagramShape } from '../components/treeDiagram';

// The impact as the shared tree diagram's two-sided shape: the selected
// artifact in the middle, what it depends on branching to the left
// (upstream) and what depends on it branching to the right (downstream).
// Each artifact hangs off the one before it on its shortest path, so the
// tree is the breadth-first walk the server made; each connector carries the
// link type it was reached through, coloured and labelled.

/** How many links deep to show: 1, 2, 3, or the whole chain. */
export type ImpactDepth = 1 | 2 | 3 | 'all';
export type ImpactSide = 'upstream' | 'downstream';

export interface ImpactBox {
  id: string;
  title: string;
  type: string;
  /** The link type it was reached through; '' for the centre. */
  via: string;
  side: ImpactSide | 'seed';
  /** Hops from the centre: 1 is a direct link, 0 the centre itself. */
  depth: number;
  /** The artifact it hangs off: the centre for a direct link. */
  parentId: string;
}

// Each link type has a colour, used for the connector that reaches an
// artifact and its label, so a dependency's kind reads at a glance.
export const LINK_COLOURS: Record<string, string> = {
  'derives-from': 'var(--purple)',
  'decomposes-to': 'var(--warning)',
  refines: 'var(--warning-strong)',
  satisfies: 'var(--accent)',
  verifies: 'var(--success)',
  validates: 'var(--success-strong)',
  mitigates: 'var(--danger)',
  impacts: 'var(--danger-strong)',
  'relates-to': 'var(--neutral)',
};
export const linkColour = (via: string) => LINK_COLOURS[via] || 'var(--neutral)';

/** The link types in use, in the key's order, unknown ones last. */
export const usedLinkTypes = (boxes: ImpactBox[]): string[] => {
  const used = new Set(boxes.map((b) => b.via).filter(Boolean));
  return Object.keys(LINK_COLOURS)
    .filter((t) => used.has(t))
    .concat(Array.from(used).filter((t) => !LINK_COLOURS[t]).sort());
};

// Sibling order, as the server groups them: needs, requirements, design,
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

const within = (depth: ImpactDepth) => (n: ImpactNode) => depth === 'all' || n.distance <= depth;

/** The key of an artifact's box: an artifact can sit on both sides (relates-to). */
export const boxKey = (side: ImpactSide, id: string) => `${side}:${id}`;

function buildSide(
  seedId: string,
  side: ImpactSide,
  groups: ImpactGroup[],
  depth: ImpactDepth,
  titleOf: (id: string) => string,
  out: ImpactBox[]
): DiagramNode<ImpactBox>[] {
  const source = groups.flatMap((g) => g.nodes).filter(within(depth));
  const present = new Set(source.map((n) => n.artifact_id));
  const children: Record<string, ImpactNode[]> = {};
  source.forEach((n) => {
    const parent = n.path.length >= 2 ? n.path[n.path.length - 2] : seedId;
    // A parent outside the shown set (never for a breadth-first walk) falls
    // back to the centre so no artifact is lost.
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
  const seen = new Set<string>();
  const visit = (n: ImpactNode, parentId: string, level: number): DiagramNode<ImpactBox> => {
    seen.add(n.artifact_id);
    const box: ImpactBox = {
      id: n.artifact_id,
      title: n.title || titleOf(n.artifact_id),
      type: n.type,
      via: n.via,
      side,
      depth: level,
      parentId,
    };
    out.push(box);
    const kids = (children[n.artifact_id] || []).filter((k) => !seen.has(k.artifact_id));
    return {
      key: boxKey(side, n.artifact_id),
      data: box,
      edge: { color: linkColour(n.via), label: n.via },
      children: kids.map((k) => visit(k, n.artifact_id, level + 1)),
    };
  };
  return (children[seedId] || []).filter((n) => !seen.has(n.artifact_id)).map((n) => visit(n, seedId, 1));
}

export interface ImpactShape {
  shape: DiagramShape<ImpactBox>;
  /** Every artifact shown, in tree order (upstream, then downstream). */
  boxes: ImpactBox[];
  upstream: number;
  downstream: number;
}

/** The report as a two-sided tree down to `depth` links. */
export function impactShape(
  report: ImpactReport,
  depth: ImpactDepth,
  seed: { title: string; type: string },
  titleOf: (id: string) => string = (id) => id
): ImpactShape {
  const seedId = report.artifact_id;
  const up: ImpactBox[] = [];
  const down: ImpactBox[] = [];
  const left = buildSide(seedId, 'upstream', report.upstream || [], depth, titleOf, up);
  const right = buildSide(seedId, 'downstream', report.downstream || [], depth, titleOf, down);
  const root: DiagramNode<ImpactBox> = {
    key: seedId,
    data: { id: seedId, title: seed.title, type: seed.type, via: '', side: 'seed', depth: 0, parentId: '' },
    children: [],
  };
  return {
    shape: { kind: 'two-sided', root, left, right },
    boxes: [...up, ...down],
    upstream: up.length,
    downstream: down.length,
  };
}

/** The deepest hop in the report, to offer only the depths that change something. */
export const reportDepth = (report: ImpactReport): number =>
  Math.max(0, ...[...(report.upstream || []), ...(report.downstream || [])].flatMap((g) => g.nodes.map((n) => n.distance)));

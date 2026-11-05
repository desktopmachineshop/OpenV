import { DiagramNode, layoutDiagram, moveFocus, parentKeys } from './layout';
import { fitView, revealRect, zoomAbout } from './usePanZoom';

const n = (key: string, children: DiagramNode<string>[] = [], edge?: DiagramNode<string>['edge']): DiagramNode<string> => ({
  key,
  data: key,
  children,
  edge,
});

const M = { boxWidth: 100, boxHeight: 40, gapX: 20, gapY: 10, pad: 0 };
const COL = 120;
const ROW = 50;

//  a ── a1 ── a1x
//    └─ a2
//  b
const FOREST = [n('a', [n('a1', [n('a1x')]), n('a2')]), n('b')];

describe('the forest layout (left to right)', () => {
  it('puts each level in a column, a leaf per row and a parent midway between its children', () => {
    const l = layoutDiagram({ kind: 'forest', roots: FOREST }, new Set(), M);
    const at = (k: string) => [l.byKey.get(k)!.x / COL, l.byKey.get(k)!.y / ROW];
    expect(at('a1x')).toEqual([2, 0]);
    expect(at('a1')).toEqual([1, 0]);
    expect(at('a2')).toEqual([1, 1]);
    expect(at('a')).toEqual([0, 0.5]);
    expect(at('b')).toEqual([0, 2]);
    expect(l.nodes.map((p) => p.key)).toEqual(['a', 'a1', 'a1x', 'a2', 'b']);
    expect(l.width).toBe(3 * COL - 20);
    expect(l.height).toBe(3 * ROW - 10);
  });

  it('draws an elbow from the right edge of a parent to the left edge of each child', () => {
    const l = layoutDiagram({ kind: 'forest', roots: FOREST }, new Set(), M);
    const e = l.edges.find((x) => x.to === 'a2')!;
    // Out of a at (100, 45), along to the elbow at x 110, down to a2's middle, into a2 at 120.
    expect(e.path).toBe('M100 45H110V70H120');
    expect(l.edges.map((x) => x.key)).toEqual(['a>a1', 'a1>a1x', 'a>a2']);
  });

  it('lays out only what is unfolded, and counts what a fold hides', () => {
    const l = layoutDiagram({ kind: 'forest', roots: FOREST }, new Set(['a']), M);
    expect(l.nodes.map((p) => p.key)).toEqual(['a', 'b']);
    const a = l.byKey.get('a')!;
    expect(a).toMatchObject({ folded: true, hasChildren: true, hiddenCount: 3, childKeys: [] });
    expect(l.edges).toHaveLength(0);
    // A fold on a leaf is no fold.
    expect(layoutDiagram({ kind: 'forest', roots: FOREST }, new Set(['b']), M).byKey.get('b')!.folded).toBe(false);
  });

  it('gives each box its level and its place among its siblings', () => {
    const l = layoutDiagram({ kind: 'forest', roots: FOREST }, new Set(), M);
    expect(l.byKey.get('a2')).toMatchObject({ level: 2, posInSet: 2, setSize: 2, parentKey: 'a' });
    expect(l.byKey.get('b')).toMatchObject({ level: 1, posInSet: 2, setSize: 2, parentKey: null });
  });

  it('names the parents below a depth, for opening a tree to two levels', () => {
    expect(parentKeys(FOREST)).toEqual(['a', 'a1']);
    expect(parentKeys(FOREST, 1)).toEqual(['a1']);
  });
});

//            u1x ─ u1 ─┐          ┌─ d1
//                      ├─ seed ───┤
//                  u2 ─┘          └─ d2 ── d2x
describe('the two-sided layout (root in the middle)', () => {
  const shape = {
    kind: 'two-sided' as const,
    root: n('seed'),
    left: [n('u1', [n('u1x')], { label: 'derives-from', color: 'var(--purple)' }), n('u2')],
    right: [n('d1', [], { label: 'verifies', color: 'var(--success)', dashed: true }), n('d2', [n('d2x')])],
  };

  it('branches upstream to the left and downstream to the right, centred on the root', () => {
    const l = layoutDiagram(shape, new Set(), M);
    const col = (k: string) => l.byKey.get(k)!.x / COL;
    expect([col('u1x'), col('u1'), col('u2'), col('seed'), col('d1'), col('d2'), col('d2x')]).toEqual([0, 1, 1, 2, 3, 3, 4]);
    const row = (k: string) => l.byKey.get(k)!.y / ROW;
    expect(row('seed')).toBe(0.5);
    expect([row('u1'), row('u2'), row('d1'), row('d2')]).toEqual([0, 1, 0, 1]);
    expect(l.byKey.get('u1')!.side).toBe('left');
    expect(l.byKey.get('seed')).toMatchObject({ side: 'root', level: 1, childKeys: ['u1', 'u2', 'd1', 'd2'] });
    expect(l.byKey.get('d2')).toMatchObject({ level: 2, posInSet: 4, setSize: 4 });
    expect(l.width).toBe(5 * COL - 20);
  });

  it('runs a left-hand connector from the left edge of the parent to the right edge of the child', () => {
    const l = layoutDiagram(shape, new Set(), M);
    const e = l.edges.find((x) => x.to === 'u1')!;
    // seed at x 240, u1 at x 120 (right edge 220): the elbow sits at 230.
    expect(e.path).toBe('M240 45H230V20H220');
  });

  it('carries each edge its label, colour and dash', () => {
    const l = layoutDiagram(shape, new Set(), M);
    expect(l.edges.find((x) => x.to === 'u1')!.style).toEqual({ label: 'derives-from', color: 'var(--purple)' });
    expect(l.edges.find((x) => x.to === 'd1')!.style).toMatchObject({ dashed: true });
  });

  it('centres a lopsided tree on its longer side', () => {
    const l = layoutDiagram({ kind: 'two-sided', root: n('s'), left: [], right: [n('a'), n('b'), n('c')] }, new Set(), M);
    expect(l.byKey.get('s')!.y / ROW).toBe(1);
    expect(l.byKey.get('s')!.x).toBe(0);
    expect(l.height).toBe(3 * ROW - 10);
  });

  it('folds a side node, and the root alone when nothing branches', () => {
    const l = layoutDiagram(shape, new Set(['d2']), M);
    expect(l.byKey.has('d2x')).toBe(false);
    expect(l.byKey.get('d2')!.hiddenCount).toBe(1);
    const alone = layoutDiagram({ kind: 'two-sided', root: n('s'), left: [], right: [] }, new Set(), M);
    expect(alone.nodes).toHaveLength(1);
    expect(alone.height).toBe(40);
  });
});

describe('moving the focus', () => {
  const l = layoutDiagram({ kind: 'forest', roots: FOREST }, new Set(), M);
  it('goes through the visible boxes in tree order, in to a child and out to a parent', () => {
    expect(moveFocus(l, 'a', 'down')).toBe('a1');
    expect(moveFocus(l, 'a2', 'up')).toBe('a1x');
    expect(moveFocus(l, 'a', 'in')).toBe('a1');
    expect(moveFocus(l, 'a1x', 'out')).toBe('a1');
    expect(moveFocus(l, 'b', 'out')).toBe('b');
    expect(moveFocus(l, 'a1', 'last')).toBe('b');
    expect(moveFocus(l, null, 'down')).toBe('a');
  });
});

describe('pan and zoom', () => {
  it('fits a diagram into the viewport, never above 1:1', () => {
    expect(fitView(1000, 500, 2000, 500, 0)).toEqual({ k: 0.5, x: 0, y: 125 });
    expect(fitView(1000, 500, 100, 100, 0).k).toBe(1);
  });
  it('zooms about a point, which stays where it is', () => {
    const v = zoomAbout({ x: 0, y: 0, k: 1 }, 2, 100, 50);
    expect(v).toEqual({ k: 2, x: -100, y: -50 });
  });
  it('pans just enough to bring a box into view', () => {
    const v = { x: 0, y: 0, k: 1 };
    expect(revealRect(v, 500, 300, { x: 10 + 24, y: 30, w: 100, h: 40 })).toBe(v);
    expect(revealRect(v, 500, 300, { x: 600, y: 30, w: 100, h: 40 }).x).toBe(500 - 24 - 700);
  });
});

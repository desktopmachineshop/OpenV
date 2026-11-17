import { describe, expect, it } from 'vitest';
import type { ImpactNode, ImpactReport } from '../api/client';
import { layoutDiagram } from '../components/treeDiagram';
import { impactShape, reportDepth, usedLinkTypes } from './ImpactViewLayout';

const node = (id: string, type: string, via: string, path: string[]): ImpactNode => ({
  artifact_id: id,
  title: id,
  type,
  via,
  distance: path.length - 1,
  path,
});

// R1 derives from N1; D1 and D2 satisfy R1; D1 mitigates H1; T1 verifies R1.
const report: ImpactReport = {
  project_id: 'p1',
  artifact_id: 'R1',
  direction: 'both',
  upstream: [{ type: 'user-need', count: 1, nodes: [node('N1', 'user-need', 'derives-from', ['R1', 'N1'])] }],
  downstream: [
    {
      type: 'design-item',
      count: 2,
      nodes: [
        node('D1', 'design-item', 'satisfies', ['R1', 'D1']),
        node('D2', 'design-item', 'satisfies', ['R1', 'D2']),
      ],
    },
    { type: 'test-case', count: 1, nodes: [node('T1', 'test-case', 'verifies', ['R1', 'T1'])] },
    { type: 'hazard', count: 1, nodes: [node('H1', 'hazard', 'mitigates', ['R1', 'D1', 'H1'])] },
  ],
  total: 5,
};

const SEED = { title: 'R1', type: 'requirement' };
const find = (l: ReturnType<typeof impactShape>, side: string, id: string) => l.boxes.find((n) => n.side === side && n.id === id)!;

describe('impactShape', () => {
  it('hangs each artifact off the one before it on its path, all levels deep', () => {
    const l = impactShape(report, 'all', SEED);
    expect(l.upstream).toBe(1);
    expect(l.downstream).toBe(4);
    expect(find(l, 'downstream', 'H1')).toMatchObject({ parentId: 'D1', depth: 2, via: 'mitigates' });
    expect(find(l, 'downstream', 'D1')).toMatchObject({ parentId: 'R1', depth: 1 });
    expect(find(l, 'upstream', 'N1')).toMatchObject({ parentId: 'R1', depth: 1 });
    expect(reportDepth(report)).toBe(2);
  });

  it("is the shared diagram's two-sided shape, its connectors coloured and labelled by link type", () => {
    const { shape } = impactShape(report, 'all', SEED);
    if (shape.kind !== 'two-sided') throw new Error('expected a two-sided shape');
    expect(shape.root.key).toBe('R1');
    expect(shape.left.map((n) => n.key)).toEqual(['upstream:N1']);
    // Siblings in spine order: design, then tests.
    expect(shape.right.map((n) => n.key)).toEqual(['downstream:D1', 'downstream:D2', 'downstream:T1']);
    expect(shape.right[0].children.map((n) => n.key)).toEqual(['downstream:H1']);
    expect(shape.right[2].edge).toEqual({ color: 'var(--success)', label: 'verifies' });
    const placed = layoutDiagram(shape, new Set());
    expect(placed.byKey.get('upstream:N1')!.x).toBeLessThan(placed.byKey.get('R1')!.x);
    expect(placed.byKey.get('downstream:H1')!.x).toBeGreaterThan(placed.byKey.get('downstream:D1')!.x);
  });

  it('stops at the depth asked for', () => {
    const l = impactShape(report, 1, SEED);
    expect(l.boxes.map((n) => n.id).sort()).toEqual(['D1', 'D2', 'N1', 'T1']);
  });

  it('keys the boxes by side, so an artifact on both sides draws twice', () => {
    const both: ImpactReport = {
      ...report,
      upstream: [{ type: 'requirement', count: 1, nodes: [node('X', 'requirement', 'relates-to', ['R1', 'X'])] }],
      downstream: [{ type: 'requirement', count: 1, nodes: [node('X', 'requirement', 'relates-to', ['R1', 'X'])] }],
    };
    const l = impactShape(both, 'all', SEED);
    expect(l.boxes.map((b) => `${b.side}:${b.id}`)).toEqual(['upstream:X', 'downstream:X']);
    expect(usedLinkTypes(l.boxes)).toEqual(['relates-to']);
  });
});

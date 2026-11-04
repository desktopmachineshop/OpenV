import { describe, expect, it } from 'vitest';
import type { ImpactNode, ImpactReport } from '../api/client';
import { layoutImpactTree } from './ImpactViewLayout';

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

const find = (l: ReturnType<typeof layoutImpactTree>, side: string, id: string) =>
  l.nodes.find((n) => n.side === side && n.id === id)!;

describe('layoutImpactTree', () => {
  it('hangs each node off the one before it on its path, all levels deep', () => {
    const l = layoutImpactTree(report, 'all');
    expect(l.upstreamDepth).toBe(1);
    expect(l.downstreamDepth).toBe(2);
    expect(find(l, 'downstream', 'H1')).toMatchObject({ parentId: 'D1', depth: 2, via: 'mitigates' });
    expect(find(l, 'downstream', 'D1')).toMatchObject({ parentId: 'R1', depth: 1 });
    expect(find(l, 'upstream', 'N1')).toMatchObject({ parentId: 'R1', depth: 1 });
  });

  it('shows only direct links at one level', () => {
    const l = layoutImpactTree(report, 'one');
    expect(l.downstreamDepth).toBe(1);
    expect(l.nodes.map((n) => n.id).sort()).toEqual(['D1', 'D2', 'N1', 'T1']);
  });

  it('gives each leaf its own row, centres parents and the shorter side', () => {
    const l = layoutImpactTree(report, 'all');
    // Downstream leaves in spine order: H1 (under D1), D2, T1.
    expect(l.rows).toBe(3);
    const rows = ['D1', 'H1', 'D2', 'T1'].map((id) => find(l, 'downstream', id).row);
    expect(rows).toEqual([0, 0, 1, 2]);
    // One upstream row centred against three, level with the seed.
    expect(find(l, 'upstream', 'N1').row).toBe(1);
    expect(l.seedRow).toBe(1);
  });
});

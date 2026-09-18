import { Artifact } from '../api/client';
import {
  ancestorIds,
  compareArtifacts,
  documentOrder,
  sequencePosition,
  stepArtifact,
} from './artifactSequence';

// Stepping to the next artifact has to agree with the tree: document order,
// depth-first, parents before their children. These are the rules that matter
// — the order itself, that the ends are dead rather than wrapping, that a
// filtered-out artifact has no neighbours, and that the tree can be opened
// onto whatever the step landed on.

const art = (
  id: string,
  parent: string | null,
  order: number,
  created = '2026-01-01T00:00:00Z'
): Artifact =>
  ({
    id,
    ref: id.toUpperCase(),
    project_id: 'p',
    parent_id: parent,
    type: id.startsWith('hdg') ? 'heading' : 'requirement',
    title: id,
    body: '',
    sort_order: order,
    attributes: {},
    version: 1,
    valid_from: '',
    valid_to: null,
    created_at: created,
    updated_at: created,
  }) as Artifact;

//  hdg-1        1
//    req-1      1.1
//      req-2    1.1.1
//    req-3      1.2
//  hdg-2        2
//    req-4      2.1
const tree = [
  art('req-3', 'hdg-1', 2),
  art('hdg-2', null, 2),
  art('req-1', 'hdg-1', 1),
  art('hdg-1', null, 1),
  art('req-4', 'hdg-2', 1),
  art('req-2', 'req-1', 1),
];

const refs = (artifacts: Artifact[]) => artifacts.map((a) => a.id);

describe('documentOrder', () => {
  it('walks the tree depth-first, a parent before its children', () => {
    expect(documentOrder(tree).map((e) => e.artifact.id)).toEqual([
      'hdg-1',
      'req-1',
      'req-2',
      'req-3',
      'hdg-2',
      'req-4',
    ]);
  });

  it('reports the depth each row is drawn at', () => {
    expect(documentOrder(tree).map((e) => e.depth)).toEqual([0, 1, 2, 1, 0, 1]);
  });

  it('treats an artifact whose parent is missing as a root, so a filter cannot hide it', () => {
    // req-1 and its child survive a filter that dropped their heading.
    const filtered = [art('req-1', 'hdg-1', 1), art('req-2', 'req-1', 1)];
    expect(refs(documentOrder(filtered).map((e) => e.artifact))).toEqual(['req-1', 'req-2']);
  });

  it('sorts a group with no sort_order by age, and puts ordered rows first', () => {
    const mixed = [
      art('late', null, 0, '2026-03-01T00:00:00Z'),
      art('early', null, 0, '2026-02-01T00:00:00Z'),
      art('ordered', null, 5),
    ];
    expect(refs(documentOrder(mixed).map((e) => e.artifact))).toEqual(['ordered', 'early', 'late']);
  });

  it('does not recurse for ever on an artifact that is its own parent', () => {
    const looped = [art('a', 'a', 1), art('b', null, 2)];
    expect(refs(documentOrder(looped).map((e) => e.artifact))).toEqual(['a', 'b']);
  });

  it('does not recurse for ever on a two-artifact cycle', () => {
    const looped = [art('a', 'b', 1), art('b', 'a', 2)];
    expect(documentOrder(looped)).toHaveLength(2);
  });
});

describe('stepArtifact', () => {
  const sequence = documentOrder(tree);

  it('steps forward into a child', () => {
    expect(stepArtifact(sequence, 'req-1', 1)?.id).toBe('req-2');
  });

  it('steps forward out of a subtree into the next heading', () => {
    expect(stepArtifact(sequence, 'req-3', 1)?.id).toBe('hdg-2');
  });

  it('steps back out of a subtree', () => {
    expect(stepArtifact(sequence, 'hdg-2', -1)?.id).toBe('req-3');
  });

  it('stops at the first artifact rather than wrapping', () => {
    expect(stepArtifact(sequence, 'hdg-1', -1)).toBeNull();
  });

  it('stops at the last artifact rather than wrapping', () => {
    expect(stepArtifact(sequence, 'req-4', 1)).toBeNull();
  });

  it('has no neighbour for an artifact the filter removed', () => {
    expect(stepArtifact(sequence, 'gone', 1)).toBeNull();
    expect(stepArtifact(sequence, 'gone', -1)).toBeNull();
  });

  it('has no neighbour when nothing is selected', () => {
    expect(stepArtifact(sequence, null, 1)).toBeNull();
    expect(stepArtifact(sequence, undefined, -1)).toBeNull();
  });
});

describe('sequencePosition', () => {
  const sequence = documentOrder(tree);

  it('counts from one, over the whole sequence', () => {
    expect(sequencePosition(sequence, 'req-2')).toEqual({ position: 3, total: 6 });
    expect(sequencePosition(sequence, 'hdg-1')).toEqual({ position: 1, total: 6 });
    expect(sequencePosition(sequence, 'req-4')).toEqual({ position: 6, total: 6 });
  });

  it('is nothing for an artifact outside the sequence, or for no selection', () => {
    expect(sequencePosition(sequence, 'gone')).toBeNull();
    expect(sequencePosition(sequence, null)).toBeNull();
  });
});

describe('ancestorIds', () => {
  it('lists the parents to open, nearest first', () => {
    expect(ancestorIds(tree, 'req-2')).toEqual(['req-1', 'hdg-1']);
  });

  it('is empty for a root and for nothing selected', () => {
    expect(ancestorIds(tree, 'hdg-1')).toEqual([]);
    expect(ancestorIds(tree, null)).toEqual([]);
  });

  it('terminates on a cycle', () => {
    expect(ancestorIds([art('a', 'b', 1), art('b', 'a', 2)], 'a')).toEqual(['b']);
  });
});

describe('compareArtifacts', () => {
  it('is the order the tree sorts a sibling group in', () => {
    const group = [art('third', null, 3), art('first', null, 1), art('second', null, 2)];
    expect(refs([...group].sort(compareArtifacts))).toEqual(['first', 'second', 'third']);
  });
});

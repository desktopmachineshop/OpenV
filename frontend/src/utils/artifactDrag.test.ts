import { Artifact } from '../api/client';
import { DropZone, dropZoneFor, isSelfOrDescendant, planMove } from './artifactDrag';

const artifact = (
  id: string,
  parent_id: string | null,
  sort_order: number,
  title = id
): Artifact => ({
  id,
  project_id: 'p1',
  parent_id,
  type: 'requirement',
  title,
  body: '',
  sort_order,
  attributes: {},
  version: 1,
  valid_from: '',
  valid_to: null,
  created_at: '',
  updated_at: '',
});

// A B C at the root; B has children B1 and B2.
const tree = (): Artifact[] => [
  artifact('A', null, 1),
  artifact('B', null, 2),
  artifact('C', null, 3),
  artifact('B1', 'B', 1),
  artifact('B2', 'B', 2),
];

const rect = { top: 0, left: 0, width: 200, height: 40 };

describe('dropZoneFor', () => {
  it('reads the top half as "before"', () => {
    expect(dropZoneFor(rect, 10, 0)).toBe('before');
    expect(dropZoneFor(rect, 190, 19)).toBe('before');
  });

  it('splits the bottom half into after (left) and child (right)', () => {
    expect(dropZoneFor(rect, 10, 30)).toBe('after');
    expect(dropZoneFor(rect, 99, 39)).toBe('after');
    expect(dropZoneFor(rect, 100, 30)).toBe('child');
    expect(dropZoneFor(rect, 199, 39)).toBe('child');
  });

  it('does not divide by a zero-height row', () => {
    expect(dropZoneFor({ ...rect, height: 0 }, 10, 0)).toBe('before');
  });

  it('reads positions relative to the row, not the page', () => {
    const offset = { top: 500, left: 300, width: 200, height: 40 };
    expect(dropZoneFor(offset, 310, 505)).toBe('before');
    expect(dropZoneFor(offset, 310, 535)).toBe('after');
    expect(dropZoneFor(offset, 450, 535)).toBe('child');
  });
});

// Dropping an artifact into its own subtree would detach that whole branch,
// and nothing downstream would notice until the document failed to render.
describe('isSelfOrDescendant', () => {
  it('recognises itself and its descendants', () => {
    expect(isSelfOrDescendant(tree(), 'B', 'B')).toBe(true);
    expect(isSelfOrDescendant(tree(), 'B', 'B1')).toBe(true);
  });

  it('finds a grandchild', () => {
    const deep = [...tree(), artifact('B1a', 'B1', 1)];
    expect(isSelfOrDescendant(deep, 'B', 'B1a')).toBe(true);
  });

  it('says no for unrelated artifacts and for a parent', () => {
    expect(isSelfOrDescendant(tree(), 'B', 'A')).toBe(false);
    expect(isSelfOrDescendant(tree(), 'B1', 'B')).toBe(false);
  });

  it('terminates on a cycle already in the data', () => {
    const cyclic = [artifact('X', 'Y', 1), artifact('Y', 'X', 1)];
    expect(isSelfOrDescendant(cyclic, 'Z', 'X')).toBe(false);
  });
});

describe('planMove', () => {
  const ids = (plan: ReturnType<typeof planMove>) => plan?.ordered.map((a) => a.id);

  it('places a sibling after the target', () => {
    const plan = planMove(tree(), 'A', 'B', 'after');
    expect(plan?.parentId).toBeNull();
    expect(plan?.reparents).toBe(false);
    expect(ids(plan)).toEqual(['B', 'A', 'C']);
  });

  it('places a sibling before the target', () => {
    const plan = planMove(tree(), 'C', 'B', 'before');
    expect(ids(plan)).toEqual(['A', 'C', 'B']);
  });

  it('makes the artifact a child, appended last', () => {
    const plan = planMove(tree(), 'A', 'B', 'child');
    expect(plan?.parentId).toBe('B');
    expect(plan?.reparents).toBe(true);
    expect(ids(plan)).toEqual(['B1', 'B2', 'A']);
  });

  it('moves a child out to the root beside its old parent', () => {
    const plan = planMove(tree(), 'B1', 'C', 'after');
    expect(plan?.parentId).toBeNull();
    expect(plan?.reparents).toBe(true);
    expect(ids(plan)).toEqual(['A', 'B', 'C', 'B1']);
  });

  it('moves between two different parents', () => {
    const withD = [...tree(), artifact('D', null, 4), artifact('D1', 'D', 1)];
    const plan = planMove(withD, 'B1', 'D1', 'before');
    expect(plan?.parentId).toBe('D');
    expect(ids(plan)).toEqual(['B1', 'D1']);
  });

  it('refuses to drop an artifact on itself', () => {
    for (const zone of ['before', 'after', 'child'] as DropZone[]) {
      expect(planMove(tree(), 'B', 'B', zone)).toBeNull();
    }
  });

  it('refuses to drop an artifact into its own subtree', () => {
    expect(planMove(tree(), 'B', 'B1', 'child')).toBeNull();
    expect(planMove(tree(), 'B', 'B1', 'after')).toBeNull();
    const deep = [...tree(), artifact('B1a', 'B1', 1)];
    expect(planMove(deep, 'B', 'B1a', 'child')).toBeNull();
  });

  it('refuses a move that changes nothing', () => {
    // A is already immediately before B, and already B's preceding sibling.
    expect(planMove(tree(), 'A', 'B', 'before')).toBeNull();
    // B1 is already the last child of B.
    expect(planMove(tree(), 'B2', 'B', 'child')).toBeNull();
  });

  it('still plans a re-parent that lands in the same visible position', () => {
    // B2 is B's last child; making it a child of B again is a no-op above,
    // but making it a child of a DIFFERENT parent is not.
    const plan = planMove(tree(), 'B2', 'A', 'child');
    expect(plan?.parentId).toBe('A');
    expect(ids(plan)).toEqual(['B2']);
  });

  it('returns nothing for artifacts it cannot find', () => {
    expect(planMove(tree(), 'ghost', 'B', 'after')).toBeNull();
    expect(planMove(tree(), 'A', 'ghost', 'after')).toBeNull();
  });
});

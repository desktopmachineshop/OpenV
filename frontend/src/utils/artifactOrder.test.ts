import { Artifact } from '../api/client';
import { nextSortOrder, planPlacement, siblingsUnder } from './artifactOrder';

// The assistant describes a place — "under HDG-2, after REQ-3", "to the
// top" — and this turns it into the same plan a drag would produce. These
// are the rules that matter: the anchor decides the index, a parent that is
// the artifact's own subtree is refused, and a move that changes nothing is
// not a move.

const art = (id: string, parent: string | null, order: number, ref = id.toUpperCase()): Artifact =>
  ({
    id,
    ref,
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
    created_at: '',
    updated_at: '',
  }) as Artifact;

const tree = [
  art('hdg-1', null, 1),
  art('req-1', 'hdg-1', 1),
  art('req-2', 'hdg-1', 2),
  art('req-3', 'hdg-1', 3),
  art('hdg-2', null, 2),
  art('req-4', 'hdg-2', 1),
];

const order = (plan: ReturnType<typeof planPlacement>) => plan!.ordered.map((a) => a.id);

describe('placing an artifact by description', () => {
  it('reorders among its siblings by anchor', () => {
    expect(order(planPlacement(tree, 'req-3', { beforeId: 'req-1' }))).toEqual(['req-3', 'req-1', 'req-2']);
    expect(order(planPlacement(tree, 'req-1', { afterId: 'req-2' }))).toEqual(['req-2', 'req-1', 'req-3']);
    expect(planPlacement(tree, 'req-1', { afterId: 'req-2' })!.reparents).toBe(false);
  });

  it('moves to the top or the bottom', () => {
    expect(order(planPlacement(tree, 'req-3', { position: 'first' }))).toEqual(['req-3', 'req-1', 'req-2']);
    expect(order(planPlacement(tree, 'req-1', { position: 'last' }))).toEqual(['req-2', 'req-3', 'req-1']);
  });

  it('moves under another heading, last by default', () => {
    const plan = planPlacement(tree, 'req-2', { parentId: 'hdg-2' });
    expect(plan!.reparents).toBe(true);
    expect(plan!.parentId).toBe('hdg-2');
    expect(order(plan)).toEqual(['req-4', 'req-2']);
    // Or beside a sibling there.
    expect(order(planPlacement(tree, 'req-2', { parentId: 'hdg-2', beforeId: 'req-4' }))).toEqual(['req-2', 'req-4']);
    // Or to the root.
    expect(order(planPlacement(tree, 'req-2', { parentId: null, position: 'first' }))).toEqual(['req-2', 'hdg-1', 'hdg-2']);
  });

  // The anchor and the parent are one instruction; an anchor under a
  // different heading contradicts it, and guessing which half was meant
  // would file the artifact somewhere the person did not say.
  it('refuses an anchor that is not under the destination', () => {
    expect(planPlacement(tree, 'req-2', { parentId: 'hdg-2', afterId: 'req-1' })).toBeNull();
    expect(planPlacement(tree, 'req-2', { afterId: 'req-4' })).toBeNull();
  });

  it('refuses a move into its own subtree and a move that changes nothing', () => {
    expect(planPlacement(tree, 'hdg-1', { parentId: 'req-1' })).toBeNull();
    expect(planPlacement(tree, 'hdg-1', { parentId: 'hdg-1' })).toBeNull();
    expect(planPlacement(tree, 'req-3', { position: 'last' })).toBeNull();
    expect(planPlacement(tree, 'req-2', { afterId: 'req-1' })).toBeNull();
    expect(planPlacement(tree, 'nope', { position: 'first' })).toBeNull();
  });
});

describe('siblings and the next slot', () => {
  it('orders siblings the way the tree shows them', () => {
    expect(siblingsUnder(tree, 'hdg-1').map((a) => a.id)).toEqual(['req-1', 'req-2', 'req-3']);
    expect(siblingsUnder(tree, null).map((a) => a.id)).toEqual(['hdg-1', 'hdg-2']);
  });

  it('gives a new child the slot after the last one', () => {
    expect(nextSortOrder(tree, 'hdg-1')).toBe(4);
    expect(nextSortOrder(tree, 'hdg-2')).toBe(2);
    expect(nextSortOrder(tree, 'req-4')).toBe(1);
  });
});

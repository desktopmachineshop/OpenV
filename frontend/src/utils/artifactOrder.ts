// Placing an artifact in the tree by description rather than by pointer.
//
// artifactDrag.ts plans a drop: "this row, onto that row, in this zone".
// The assistant cannot point; it says "put REQ-12 under HDG-3, after REQ-9"
// or "move it to the top". This is the same plan built from that kind of
// instruction, with the same rules — no cycles, no lost ordering — and the
// same shape out, so the two are saved the same way.
//
// Everything here is pure except saveSiblingOrder, which is the one write.
import { Artifact, artifactAPI } from '../api/client';
import { MovePlan, isSelfOrDescendant } from './artifactDrag';

const parentOf = (artifact: Artifact): string | null => artifact.parent_id ?? null;

/** Artifacts ordered the way the tree shows them: by sort order, then title. */
export const bySortOrder = (a: Artifact, b: Artifact): number => {
  const left = a.sort_order ?? 0;
  const right = b.sort_order ?? 0;
  if (left !== right) return left - right;
  return a.title.localeCompare(b.title);
};

/** The children of one parent (null for the root), in tree order. */
export const siblingsUnder = (artifacts: Artifact[], parentId: string | null): Artifact[] =>
  artifacts.filter((a) => parentOf(a) === parentId).sort(bySortOrder);

/** Where an artifact should go, as the assistant describes it. */
export interface Placement {
  /**
   * The parent to move under: an id, null for the root, or undefined to keep
   * the current parent and only change the position.
   */
  parentId?: string | null;
  /** Sibling to land immediately before. */
  beforeId?: string;
  /** Sibling to land immediately after. */
  afterId?: string;
  /** First or last among the siblings; the default is last. */
  position?: 'first' | 'last';
}

/**
 * Plan a move by description, or return null when it cannot or need not
 * happen: an unknown artifact or anchor, an anchor that is not under the
 * destination parent, a move into the artifact's own subtree, or one that
 * changes nothing.
 *
 * Like planMove, the destination group is returned whole so the caller can
 * renumber it: sort orders are plain integers with no guaranteed gaps.
 */
export const planPlacement = (
  artifacts: Artifact[],
  sourceId: string,
  placement: Placement
): MovePlan | null => {
  const source = artifacts.find((a) => a.id === sourceId);
  if (!source) return null;

  const parentId = placement.parentId === undefined ? parentOf(source) : placement.parentId;
  if (parentId && isSelfOrDescendant(artifacts, source.id, parentId)) return null;
  const reparents = parentOf(source) !== parentId;

  const siblings = siblingsUnder(artifacts, parentId).filter((a) => a.id !== source.id);

  let index: number;
  const anchorId = placement.beforeId || placement.afterId;
  if (anchorId) {
    const at = siblings.findIndex((a) => a.id === anchorId);
    // An anchor under some other parent would be a contradiction; refuse
    // rather than guess which half of the instruction was meant.
    if (at === -1) return null;
    index = placement.beforeId ? at : at + 1;
  } else {
    index = placement.position === 'first' ? 0 : siblings.length;
  }

  const ordered = [...siblings];
  ordered.splice(index, 0, source);

  if (!reparents) {
    const before = siblingsUnder(artifacts, parentId).map((a) => a.id);
    if (before.length === ordered.length && before.every((id, i) => id === ordered[i].id)) {
      return null;
    }
  }
  return { parentId, reparents, ordered };
};

/**
 * The sort order a new last child of `parentId` should get: one past the
 * highest there, so an addition lands where a reader expects it.
 */
export const nextSortOrder = (artifacts: Artifact[], parentId: string | null): number =>
  siblingsUnder(artifacts, parentId).reduce((max, a) => Math.max(max, a.sort_order ?? 0), 0) + 1;

/**
 * Save a sibling group in the given order, re-parenting `reparentId` on the
 * way when the move changed its parent. Returns the artifacts the server
 * wrote, so the caller can fold them into what it shows.
 *
 * Renumbering the whole group is what the module view's reorder does too:
 * rewriting 1..n is simpler than finding room between two integers. Only
 * rows whose number (or parent) actually changes are written.
 */
export const saveSiblingOrder = async (
  plan: MovePlan,
  reparentId?: string
): Promise<Artifact[]> => {
  const updates = plan.ordered
    .map((artifact, index) => ({ artifact, newOrder: index + 1 }))
    .filter(
      ({ artifact, newOrder }) =>
        (artifact.sort_order ?? 0) !== newOrder || artifact.id === reparentId
    );
  const responses = await Promise.all(
    updates.map(({ artifact, newOrder }) =>
      artifactAPI.update(artifact.id, {
        // Only the moved artifact changes parent; its new siblings keep
        // theirs, which is the same value.
        parent_id: artifact.id === reparentId ? plan.parentId : artifact.parent_id ?? null,
        type: artifact.type,
        title: artifact.title,
        body: artifact.body,
        attributes: artifact.attributes,
        sort_order: newOrder,
      })
    )
  );
  return responses.map((r) => r.data);
};

// Dragging an artifact onto another one: where the pointer lands decides
// whether the artifact becomes a sibling or a child.
//
//   ┌─────────────────────────────┐
//   │  top half → before target   │
//   ├──────────────┬──────────────┤
//   │ after target │ child of it  │
//   └──────────────┴──────────────┘
//
// Reordering used to be sibling-only, so the one thing a tree really needs —
// changing an artifact's parent — could only be done by deleting and
// recreating it. The bottom-right quadrant is that move, and the split keeps it
// in the same gesture rather than behind a separate mode.
//
// Everything here is pure: the zone is derived from a rectangle and a point,
// and a move is planned as data. The list component only draws it and the view
// only persists it, so the rules that matter — no cycles, no lost ordering —
// are testable without a browser.
import { Artifact } from '../api/client';

/** Where a drop lands relative to the artifact under the pointer. */
export type DropZone = 'before' | 'after' | 'child';

/** The part of a row's box the pointer is over. */
export interface DropRect {
  top: number;
  left: number;
  width: number;
  height: number;
}

/**
 * The zone a pointer sits in. The top half keeps the old behaviour (drop
 * before the row under the pointer); the bottom half splits left/right into
 * "after it" and "inside it".
 */
export const dropZoneFor = (rect: DropRect, clientX: number, clientY: number): DropZone => {
  if (rect.height <= 0 || clientY - rect.top < rect.height / 2) return 'before';
  return clientX - rect.left >= rect.width / 2 ? 'child' : 'after';
};

const parentOf = (artifact: Artifact): string | null => artifact.parent_id ?? null;

/** Artifacts ordered the way the tree shows them: by sort order, then title. */
const bySortOrder = (a: Artifact, b: Artifact): number => {
  const left = a.sort_order ?? 0;
  const right = b.sort_order ?? 0;
  if (left !== right) return left - right;
  return a.title.localeCompare(b.title);
};

/**
 * Whether `candidateId` is `rootId` itself or somewhere inside its subtree.
 *
 * This is the cycle guard: dropping an artifact into its own descendant would
 * detach that whole branch from the tree, and nothing downstream would notice
 * until the document failed to render.
 */
export const isSelfOrDescendant = (
  artifacts: Artifact[],
  rootId: string,
  candidateId: string
): boolean => {
  if (rootId === candidateId) return true;
  const byId = new Map(artifacts.map((a) => [a.id, a]));
  const seen = new Set<string>();
  let current = byId.get(candidateId);
  while (current) {
    const parent = parentOf(current);
    // A pre-existing cycle in the data must not hang the drag.
    if (!parent || seen.has(parent)) return false;
    if (parent === rootId) return true;
    seen.add(parent);
    current = byId.get(parent);
  }
  return false;
};

/** Where a dragged artifact ends up, and how its new siblings are ordered. */
export interface MovePlan {
  /** The artifact's parent after the move; null at the root. */
  parentId: string | null;
  /** True when the move changes its parent, not just its position. */
  reparents: boolean;
  /** The destination sibling group, in the order it should be saved. */
  ordered: Artifact[];
}

/**
 * Plan a drop, or return null when it should not happen: onto itself, into its
 * own subtree, or a move that changes nothing.
 *
 * The destination group is returned in full because sort orders are plain
 * integers with no guaranteed gaps — the caller renumbers the group, exactly as
 * the existing reorder does.
 */
export const planMove = (
  artifacts: Artifact[],
  sourceId: string,
  targetId: string,
  zone: DropZone
): MovePlan | null => {
  const source = artifacts.find((a) => a.id === sourceId);
  const target = artifacts.find((a) => a.id === targetId);
  if (!source || !target || source.id === target.id) return null;
  if (isSelfOrDescendant(artifacts, source.id, target.id)) return null;

  const parentId = zone === 'child' ? target.id : parentOf(target);
  const reparents = parentOf(source) !== parentId;

  const siblings = artifacts
    .filter((a) => parentOf(a) === parentId && a.id !== source.id)
    .sort(bySortOrder);

  let index: number;
  if (zone === 'child') {
    // A new child joins at the end, where a reader expects an addition.
    index = siblings.length;
  } else {
    const at = siblings.findIndex((a) => a.id === target.id);
    if (at === -1) return null;
    index = zone === 'after' ? at + 1 : at;
  }

  const ordered = [...siblings];
  ordered.splice(index, 0, source);

  if (!reparents) {
    // Same parent and same order: nothing to save.
    const before = artifacts
      .filter((a) => parentOf(a) === parentId)
      .sort(bySortOrder)
      .map((a) => a.id);
    if (before.length === ordered.length && before.every((id, i) => id === ordered[i].id)) {
      return null;
    }
  }

  return { parentId, reparents, ordered };
};

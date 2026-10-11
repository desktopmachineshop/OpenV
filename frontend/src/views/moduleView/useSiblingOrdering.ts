import { artifactAPI, Artifact } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { DropZone, planMove } from '../../utils/artifactDrag';
import { compareArtifacts, normalizeParentId } from '../../utils/artifactSequence';
import type { Setter } from './shared';

export interface SiblingOrderingOptions {
  /** The store's artifact list, as this render has it. */
  artifacts: Artifact[];
  /** The store's artifact list, written with the saved artifacts. */
  setArtifacts: (artifacts: Artifact[]) => void;
  /** The shell's error banner. */
  setError: Setter<string>;
  isBaselineView: boolean;
}

/**
 * Where an artifact sits among its siblings: the ▲▼ swap, a drag or Move to…,
 * and the placement of a new artifact beside another. Each renumbers its
 * sibling group 1..n and saves the artifacts whose number changed. Three
 * orders are in play and each is kept: the swap reads the group as the tree
 * draws it (compareArtifacts), a drag as planMove does (sort order, then
 * title), and a placement by sort order alone.
 *
 * Nothing here is a React hook call: the functions are rebuilt each render
 * over that render's artifacts, as they were in ModuleView.
 */
export function useSiblingOrdering({ artifacts, setArtifacts, setError, isBaselineView }: SiblingOrderingOptions) {
  /**
   * Save a sibling group in a given order, re-parenting `reparentId` on the way
   * when the move changed its parent.
   *
   * Renumbering the whole group is the same approach the ▲▼ buttons and paste
   * already use: sort orders are plain integers with no guaranteed gaps, so
   * rewriting 1..n is simpler than finding room between two of them.
   */
  const saveSiblingOrder = async (
    ordered: Artifact[],
    parentId: string | null,
    reparentId?: string
  ) => {
    const updates = ordered
      .map((artifact, index) => ({ artifact, newOrder: index + 1 }))
      .filter(
        ({ artifact, newOrder }) =>
          (artifact.sort_order ?? 0) !== newOrder || artifact.id === reparentId
      );
    if (updates.length === 0) return;

    try {
      const responses = await Promise.all(
        updates.map(({ artifact, newOrder }) =>
          artifactAPI.update(artifact.id, {
            // Only the moved artifact changes parent; its new siblings keep
            // theirs, which is the same value.
            parent_id: artifact.id === reparentId ? parentId : artifact.parent_id ?? null,
            type: artifact.type,
            title: artifact.title,
            body: artifact.body,
            attributes: artifact.attributes,
            sort_order: newOrder,
          })
        )
      );

      const updatedMap = new Map<string, Artifact>(
        responses.map((response) => [response.data.id, response.data])
      );
      setArtifacts(artifacts.map((item) => updatedMap.get(item.id) || item));
      setError('');
    } catch (error: any) {
      console.error('Failed to move artifacts:', error);
      setError(`Failed to move artifacts: ${apiErrorMessage(error, 'Unknown error')}`);
    }
  };

  const handleReorderArtifact = async (
    sourceId: string,
    targetId: string,
    mode: 'swap' | DropZone
  ) => {
    if (isBaselineView) return;

    // The ▲▼ buttons swap two siblings in place; they never re-parent.
    if (mode === 'swap') {
      const source = artifacts.find((item) => item.id === sourceId);
      const target = artifacts.find((item) => item.id === targetId);
      if (!source || !target) return;
      if (normalizeParentId(source.parent_id) !== normalizeParentId(target.parent_id)) return;

      const siblings = artifacts
        .filter((item) => normalizeParentId(item.parent_id) === normalizeParentId(source.parent_id))
        .sort(compareArtifacts);
      const sourceIndex = siblings.findIndex((item) => item.id === sourceId);
      const targetIndex = siblings.findIndex((item) => item.id === targetId);
      if (sourceIndex === -1 || targetIndex === -1) return;

      const reordered = [...siblings];
      reordered[sourceIndex] = siblings[targetIndex];
      reordered[targetIndex] = siblings[sourceIndex];
      await saveSiblingOrder(reordered, normalizeParentId(source.parent_id));
      return;
    }

    // A drag: the planner decides where it lands and refuses the moves that
    // must not happen (into its own subtree, or changing nothing).
    const plan = planMove(artifacts, sourceId, targetId, mode);
    if (!plan) return;
    await saveSiblingOrder(plan.ordered, plan.parentId, plan.reparents ? sourceId : undefined);
  };

  /**
   * Move `created` to sit immediately before or after `anchorId` within its
   * sibling group, by renumbering that group 1..n.
   *
   * Renumbering the whole group is what drag-to-reorder already does: sort
   * orders are plain integers, so there is not always a gap to slot into, and
   * rewriting 1..n is both simpler and always correct. Returns the artifacts
   * that changed, so the caller can fold them into state in one go.
   */
  const placeAmongSiblings = async (
    created: Artifact,
    anchorId: string,
    position: 'before' | 'after',
    pool: Artifact[]
  ): Promise<Artifact[]> => {
    const siblings = pool
      .filter((a) => (a.parent_id ?? null) === (created.parent_id ?? null) && a.id !== created.id)
      .sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0));
    const at = siblings.findIndex((a) => a.id === anchorId);
    if (at === -1) return [];

    const ordered = [...siblings];
    ordered.splice(position === 'before' ? at : at + 1, 0, created);
    const updates = ordered
      .map((a, index) => ({ artifact: a, newOrder: index + 1 }))
      .filter(({ artifact: a, newOrder }) => (a.sort_order ?? 0) !== newOrder);

    const responses = await Promise.all(
      updates.map(({ artifact: a, newOrder }) =>
        artifactAPI.update(a.id, {
          parent_id: a.parent_id ?? null,
          type: a.type,
          title: a.title,
          body: a.body,
          attributes: a.attributes,
          sort_order: newOrder,
        })
      )
    );
    return responses.map((r) => r.data);
  };

  return { handleReorderArtifact, placeAmongSiblings };
}

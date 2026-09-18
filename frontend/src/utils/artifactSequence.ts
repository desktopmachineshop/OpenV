// Reading a project in document order, and stepping through it.
//
// The tree groups artifacts by parent_id and sorts each sibling group by
// sort_order; walking that depth-first, parents before their children, gives
// the order a requirements document is read in — 1, 1.1, 1.1.1, 1.2, 2. That
// is the sequence ‹ and › follow, so "next" always lands where the eye would
// go next rather than somewhere the tree does not show.
//
// The comparator and the hierarchy builder live here because three callers
// need the same answer: the tree that draws the rows, the module that steps
// between them, and the placement logic that inserts among siblings. A second
// opinion about order anywhere would make "next" disagree with the tree.
//
// Everything here is pure.
import { Artifact } from '../api/client';

export const normalizeParentId = (parentId?: string | null): string | null => parentId ?? null;

/**
 * Two artifacts in the order their sibling group is drawn: by sort_order when
 * both carry one, oldest first when neither does, and an ordered artifact
 * before an unordered one — rows that predate sort_order sink to the bottom of
 * their group rather than scattering through it.
 */
export const compareArtifacts = (left: Artifact, right: Artifact): number => {
  const leftOrder = left.sort_order ?? 0;
  const rightOrder = right.sort_order ?? 0;
  const leftHasOrder = leftOrder > 0;
  const rightHasOrder = rightOrder > 0;

  if (leftHasOrder && rightHasOrder) {
    return leftOrder - rightOrder;
  }

  if (!leftHasOrder && !rightHasOrder) {
    return new Date(left.created_at).getTime() - new Date(right.created_at).getTime();
  }

  return leftHasOrder ? -1 : 1;
};

export interface ArtifactTreeNode {
  artifact: Artifact;
  children: ArtifactTreeNode[];
}

/**
 * The artifacts as a tree, each sibling group sorted.
 *
 * An artifact whose parent is not in the set becomes a root. That is what
 * makes the tree usable under a filter: a requirement whose heading was
 * filtered out still has to appear somewhere, and orphaning it silently would
 * drop it from both the tree and the sequence.
 */
export const buildHierarchy = (artifacts: Artifact[]): ArtifactTreeNode[] => {
  const byId = new Map(artifacts.map((artifact) => [artifact.id, artifact]));
  const nodeMap = new Map<string, ArtifactTreeNode>();
  const roots: ArtifactTreeNode[] = [];

  artifacts.forEach((artifact) => {
    nodeMap.set(artifact.id, { artifact, children: [] });
  });

  // The parent to hang an artifact under, or null to make it a root: the
  // parent it names, unless following that chain upwards comes back to the
  // artifact itself. Hanging every member of a cycle under another member
  // leaves the group with no root at all, and everything in it would drop out
  // of both the tree and the sequence.
  const effectiveParentId = (artifact: Artifact): string | null => {
    const parentId = normalizeParentId(artifact.parent_id);
    if (!parentId || !nodeMap.has(parentId)) return null;
    const seen = new Set<string>([artifact.id]);
    let cursor: string | null = parentId;
    while (cursor && nodeMap.has(cursor)) {
      if (seen.has(cursor)) return null;
      seen.add(cursor);
      cursor = normalizeParentId(byId.get(cursor)?.parent_id);
    }
    return parentId;
  };

  artifacts.forEach((artifact) => {
    const node = nodeMap.get(artifact.id)!;
    const parentId = effectiveParentId(artifact);
    const parent = parentId ? nodeMap.get(parentId) : undefined;
    if (parent) {
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  });

  const sortNodes = (nodes: ArtifactTreeNode[]): void => {
    nodes.sort((left, right) => compareArtifacts(left.artifact, right.artifact));
    nodes.forEach((node) => sortNodes(node.children));
  };
  sortNodes(roots);

  return roots;
};

export interface SequenceEntry {
  artifact: Artifact;
  /** How deep in the tree, roots at 0 — the indent a flat list needs. */
  depth: number;
}

/**
 * Every artifact in document order with its depth: depth-first, a parent
 * immediately before its children.
 *
 * Collapsed rows are still in here. Navigation deliberately ignores what the
 * tree has folded away — the tree opens fully collapsed, so honouring it would
 * leave everything below the top level unreachable — and the tree reveals the
 * artifact it lands on instead.
 */
export const documentOrder = (artifacts: Artifact[]): SequenceEntry[] => {
  const out: SequenceEntry[] = [];
  const seen = new Set<string>();
  const walk = (nodes: ArtifactTreeNode[], depth: number) => {
    nodes.forEach((node) => {
      // A cycle below a root would otherwise recurse for ever.
      if (seen.has(node.artifact.id)) return;
      seen.add(node.artifact.id);
      out.push({ artifact: node.artifact, depth });
      walk(node.children, depth + 1);
    });
  };
  walk(buildHierarchy(artifacts), 0);
  return out;
};

/** Where an artifact sits in a sequence: a 1-based position, or null when the
 *  sequence does not hold it — filtered out, or nothing selected. */
export const sequencePosition = (
  sequence: SequenceEntry[],
  artifactId?: string | null
): { position: number; total: number } | null => {
  if (!artifactId) return null;
  const at = sequence.findIndex((entry) => entry.artifact.id === artifactId);
  if (at === -1) return null;
  return { position: at + 1, total: sequence.length };
};

/**
 * The artifact `delta` steps away, or null when there is none: at either end
 * of the sequence, or when the current artifact is not in it at all.
 *
 * Stopping at the ends rather than wrapping is deliberate: a reader who has
 * reached the last requirement should be told so by a dead control, not sent
 * silently back to the first.
 */
export const stepArtifact = (
  sequence: SequenceEntry[],
  artifactId: string | null | undefined,
  delta: number
): Artifact | null => {
  if (!artifactId) return null;
  const at = sequence.findIndex((entry) => entry.artifact.id === artifactId);
  if (at === -1) return null;
  const next = sequence[at + delta];
  return next ? next.artifact : null;
};

/**
 * The ancestors of an artifact, nearest parent first. Used to open the tree
 * onto a row: every one of these has to be expanded for it to be visible.
 */
export const ancestorIds = (artifacts: Artifact[], artifactId?: string | null): string[] => {
  if (!artifactId) return [];
  const byId = new Map(artifacts.map((artifact) => [artifact.id, artifact]));
  const out: string[] = [];
  const seen = new Set<string>([artifactId]);
  let parentId = normalizeParentId(byId.get(artifactId)?.parent_id);
  while (parentId && !seen.has(parentId)) {
    seen.add(parentId);
    out.push(parentId);
    parentId = normalizeParentId(byId.get(parentId)?.parent_id);
  }
  return out;
};

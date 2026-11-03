import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Artifact } from '../api/client';
import { DropZone, dropZoneFor, planMove } from '../utils/artifactDrag';
import { ArtifactTreeNode, ancestorIds, buildHierarchy } from '../utils/artifactSequence';
import type { FactsIndex } from '../utils/artifactFacts';
import { ArtifactActionsMenu, ArtifactContextAction, ArtifactMenuAnchor, siblingNeighbors } from './ArtifactActionsMenu';
import { ArtifactTreeRow, TREE_COLUMNS } from './ArtifactTreeRow';
import { useViewport } from '../hooks/useViewport';

export type { ArtifactContextAction } from './ArtifactActionsMenu';

/** Per-artifact quality summary keyed by artifact id (issue #217). */
export interface QualityRowInfo {
  score: number;
  band: string;
  findingCount: number;
}

interface ArtifactListProps {
  artifacts: Artifact[];
  allArtifacts: Artifact[];
  selectedId?: string;
  onSelect: (id: string) => void;
  /**
   * 'swap' comes from Move up / Move down (the row's menu, or Alt+↑ / Alt+↓ on
   * a focused row). The rest come from a drag or Move to…, and say where the
   * artifact goes: before the target, after it, or inside it.
   */
  onReorder: (sourceId: string, targetId: string, mode: 'swap' | DropZone) => void;
  onContextMenuAction?: (action: ArtifactContextAction, artifact: Artifact) => void;
  /**
   * Whether something has been copied, which is what makes the paste entries
   * usable. Without it they are shown greyed rather than hidden, so the menu
   * keeps the same shape whether or not a copy has happened.
   */
  canPaste?: boolean;
  defaultCollapsed?: boolean;
  collapseAllTrigger?: number;
  expandAllTrigger?: number;
  /** Open every level above `depth` (1 = only the top level shows), once per token. */
  depthTrigger?: { depth: number; token: number };
  readOnly?: boolean;
  /** The requirements module's panes need no "Artifacts" heading over the tree. */
  hideHeading?: boolean;
  /** Quality scores by artifact id; a badge is shown on rows present here. */
  qualityScores?: Record<string, QualityRowInfo>;
  /** Status, trace and verification per row: given, the rows show them as columns. */
  facts?: FactsIndex;
  /** Narrow (a phone, a slim column): one line per row, no columns. */
  compact?: boolean;
}

const parentIdsOf = (nodes: ArtifactTreeNode[], maxDepth = Infinity, depth = 0, ids: string[] = []): string[] => {
  nodes.forEach((node) => {
    if (node.children.length > 0 && depth < maxDepth) {
      ids.push(node.artifact.id);
      parentIdsOf(node.children, maxDepth, depth + 1, ids);
    }
  });
  return ids;
};

export const ArtifactList: React.FC<ArtifactListProps> = ({
  artifacts,
  allArtifacts,
  selectedId,
  onSelect,
  onReorder,
  onContextMenuAction,
  canPaste = false,
  defaultCollapsed = false,
  collapseAllTrigger = 0,
  expandAllTrigger = 0,
  depthTrigger,
  readOnly = false,
  qualityScores,
  hideHeading = false,
  facts,
  compact = false,
}) => {
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [collapsedIds, setCollapsedIds] = useState<Set<string>>(new Set());
  const [menu, setMenu] = useState<ArtifactMenuAnchor | null>(null);
  // Which row a drag is over and which quadrant, so the row can show what
  // letting go would do before it happens.
  const [dropTarget, setDropTarget] = useState<{ id: string; zone: DropZone } | null>(null);
  const hasInitialized = useRef(false);
  const hierarchy = useMemo(() => buildHierarchy(artifacts), [artifacts]);
  const columns = !!facts && !compact;
  // A touch screen has no right-click: every row's ⋯ button is a fingertip
  // wide, and the menu's Move to… does what a drop does (REQ-106). Rows keep
  // the HTML5 drag, which mobile browsers start from a long press.
  const touch = useViewport().coarsePointer;

  useEffect(() => {
    if (!defaultCollapsed || hasInitialized.current) return;
    hasInitialized.current = true;
    setCollapsedIds(new Set(parentIdsOf(hierarchy)));
  }, [defaultCollapsed, hierarchy]);

  useEffect(() => {
    if (collapseAllTrigger === 0) return;
    setCollapsedIds(new Set(parentIdsOf(hierarchy)));
  }, [collapseAllTrigger, hierarchy]);

  useEffect(() => {
    if (expandAllTrigger === 0) return;
    setCollapsedIds(new Set());
  }, [expandAllTrigger]);

  // Depth L1–L4: every parent at that depth or deeper is shut, everything
  // above it open. Keyed on the token, so picking the same depth again resets
  // whatever was opened by hand since.
  const depthToken = depthTrigger?.token ?? 0;
  useEffect(() => {
    if (!depthTrigger || depthToken === 0) return;
    const open = new Set(parentIdsOf(hierarchy, depthTrigger.depth - 1));
    setCollapsedIds(new Set(parentIdsOf(hierarchy).filter((id) => !open.has(id))));
    // Only a new pick re-applies a depth: a refiltered tree keeps what is open.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [depthToken]);

  // A selection can arrive from outside the tree — a deep link, a citation, the
  // review queue, the ‹ / › stepper, J / K, a swipe — and land on a row inside a
  // parent the reader collapsed. So the chain from the selection up to the root
  // is opened: only that chain, and only when the chain itself changes, so a
  // section closed on purpose stays closed unless the selection moves into it.
  // An ancestor the filter excluded is not in the tree either, so walking the
  // filtered set simply finds nothing to open.
  const revealIds = selectedId ? ancestorIds(artifacts, selectedId) : [];
  const revealKey = revealIds.join(' ');
  // revealIds is re-derived every render; the ref keeps the effect's
  // dependency down to the value that actually changed.
  const revealRef = useRef<string[]>(revealIds);
  useEffect(() => {
    revealRef.current = revealIds;
  });
  useEffect(() => {
    if (!revealKey) return;
    setCollapsedIds((prev) => {
      const closed = revealRef.current.filter((id) => prev.has(id));
      if (closed.length === 0) return prev;
      const next = new Set(prev);
      closed.forEach((id) => next.delete(id));
      return next;
    });
  }, [revealKey]);

  const open = (id: string) =>
    setCollapsedIds((prev) => {
      if (!prev.has(id)) return prev;
      const next = new Set(prev);
      next.delete(id);
      return next;
    });

  const toggleCollapsed = (id: string) =>
    setCollapsedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const countDescendants = (node: ArtifactTreeNode): number =>
    node.children.reduce((sum, child) => sum + 1 + countDescendants(child), 0);

  // The rows' own keys: Enter selects, → and ← open and shut, ↑ and ↓ move
  // the focus, and Alt+↑ / Alt+↓ move the artifact among its siblings, which
  // is what the stacked ▲ ▼ buttons beside every row used to do.
  const onRowKey = (event: React.KeyboardEvent<HTMLDivElement>, node: ArtifactTreeNode) => {
    const { artifact } = node;
    if (event.target !== event.currentTarget) return;
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onSelect(artifact.id);
    } else if (event.altKey && (event.key === 'ArrowUp' || event.key === 'ArrowDown')) {
      if (readOnly) return;
      event.preventDefault();
      const neighbor = siblingNeighbors(allArtifacts, artifact)[event.key === 'ArrowUp' ? 'up' : 'down'];
      if (neighbor) onReorder(artifact.id, neighbor, 'swap');
    } else if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
      event.preventDefault();
      const rows = Array.from(
        event.currentTarget.closest('[role="tree"]')?.querySelectorAll<HTMLElement>('[role="treeitem"]') ?? []
      );
      const at = rows.indexOf(event.currentTarget);
      rows[at + (event.key === 'ArrowUp' ? -1 : 1)]?.focus();
    } else if (event.key === 'ArrowRight' && node.children.length && collapsedIds.has(artifact.id)) {
      event.preventDefault();
      toggleCollapsed(artifact.id);
    } else if (event.key === 'ArrowLeft' && node.children.length && !collapsedIds.has(artifact.id)) {
      event.preventDefault();
      toggleCollapsed(artifact.id);
    }
  };

  const renderArtifact = (node: ArtifactTreeNode, depth: number, guides: boolean[]): React.ReactNode => {
    const { artifact } = node;
    const collapsed = collapsedIds.has(artifact.id);
    const drop = dropTarget && dropTarget.id === artifact.id ? dropTarget.zone : null;
    const last = (index: number) => index === node.children.length - 1;
    return (
      <React.Fragment key={artifact.id}>
        <ArtifactTreeRow
          node={node}
          depth={depth}
          guides={guides}
          collapsed={collapsed}
          descendants={node.children.length ? countDescendants(node) : 0}
          selected={selectedId === artifact.id}
          drop={drop}
          dragging={draggingId === artifact.id}
          readOnly={readOnly}
          touch={touch}
          columns={columns}
          facts={facts}
          quality={qualityScores?.[artifact.id]}
          onSelect={() => onSelect(artifact.id)}
          onToggle={() => toggleCollapsed(artifact.id)}
          onKeyDown={(event) => onRowKey(event, node)}
          onMenu={(x, y) => setMenu({ x, y, artifactId: artifact.id })}
          dragHandlers={{
            onDragStart: (event) => {
              if (readOnly) return;
              event.dataTransfer.setData('text/plain', artifact.id);
              event.dataTransfer.effectAllowed = 'move';
              setDraggingId(artifact.id);
            },
            onDragOver: (event) => {
              if (readOnly) return;
              // dataTransfer is unreadable during dragover in most browsers, so
              // the dragged id comes from state; the id is only read on drop.
              if (!draggingId || draggingId === artifact.id) return;
              const zone = dropZoneFor(event.currentTarget.getBoundingClientRect(), event.clientX, event.clientY);
              // The planner refuses a drop into the dragged artifact's own
              // subtree and one that changes nothing; the row should not invite
              // a drop that would be ignored.
              if (!planMove(allArtifacts, draggingId, artifact.id, zone)) {
                setDropTarget(null);
                return;
              }
              event.preventDefault();
              event.dataTransfer.dropEffect = 'move';
              setDropTarget((prev) => (prev && prev.id === artifact.id && prev.zone === zone ? prev : { id: artifact.id, zone }));
            },
            onDragLeave: (event) => {
              // Leaving for a child element of the same row is not leaving.
              if (event.currentTarget.contains(event.relatedTarget as Node | null)) return;
              setDropTarget((prev) => (prev && prev.id === artifact.id ? null : prev));
            },
            onDrop: (event) => {
              if (readOnly) return;
              event.preventDefault();
              const draggedId = event.dataTransfer.getData('text/plain') || draggingId;
              const zone = dropZoneFor(event.currentTarget.getBoundingClientRect(), event.clientX, event.clientY);
              setDropTarget(null);
              setDraggingId(null);
              if (!draggedId || draggedId === artifact.id) return;
              if (!planMove(allArtifacts, draggedId, artifact.id, zone)) return;
              // A drop into a collapsed parent would otherwise vanish from view.
              if (zone === 'child') open(artifact.id);
              onReorder(draggedId, artifact.id, zone);
            },
            onDragEnd: () => {
              setDraggingId(null);
              setDropTarget(null);
            },
          }}
        />
        {!collapsed && node.children.map((child, index) => renderArtifact(child, depth + 1, [...guides, !last(index)]))}
      </React.Fragment>
    );
  };

  return (
    // No card around the tree: the list is the column's content, and it fills
    // the height it is given rather than stopping at a fixed height.
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0 }}>
      {!hideHeading && <h3 style={{ marginTop: 0 }}>Artifacts</h3>}
      {artifacts.length === 0 ? (
        <p>No artifacts yet. Create one to get started.</p>
      ) : (
        <div
          style={{
            overflow: 'auto',
            flex: 1,
            minHeight: 0,
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 6,
          }}
          data-help-key="requirements.navigate"
        >
          <div role="tree" aria-label="Requirements tree" style={{ minWidth: columns ? 820 : undefined }}>
            {columns && (
              <div
                aria-hidden="true"
                style={{
                  display: 'grid',
                  gridTemplateColumns: TREE_COLUMNS,
                  alignItems: 'center',
                  height: 30,
                  padding: '0 8px 0 12px',
                  background: 'var(--surface-inset)',
                  borderBottom: '1px solid var(--border)',
                  color: 'var(--text-muted)',
                  fontSize: 12,
                  fontWeight: 600,
                  textTransform: 'uppercase',
                  letterSpacing: '0.04em',
                  position: 'sticky',
                  top: 0,
                  zIndex: 1,
                }}
              >
                <span>Ref · Title</span>
                <span>Status</span>
                <span>Prio</span>
                <span>Quality</span>
                <span>Trace</span>
                <span>V&amp;V below</span>
                <span style={{ textAlign: 'right' }}>Upd.</span>
                <span />
              </div>
            )}
            {hierarchy.map((node) => renderArtifact(node, 0, []))}
          </div>
        </div>
      )}

      {!readOnly && (
        <ArtifactActionsMenu
          menu={menu}
          onClose={() => setMenu(null)}
          allArtifacts={allArtifacts}
          canPaste={canPaste}
          onContextMenuAction={onContextMenuAction}
          onReorder={onReorder}
          onRevealParent={open}
        />
      )}
    </div>
  );
};

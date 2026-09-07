import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Artifact } from '../api/client';
import { DropZone, dropZoneFor, planMove } from '../utils/artifactDrag';
import { QualityBadge } from './QualityBadge';
import { Modal } from './ui/Modal';
import { useViewport } from '../hooks/useViewport';

/** Per-artifact quality summary keyed by artifact id (issue #217). */
export interface QualityRowInfo {
  score: number;
  band: string;
  findingCount: number;
}

/** What the artifact context menu can ask the view to do. */
export type ArtifactContextAction =
  | 'create-before'
  | 'create-after'
  | 'create-child'
  | 'copy'
  | 'paste-before'
  | 'paste-after'
  | 'duplicate';

interface ArtifactListProps {
  artifacts: Artifact[];
  allArtifacts: Artifact[];
  selectedId?: string;
  onSelect: (id: string) => void;
  /**
   * 'swap' comes from the ▲▼ buttons. The rest come from a drag, and say where
   * the pointer let go: before the target, after it, or inside it.
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
  readOnly?: boolean;
  /** Stacked on a phone the pane's segmented control already says "Tree". */
  hideHeading?: boolean;
  /** Quality scores by artifact id; a badge is shown on rows present here. */
  qualityScores?: Record<string, QualityRowInfo>;
}

interface ArtifactTreeNode {
  artifact: Artifact;
  children: ArtifactTreeNode[];
}

const normalizeParentId = (parentId?: string | null): string | null => parentId ?? null;

const compareArtifacts = (left: Artifact, right: Artifact): number => {
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

const buildHierarchy = (artifacts: Artifact[]): ArtifactTreeNode[] => {
  const nodeMap = new Map<string, ArtifactTreeNode>();
  const roots: ArtifactTreeNode[] = [];

  // Create nodes for all artifacts
  artifacts.forEach((artifact) => {
    nodeMap.set(artifact.id, { artifact, children: [] });
  });

  // Build tree structure
  artifacts.forEach((artifact) => {
    const node = nodeMap.get(artifact.id)!;
    if (artifact.parent_id && nodeMap.has(artifact.parent_id)) {
      nodeMap.get(artifact.parent_id)!.children.push(node);
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
  readOnly = false,
  qualityScores,
  hideHeading = false,
}) => {
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [collapsedIds, setCollapsedIds] = useState<Set<string>>(new Set());
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; artifactId: string } | null>(null);
  // Which row a drag is over and which quadrant, so the row can show what
  // letting go would do before it happens.
  const [dropTarget, setDropTarget] = useState<{ id: string; zone: DropZone } | null>(null);
  const hasInitialized = useRef(false);
  // Touch parity (docs/plans/mobile-support.md, REQ-106): a touch screen has
  // no right-click, so every row carries a visible actions button and the
  // menu gains a Move to… dialog that does what a drop does. Rows keep the
  // HTML5 drag: mobile browsers start it from a long press on the row.
  const viewport = useViewport();
  const touch = viewport.coarsePointer;
  const [moveDialogFor, setMoveDialogFor] = useState<Artifact | null>(null);
  const [moveTargetId, setMoveTargetId] = useState('');
  const [moveZone, setMoveZone] = useState<DropZone>('after');

  const hierarchy = useMemo(() => buildHierarchy(artifacts), [artifacts]);

  const collectParentIds = (nodes: ArtifactTreeNode[]): string[] => {
    const ids: string[] = [];
    const walk = (nodeList: ArtifactTreeNode[]) => {
      nodeList.forEach((node) => {
        if (node.children.length > 0) {
          ids.push(node.artifact.id);
        }
        if (node.children.length > 0) {
          walk(node.children);
        }
      });
    };
    walk(nodes);
    return ids;
  };

  useEffect(() => {
    if (!defaultCollapsed || hasInitialized.current) {
      return;
    }
    hasInitialized.current = true;
    const parentIds = collectParentIds(hierarchy);
    setCollapsedIds(new Set(parentIds));
  }, [defaultCollapsed, hierarchy]);

  useEffect(() => {
    if (collapseAllTrigger === 0) return;
    const parentIds = collectParentIds(hierarchy);
    setCollapsedIds(new Set(parentIds));
  }, [collapseAllTrigger, hierarchy]);

  useEffect(() => {
    if (expandAllTrigger === 0) return;
    setCollapsedIds(new Set());
  }, [expandAllTrigger]);

  const getSiblingOrder = (artifact: Artifact): Artifact[] => {
    const parentId = normalizeParentId(artifact.parent_id);
    return allArtifacts
      .filter((item) => normalizeParentId(item.parent_id) === parentId)
      .sort(compareArtifacts);
  };

  const getNeighborIds = (artifact: Artifact): { up?: string; down?: string } => {
    const siblings = getSiblingOrder(artifact);
    const index = siblings.findIndex((item) => item.id === artifact.id);
    return {
      up: index > 0 ? siblings[index - 1].id : undefined,
      down: index >= 0 && index < siblings.length - 1 ? siblings[index + 1].id : undefined,
    };
  };

  const isCollapsed = (artifactId: string): boolean => collapsedIds.has(artifactId);

  const toggleCollapsed = (artifactId: string) => {
    setCollapsedIds((prev) => {
      const next = new Set(prev);
      if (next.has(artifactId)) {
        next.delete(artifactId);
      } else {
        next.add(artifactId);
      }
      return next;
    });
  };

  const openMoveDialog = (artifact: Artifact) => {
    // Default to "after the next sibling", the commonest small move; the
    // dialog only offers placements the planner would accept.
    const neighbors = getNeighborIds(artifact);
    setMoveTargetId(neighbors.down || neighbors.up || '');
    setMoveZone(neighbors.down ? 'after' : 'before');
    setMoveDialogFor(artifact);
  };

  const submitMove = () => {
    if (!moveDialogFor || !moveTargetId) return;
    if (!planMove(allArtifacts, moveDialogFor.id, moveTargetId, moveZone)) return;
    if (moveZone === 'child') {
      setCollapsedIds((prev) => {
        if (!prev.has(moveTargetId)) return prev;
        const next = new Set(prev);
        next.delete(moveTargetId);
        return next;
      });
    }
    onReorder(moveDialogFor.id, moveTargetId, moveZone);
    setMoveDialogFor(null);
  };

  // Every artifact in document order with its depth, for the Move to…
  // picker: a flat list the native select can show with indentation.
  const flattened = useMemo(() => {
    const out: { artifact: Artifact; depth: number }[] = [];
    const walk = (nodes: ArtifactTreeNode[], depth: number) => {
      nodes.forEach((node) => {
        out.push({ artifact: node.artifact, depth });
        walk(node.children, depth + 1);
      });
    };
    walk(buildHierarchy(allArtifacts), 0);
    return out;
  }, [allArtifacts]);

  const renderArtifact = (node: ArtifactTreeNode, depth: number = 0): React.ReactNode => {
    const { artifact } = node;
    const indentPx = depth * 20;
    const neighborIds = getNeighborIds(artifact);
    const hasChildren = node.children.length > 0;
    const collapsed = isCollapsed(artifact.id);
    const countDescendants = (current: ArtifactTreeNode): number => {
      return current.children.reduce((sum, child) => sum + 1 + countDescendants(child), 0);
    };
    const descendantCount = hasChildren ? countDescendants(node) : 0;
    const drop = dropTarget && dropTarget.id === artifact.id ? dropTarget.zone : null;

    return (
      <React.Fragment key={artifact.id}>
        <div
          onClick={() => onSelect(artifact.id)}
          onContextMenu={(event) => {
            event.preventDefault();
            if (!readOnly) {
              setContextMenu({ x: event.clientX, y: event.clientY, artifactId: artifact.id });
            }
          }}
          draggable={!readOnly}
          onDragStart={(event) => {
            if (readOnly) return;
            event.dataTransfer.setData('text/plain', artifact.id);
            event.dataTransfer.effectAllowed = 'move';
            setDraggingId(artifact.id);
          }}
          onDragOver={(event) => {
            if (readOnly) return;
            // dataTransfer is unreadable during dragover in most browsers, so
            // the dragged id comes from state; the id is only read on drop.
            const draggedId = draggingId;
            if (!draggedId || draggedId === artifact.id) return;
            const box = event.currentTarget.getBoundingClientRect();
            const zone = dropZoneFor(box, event.clientX, event.clientY);
            // Ask the planner rather than guessing: it refuses a drop into the
            // dragged artifact's own subtree and one that changes nothing, and
            // the row should not invite a drop that would be ignored.
            if (!planMove(allArtifacts, draggedId, artifact.id, zone)) {
              setDropTarget(null);
              return;
            }
            event.preventDefault();
            event.dataTransfer.dropEffect = 'move';
            setDropTarget((prev) =>
              prev && prev.id === artifact.id && prev.zone === zone
                ? prev
                : { id: artifact.id, zone }
            );
          }}
          onDragLeave={(event) => {
            // Leaving for a child element of the same row is not leaving.
            if (event.currentTarget.contains(event.relatedTarget as Node | null)) return;
            setDropTarget((prev) => (prev && prev.id === artifact.id ? null : prev));
          }}
          onDrop={(event) => {
            if (readOnly) return;
            event.preventDefault();
            const draggedId = event.dataTransfer.getData('text/plain') || draggingId;
            const box = event.currentTarget.getBoundingClientRect();
            const zone = dropZoneFor(box, event.clientX, event.clientY);
            setDropTarget(null);
            setDraggingId(null);
            if (!draggedId || draggedId === artifact.id) return;
            if (!planMove(allArtifacts, draggedId, artifact.id, zone)) return;
            // A drop into a collapsed parent would otherwise vanish from view.
            if (zone === 'child') {
              setCollapsedIds((prev) => {
                if (!prev.has(artifact.id)) return prev;
                const next = new Set(prev);
                next.delete(artifact.id);
                return next;
              });
            }
            onReorder(draggedId, artifact.id, zone);
          }}
          onDragEnd={() => {
            setDraggingId(null);
            setDropTarget(null);
          }}
          style={{
            position: 'relative',
            padding: '12px',
            paddingLeft: `${12 + indentPx}px`,
            borderBottom: '1px solid var(--border-soft)',
            cursor: 'pointer',
            backgroundColor:
              drop === 'child'
                ? 'var(--tint-blue)'
                : selectedId === artifact.id
                ? 'var(--tint-blue)'
                : 'transparent',
            transition: 'background-color 0.2s',
            // A sibling drop shows a line on the edge it would land against; a
            // child drop outlines the row it would go inside.
            boxShadow:
              drop === 'before'
                ? 'inset 0 2px 0 0 var(--accent)'
                : drop === 'after'
                ? 'inset 0 -2px 0 0 var(--accent)'
                : 'none',
            outline: drop === 'child' ? '2px solid var(--accent)' : 'none',
            outlineOffset: drop === 'child' ? '-2px' : undefined,
            opacity: draggingId === artifact.id ? 0.6 : 1,
          }}
          onMouseOver={(e) => {
            if (selectedId !== artifact.id) {
              e.currentTarget.style.backgroundColor = 'var(--surface-alt)';
            }
          }}
          onMouseOut={(e) => {
            if (selectedId !== artifact.id) {
              e.currentTarget.style.backgroundColor = 'transparent';
            }
          }}
        >
          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
            <div style={{ flex: 1 }}>
              <div style={{ fontWeight: 600, color: 'var(--text)', display: 'flex', alignItems: 'center', gap: '8px' }}>
                {depth > 0 && (
                  <span style={{ color: 'var(--neutral)', fontSize: '10px' }}>└</span>
                )}
                {hasChildren && (
                  <button
                    onClick={(event) => {
                      event.stopPropagation();
                      toggleCollapsed(artifact.id);
                    }}
                    aria-label={collapsed ? 'Expand children' : 'Collapse children'}
                    style={{
                      border: 'none',
                      background: 'transparent',
                      padding: 0,
                      cursor: 'pointer',
                      color: 'var(--text-muted)',
                      fontSize: '12px',
                      width: '14px',
                      textAlign: 'center',
                    }}
                    title={collapsed ? 'Expand' : 'Collapse'}
                  >
                    {collapsed ? '▸' : '▾'}
                  </button>
                )}
                {/* A section shows where it sits ("1.2"); everything else
                    shows the address a reader cites ("REQ-12"). */}
                {artifact.doc_number && (
                  <span style={{ color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}>
                    {artifact.doc_number}
                  </span>
                )}
                {artifact.title}
              </div>
              <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px', display: 'flex', alignItems: 'center', gap: '6px', flexWrap: 'wrap' }}>
                {artifact.ref && !artifact.doc_number && (
                  <code
                    style={{
                      background: 'var(--surface-alt)',
                      border: '1px solid var(--border-soft)',
                      borderRadius: 3,
                      padding: '0 4px',
                      fontSize: '11px',
                    }}
                    title="Stable reference — cite this; it never changes or gets reused"
                  >
                    {artifact.ref}
                  </code>
                )}
                <span>
                  {artifact.type} • v{artifact.version}
                  {hasChildren && (
                    <span style={{ marginLeft: '6px', color: 'var(--neutral)' }}>
                      {collapsed ? `(collapsed · ${descendantCount})` : `(${node.children.length})`}
                    </span>
                  )}
                </span>
                {qualityScores?.[artifact.id] && (
                  <QualityBadge
                    score={qualityScores[artifact.id].score}
                    band={qualityScores[artifact.id].band}
                    findingCount={qualityScores[artifact.id].findingCount}
                  />
                )}
              </div>
            </div>
            {!readOnly && (
              <div style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <button
                  type="button"
                  aria-label={`Actions for ${artifact.title}`}
                  title="More actions"
                  onClick={(event) => {
                    event.stopPropagation();
                    const box = event.currentTarget.getBoundingClientRect();
                    setContextMenu({ x: Math.max(8, box.right - 200), y: box.bottom + 4, artifactId: artifact.id });
                  }}
                  style={{
                    width: touch ? 44 : 24,
                    minHeight: touch ? 44 : 24,
                    border: '1px solid var(--border)',
                    borderRadius: 3,
                    background: 'var(--neutral-soft)',
                    color: 'var(--text)',
                    fontSize: touch ? 20 : 14,
                    lineHeight: 1,
                    cursor: 'pointer',
                    padding: 0,
                  }}
                >
                  ⋯
                </button>
                <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      if (neighborIds.up) {
                        onReorder(artifact.id, neighborIds.up, 'swap');
                      }
                    }}
                    disabled={!neighborIds.up}
                    title="Move up"
                    style={{
                      backgroundColor: neighborIds.up ? 'var(--neutral-soft)' : 'var(--surface-inset)',
                      color: 'var(--text)',
                      border: '1px solid var(--border)',
                      padding: touch ? '0 10px' : '2px 6px',
                      minHeight: touch ? 20 : undefined,
                      borderRadius: '3px',
                      cursor: neighborIds.up ? 'pointer' : 'not-allowed',
                      fontSize: '10px',
                      lineHeight: 1,
                    }}
                  >
                    ▲
                  </button>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      if (neighborIds.down) {
                        onReorder(artifact.id, neighborIds.down, 'swap');
                      }
                    }}
                    disabled={!neighborIds.down}
                    title="Move down"
                    style={{
                      backgroundColor: neighborIds.down ? 'var(--neutral-soft)' : 'var(--surface-inset)',
                      color: 'var(--text)',
                      border: '1px solid var(--border)',
                      padding: touch ? '0 10px' : '2px 6px',
                      minHeight: touch ? 20 : undefined,
                      borderRadius: '3px',
                      cursor: neighborIds.down ? 'pointer' : 'not-allowed',
                      fontSize: '10px',
                      lineHeight: 1,
                    }}
                  >
                    ▼
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
        {!collapsed && node.children.map((child) => renderArtifact(child, depth + 1))}
      </React.Fragment>
    );
  };

  return (
    // No card around the tree: the list is the column's content, and a panel
    // inside a panel only wasted the width. It fills the height it is given
    // rather than stopping at a fixed 500px with empty space beneath.
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0 }}>
      {!hideHeading && <h3 style={{ marginTop: 0 }}>Artifacts</h3>}
      {artifacts.length === 0 ? (
        <p>No artifacts yet. Create one to get started.</p>
      ) : (
        <div style={{ overflowY: 'auto', flex: 1, minHeight: 0 }}>
          {hierarchy.map((node) => renderArtifact(node))}
        </div>
      )}

      {/* Context Menu */}
      {contextMenu && (
        <>
          <div
            style={{
              position: 'fixed',
              inset: 0,
              zIndex: 999,
            }}
            onClick={() => setContextMenu(null)}
            onContextMenu={(e) => e.preventDefault()}
          />
          <div
            style={{
              position: 'fixed',
              left: `${Math.min(contextMenu.x, Math.max(8, window.innerWidth - 208))}px`,
              top: `${Math.min(contextMenu.y, Math.max(8, window.innerHeight - 340))}px`,
              backgroundColor: 'var(--surface)',
              border: '1px solid var(--border)',
              borderRadius: '4px',
              boxShadow: '0 2px 8px rgba(0,0,0,0.15)',
              zIndex: 1000,
              minWidth: '180px',
            }}
          >
            {([
              { action: 'create-before' as const, label: '➕ Create before' },
              { action: 'create-after' as const, label: '➕ Create after' },
              { action: 'create-child' as const, label: '➕ Create new child' },
              { action: 'move-to' as const, label: '↕️ Move to…', separated: true },
              { action: 'copy' as const, label: '📋 Copy', separated: true },
              { action: 'duplicate' as const, label: '🧬 Duplicate' },
              { action: 'paste-before' as const, label: '📥 Paste before', needsClipboard: true },
              { action: 'paste-after' as const, label: '📥 Paste after', needsClipboard: true },
            ]).map((item, index, all) => {
              const disabled = !!item.needsClipboard && !canPaste;
              return (
                <button
                  key={item.action}
                  disabled={disabled}
                  title={disabled ? 'Copy an artifact first' : undefined}
                  onClick={() => {
                    const artifact = allArtifacts.find((a) => a.id === contextMenu.artifactId);
                    if (artifact) {
                      if (item.action === 'move-to') {
                        openMoveDialog(artifact);
                      } else if (onContextMenuAction) {
                        onContextMenuAction(item.action, artifact);
                      }
                    }
                    setContextMenu(null);
                  }}
                  style={{
                    display: 'block',
                    width: '100%',
                    padding: touch ? '12px 14px' : '8px 12px',
                    border: 'none',
                    backgroundColor: 'transparent',
                    textAlign: 'left',
                    cursor: disabled ? 'not-allowed' : 'pointer',
                    fontSize: touch ? '14px' : '12px',
                    color: disabled ? 'var(--neutral)' : 'var(--text)',
                    borderBottom: index < all.length - 1 ? '1px solid var(--neutral-soft)' : 'none',
                    // A rule above Copy separates making new artifacts from
                    // moving this one around.
                    borderTop: item.separated ? '1px solid var(--border)' : undefined,
                  }}
                  onMouseEnter={(e) => {
                    if (!disabled) e.currentTarget.style.backgroundColor = 'var(--tint-blue)';
                  }}
                  onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = 'transparent')}
                >
                  {item.label}
                </button>
              );
            })}
          </div>
        </>
      )}

      {moveDialogFor && (
        <Modal title={`Move “${moveDialogFor.title}”`} width={420} onClose={() => setMoveDialogFor(null)}>
          <label htmlFor="move-target" style={{ fontSize: 12 }}>
            Relative to
          </label>
          <select
            id="move-target"
            value={moveTargetId}
            onChange={(e) => setMoveTargetId(e.target.value)}
            style={{ marginBottom: 12 }}
          >
            <option value="">Choose an artifact…</option>
            {flattened
              .filter(({ artifact }) => artifact.id !== moveDialogFor.id)
              .map(({ artifact, depth }) => (
                <option
                  key={artifact.id}
                  value={artifact.id}
                  disabled={!planMove(allArtifacts, moveDialogFor.id, artifact.id, moveZone)}
                >
                  {'\u00a0\u00a0'.repeat(depth)}
                  {artifact.ref ? `${artifact.ref} ` : ''}
                  {artifact.title}
                </option>
              ))}
          </select>
          <div role="radiogroup" aria-label="Placement" style={{ display: 'flex', gap: 8, marginBottom: 16, flexWrap: 'wrap' }}>
            {([
              { zone: 'before' as const, label: 'Before it' },
              { zone: 'after' as const, label: 'After it' },
              { zone: 'child' as const, label: 'Inside it' },
            ]).map((opt) => (
              <label
                key={opt.zone}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 6,
                  minHeight: 44,
                  padding: '0 12px',
                  border: '1px solid var(--border)',
                  borderRadius: 4,
                  background: moveZone === opt.zone ? 'var(--tint-blue)' : 'var(--surface)',
                  cursor: 'pointer',
                  flex: '1 1 auto',
                  fontWeight: 500,
                }}
              >
                <input
                  type="radio"
                  name="move-zone"
                  value={opt.zone}
                  checked={moveZone === opt.zone}
                  onChange={() => setMoveZone(opt.zone)}
                  style={{ width: 'auto' }}
                />
                {opt.label}
              </label>
            ))}
          </div>
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10 }}>
            <button className="button-secondary" onClick={() => setMoveDialogFor(null)} style={{ minHeight: 44 }}>
              Cancel
            </button>
            <button
              className="button"
              onClick={submitMove}
              disabled={!moveTargetId || !planMove(allArtifacts, moveDialogFor.id, moveTargetId, moveZone)}
              style={{ minHeight: 44 }}
            >
              Move
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
};

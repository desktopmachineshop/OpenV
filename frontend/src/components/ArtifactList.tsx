import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Artifact } from '../api/client';

interface ArtifactListProps {
  artifacts: Artifact[];
  allArtifacts: Artifact[];
  selectedId?: string;
  onSelect: (id: string) => void;
  onReorder: (sourceId: string, targetId: string, mode: 'swap' | 'insert') => void;
  onContextMenuAction?: (action: 'create-before' | 'create-after' | 'create-child', artifact: Artifact) => void;
  defaultCollapsed?: boolean;
  collapseAllTrigger?: number;
  expandAllTrigger?: number;
  readOnly?: boolean;
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
  defaultCollapsed = false,
  collapseAllTrigger = 0,
  expandAllTrigger = 0,
  readOnly = false,
}) => {
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [dragOverId, setDragOverId] = useState<string | null>(null);
  const [collapsedIds, setCollapsedIds] = useState<Set<string>>(new Set());
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; artifactId: string } | null>(null);
  const hasInitialized = useRef(false);

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
            const draggedId = draggingId || event.dataTransfer.getData('text/plain');
            if (!draggedId || draggedId === artifact.id) {
              return;
            }
            const dragged = allArtifacts.find((item) => item.id === draggedId);
            if (!dragged) {
              return;
            }
            if (normalizeParentId(dragged.parent_id) !== normalizeParentId(artifact.parent_id)) {
              return;
            }
            event.preventDefault();
            setDragOverId(artifact.id);
          }}
          onDragLeave={() => setDragOverId(null)}
          onDrop={(event) => {
            if (readOnly) return;
            event.preventDefault();
            const draggedId = event.dataTransfer.getData('text/plain');
            setDragOverId(null);
            setDraggingId(null);
            if (!draggedId || draggedId === artifact.id) {
              return;
            }
            const dragged = allArtifacts.find((item) => item.id === draggedId);
            if (!dragged) {
              return;
            }
            if (normalizeParentId(dragged.parent_id) !== normalizeParentId(artifact.parent_id)) {
              return;
            }
            onReorder(draggedId, artifact.id, 'insert');
          }}
          onDragEnd={() => {
            setDraggingId(null);
            setDragOverId(null);
          }}
          style={{
            padding: '12px',
            paddingLeft: `${12 + indentPx}px`,
            borderBottom: '1px solid #eee',
            cursor: 'pointer',
            backgroundColor:
              selectedId === artifact.id ? '#e8f4f8' : 'transparent',
            transition: 'background-color 0.2s',
            outline: dragOverId === artifact.id ? '2px dashed #7f8c8d' : 'none',
            opacity: draggingId === artifact.id ? 0.6 : 1,
          }}
          onMouseOver={(e) => {
            if (selectedId !== artifact.id) {
              e.currentTarget.style.backgroundColor = '#f9f9f9';
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
              <div style={{ fontWeight: 600, color: '#2c3e50', display: 'flex', alignItems: 'center', gap: '8px' }}>
                {depth > 0 && (
                  <span style={{ color: '#95a5a6', fontSize: '10px' }}>└</span>
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
                      color: '#7f8c8d',
                      fontSize: '12px',
                      width: '14px',
                      textAlign: 'center',
                    }}
                    title={collapsed ? 'Expand' : 'Collapse'}
                  >
                    {collapsed ? '▸' : '▾'}
                  </button>
                )}
                {artifact.title}
              </div>
              <div style={{ fontSize: '12px', color: '#7f8c8d', marginTop: '4px' }}>
                {artifact.type} • v{artifact.version}
                {hasChildren && (
                  <span style={{ marginLeft: '6px', color: '#95a5a6' }}>
                    {collapsed ? `(collapsed · ${descendantCount})` : `(${node.children.length})`}
                  </span>
                )}
              </div>
            </div>
            {!readOnly && (
              <div style={{ display: 'flex', gap: '8px' }}>
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
                      backgroundColor: neighborIds.up ? '#ecf0f1' : '#f5f5f5',
                      color: '#2c3e50',
                      border: '1px solid #dcdde1',
                      padding: '2px 6px',
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
                      backgroundColor: neighborIds.down ? '#ecf0f1' : '#f5f5f5',
                      color: '#2c3e50',
                      border: '1px solid #dcdde1',
                      padding: '2px 6px',
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
    <div className="card">
      <h3>Artifacts</h3>
      {artifacts.length === 0 ? (
        <p>No artifacts yet. Create one to get started.</p>
      ) : (
        <div style={{ overflowY: 'auto', maxHeight: '500px' }}>
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
              left: `${contextMenu.x}px`,
              top: `${contextMenu.y}px`,
              backgroundColor: 'white',
              border: '1px solid #dcdde1',
              borderRadius: '4px',
              boxShadow: '0 2px 8px rgba(0,0,0,0.15)',
              zIndex: 1000,
              minWidth: '180px',
            }}
          >
            <button
              onClick={() => {
                const artifact = allArtifacts.find(a => a.id === contextMenu.artifactId);
                if (artifact && onContextMenuAction) {
                  onContextMenuAction('create-before', artifact);
                }
                setContextMenu(null);
              }}
              style={{
                display: 'block',
                width: '100%',
                padding: '8px 12px',
                border: 'none',
                backgroundColor: 'transparent',
                textAlign: 'left',
                cursor: 'pointer',
                fontSize: '12px',
                color: '#2c3e50',
                borderBottom: '1px solid #ecf0f1',
              }}
              onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = '#f0f8ff')}
              onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = 'transparent')}
            >
              ➕ Create before
            </button>
            <button
              onClick={() => {
                const artifact = allArtifacts.find(a => a.id === contextMenu.artifactId);
                if (artifact && onContextMenuAction) {
                  onContextMenuAction('create-after', artifact);
                }
                setContextMenu(null);
              }}
              style={{
                display: 'block',
                width: '100%',
                padding: '8px 12px',
                border: 'none',
                backgroundColor: 'transparent',
                textAlign: 'left',
                cursor: 'pointer',
                fontSize: '12px',
                color: '#2c3e50',
                borderBottom: '1px solid #ecf0f1',
              }}
              onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = '#f0f8ff')}
              onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = 'transparent')}
            >
              ➕ Create after
            </button>
            <button
              onClick={() => {
                const artifact = allArtifacts.find(a => a.id === contextMenu.artifactId);
                if (artifact && onContextMenuAction) {
                  onContextMenuAction('create-child', artifact);
                }
                setContextMenu(null);
              }}
              style={{
                display: 'block',
                width: '100%',
                padding: '8px 12px',
                border: 'none',
                backgroundColor: 'transparent',
                textAlign: 'left',
                cursor: 'pointer',
                fontSize: '12px',
                color: '#2c3e50',
              }}
              onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = '#f0f8ff')}
              onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = 'transparent')}
            >
              ➕ Create new child
            </button>
          </div>
        </>
      )}
    </div>
  );
};

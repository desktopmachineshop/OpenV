import React from 'react';
import { Artifact } from '../api/client';

interface ArtifactListProps {
  artifacts: Artifact[];
  selectedId?: string;
  onSelect: (id: string) => void;
  onEdit: (artifact: Artifact) => void;
  onDelete: (id: string) => void;
  readOnly?: boolean;
}

interface ArtifactTreeNode {
  artifact: Artifact;
  children: ArtifactTreeNode[];
}

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

  return roots;
};

export const ArtifactList: React.FC<ArtifactListProps> = ({
  artifacts,
  selectedId,
  onSelect,
  onEdit,
  onDelete,
  readOnly = false,
}) => {
  const hierarchy = buildHierarchy(artifacts);

  const renderArtifact = (node: ArtifactTreeNode, depth: number = 0): React.ReactNode => {
    const { artifact } = node;
    const indentPx = depth * 20;

    return (
      <React.Fragment key={artifact.id}>
        <div
          onClick={() => onSelect(artifact.id)}
          style={{
            padding: '12px',
            paddingLeft: `${12 + indentPx}px`,
            borderBottom: '1px solid #eee',
            cursor: 'pointer',
            backgroundColor:
              selectedId === artifact.id ? '#e8f4f8' : 'transparent',
            transition: 'background-color 0.2s',
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
                {artifact.title}
              </div>
              <div style={{ fontSize: '12px', color: '#7f8c8d', marginTop: '4px' }}>
                {artifact.type} • v{artifact.version}
              </div>
            </div>
            {!readOnly && (
              <div style={{ display: 'flex', gap: '8px' }}>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onEdit(artifact);
                  }}
                  style={{
                    backgroundColor: '#3498db',
                    color: 'white',
                    border: 'none',
                    padding: '4px 10px',
                    borderRadius: '3px',
                    cursor: 'pointer',
                    fontSize: '12px',
                  }}
                >
                  Edit
                </button>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onDelete(artifact.id);
                  }}
                  style={{
                    backgroundColor: '#e74c3c',
                    color: 'white',
                    border: 'none',
                    padding: '4px 10px',
                    borderRadius: '3px',
                    cursor: 'pointer',
                    fontSize: '12px',
                  }}
                >
                  Delete
                </button>
              </div>
            )}
          </div>
        </div>
        {node.children.map((child) => renderArtifact(child, depth + 1))}
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
    </div>
  );
};

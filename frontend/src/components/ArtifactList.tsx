import React from 'react';
import { Artifact } from '../api/client';

interface ArtifactListProps {
  artifacts: Artifact[];
  selectedId?: string;
  onSelect: (id: string) => void;
  onEdit: (artifact: Artifact) => void;
  onDelete: (id: string) => void;
}

export const ArtifactList: React.FC<ArtifactListProps> = ({
  artifacts,
  selectedId,
  onSelect,
  onEdit,
  onDelete,
}) => {
  return (
    <div className="card">
      <h3>Artifacts</h3>
      {artifacts.length === 0 ? (
        <p>No artifacts yet. Create one to get started.</p>
      ) : (
        <div style={{ overflowY: 'auto', maxHeight: '500px' }}>
          {artifacts.map((artifact) => (
            <div
              key={artifact.id}
              onClick={() => onSelect(artifact.id)}
              style={{
                padding: '12px',
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
                <div>
                  <div style={{ fontWeight: 600, color: '#2c3e50' }}>
                    {artifact.title}
                  </div>
                  <div style={{ fontSize: '12px', color: '#7f8c8d', marginTop: '4px' }}>
                    {artifact.type} • v{artifact.version}
                  </div>
                </div>
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
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

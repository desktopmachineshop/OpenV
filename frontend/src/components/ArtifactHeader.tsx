import React, { useState, useEffect } from 'react';
import { Artifact, artifactAPI } from '../api/client';

interface ArtifactHeaderProps {
  artifact: Artifact;
  onEdit: (artifact: Artifact) => void;
  onDelete: (artifactId: string) => void;
  onRestore?: (artifact: Artifact) => void;
}

export const ArtifactHeader: React.FC<ArtifactHeaderProps> = ({
  artifact,
  onEdit,
  onDelete,
  onRestore,
}) => {
  const [versions, setVersions] = useState<Artifact[]>([artifact]);
  const [showVersions, setShowVersions] = useState(false);
  const [loadingVersions, setLoadingVersions] = useState(false);

  // Load versions when artifact changes
  useEffect(() => {
    const loadVersions = async () => {
      try {
        setLoadingVersions(true);
        const response = await artifactAPI.getVersions(artifact.id);
        setVersions(response.data || [artifact]);
      } catch (error) {
        console.error('Failed to load artifact versions', error);
        setVersions([artifact]);
      } finally {
        setLoadingVersions(false);
      }
    };

    loadVersions();
  }, [artifact.id]);

  const handleVersionRestore = async (version: number) => {
    if (version === artifact.version) {
      alert('This is the current version');
      return;
    }

    if (!window.confirm(`Restore version ${version}?`)) {
      return;
    }

    try {
      const response = await artifactAPI.restoreVersion(artifact.id, version);
      const restored = response.data;
      onRestore?.(restored);
      setShowVersions(false);
      // Reload versions
      const versionsResponse = await artifactAPI.getVersions(artifact.id);
      setVersions(versionsResponse.data || []);
    } catch (error) {
      console.error('Failed to restore version', error);
      alert('Failed to restore version');
    }
  };

  return (
    <div className="card" style={{ marginBottom: '20px' }}>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'flex-start',
          gap: '20px',
        }}
      >
        <div style={{ flex: 1 }}>
          <h3 style={{ margin: '0 0 8px 0' }}>{artifact.title}</h3>
          <div style={{ display: 'flex', gap: '10px', alignItems: 'center', marginBottom: '8px' }}>
            <span
              style={{
                display: 'inline-block',
                backgroundColor: '#3498db',
                color: 'white',
                padding: '4px 8px',
                borderRadius: '3px',
                fontSize: '11px',
              }}
            >
              {artifact.type}
            </span>
            <span style={{ fontSize: '12px', color: '#7f8c8d' }}>
              Version {artifact.version}
            </span>
            {versions.length > 1 && (
              <span style={{ fontSize: '12px', color: '#7f8c8d' }}>
                • {versions.length} total
              </span>
            )}
          </div>
          <p style={{ margin: 0, fontSize: '12px', color: '#7f8c8d' }}>
            UID: <code style={{ backgroundColor: '#ecf0f1', padding: '2px 6px', borderRadius: '3px' }}>
              {artifact.id}
            </code>
          </p>
        </div>

        <div style={{ display: 'flex', gap: '8px', flexDirection: 'column' }}>
          <button
            onClick={() => onEdit(artifact)}
            style={{
              backgroundColor: '#3498db',
              color: 'white',
              border: 'none',
              padding: '6px 12px',
              borderRadius: '3px',
              cursor: 'pointer',
              fontSize: '12px',
            }}
          >
            Edit
          </button>
          <button
            onClick={() => onDelete(artifact.id)}
            style={{
              backgroundColor: '#e74c3c',
              color: 'white',
              border: 'none',
              padding: '6px 12px',
              borderRadius: '3px',
              cursor: 'pointer',
              fontSize: '12px',
            }}
          >
            Delete
          </button>
          {versions.length > 1 && (
            <button
              onClick={() => setShowVersions(!showVersions)}
              style={{
                backgroundColor: '#27ae60',
                color: 'white',
                border: 'none',
                padding: '6px 12px',
                borderRadius: '3px',
                cursor: 'pointer',
                fontSize: '12px',
              }}
            >
              History
            </button>
          )}
        </div>
      </div>

      {/* Version History */}
      {showVersions && versions.length > 1 && (
        <div
          style={{
            marginTop: '16px',
            paddingTop: '16px',
            borderTop: '1px solid #ecf0f1',
          }}
        >
          <h4 style={{ margin: '0 0 12px 0', fontSize: '13px', color: '#2c3e50' }}>
            Version History
          </h4>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
            {loadingVersions ? (
              <p style={{ color: '#7f8c8d', fontSize: '12px' }}>Loading versions...</p>
            ) : (
              versions.map((v) => (
                <div
                  key={v.version}
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    padding: '8px',
                    backgroundColor: v.version === artifact.version ? '#ecf0f1' : '#f8f9fa',
                    borderRadius: '3px',
                    border: '1px solid #dcdde1',
                  }}
                >
                  <div style={{ fontSize: '12px' }}>
                    <strong>Version {v.version}</strong>
                    {v.version === artifact.version && (
                      <span
                        style={{
                          marginLeft: '6px',
                          backgroundColor: '#27ae60',
                          color: 'white',
                          padding: '2px 6px',
                          borderRadius: '3px',
                          fontSize: '10px',
                        }}
                      >
                        Current
                      </span>
                    )}
                    <div style={{ color: '#7f8c8d', fontSize: '11px', marginTop: '2px' }}>
                      Updated: {new Date(v.updated_at).toLocaleString()}
                    </div>
                  </div>
                  {v.version !== artifact.version && (
                    <button
                      onClick={() => handleVersionRestore(v.version)}
                      style={{
                        backgroundColor: '#27ae60',
                        color: 'white',
                        border: 'none',
                        padding: '4px 8px',
                        borderRadius: '3px',
                        cursor: 'pointer',
                        fontSize: '11px',
                      }}
                    >
                      Restore
                    </button>
                  )}
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default ArtifactHeader;

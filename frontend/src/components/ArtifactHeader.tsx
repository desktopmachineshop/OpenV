import React, { useState, useEffect } from 'react';
import { Artifact, artifactAPI } from '../api/client';

interface ArtifactHeaderProps {
  artifact: Artifact;
  onEdit: (artifact: Artifact) => void;
  onDelete: (artifactId: string) => void;
  onRestore?: (artifact: Artifact) => void;
  previewVersion?: Artifact | null;
  onPreviewChange?: (preview: Artifact | null) => void;
}

export const ArtifactHeader: React.FC<ArtifactHeaderProps> = ({
  artifact,
  onEdit,
  onDelete,
  onRestore,
  onPreviewChange,
  previewVersion,
}) => {
  const [versions, setVersions] = useState<Artifact[]>([artifact]);
  const [showVersions, setShowVersions] = useState(false);
  const [loadingVersions, setLoadingVersions] = useState(false);
  const [localPreviewVersion, setLocalPreviewVersion] = useState<Artifact | null>(previewVersion || null);

  // Display either preview or current artifact in header
  const displayArtifact = localPreviewVersion || artifact;

  // Update local state when prop changes
  useEffect(() => {
    setLocalPreviewVersion(previewVersion || null);
  }, [previewVersion]);

  // Notify parent when preview changes
  useEffect(() => {
    onPreviewChange?.(localPreviewVersion);
  }, [localPreviewVersion, onPreviewChange]);

  // Load versions when artifact changes
  useEffect(() => {
    const loadVersions = async () => {
      try {
        setLoadingVersions(true);
        const response = await artifactAPI.getVersions(artifact.id);
        const loadedVersions = response.data || [];
        console.log('Loaded versions:', loadedVersions);
        setVersions(loadedVersions.length > 0 ? loadedVersions : [artifact]);
      } catch (error) {
        console.error('Failed to load artifact versions', error);
        // If API fails, create a list with just the current artifact so at least History button shows
        setVersions([artifact]);
      } finally {
        setLoadingVersions(false);
      }
    };

    if (artifact.version > 1) {
      loadVersions();
    }
  }, [artifact.id, artifact.version]);

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
      setLocalPreviewVersion(null);
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
          <h3 style={{ margin: '0 0 8px 0' }}>{displayArtifact.title}</h3>
          <div style={{ display: 'flex', gap: '10px', alignItems: 'center', marginBottom: '8px' }}>
            <span
              style={{
                display: 'inline-block',
                backgroundColor: 'var(--accent)',
                color: 'white',
                padding: '4px 8px',
                borderRadius: '3px',
                fontSize: '11px',
              }}
            >
              {displayArtifact.type}
            </span>
            <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
              Version {displayArtifact.version}
            </span>
            {displayArtifact.version > 1 && (
              <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
                • {versions.length} total
              </span>
            )}
          </div>
          <p style={{ margin: 0, fontSize: '12px', color: 'var(--text-muted)' }}>
            UID: <code style={{ backgroundColor: 'var(--neutral-soft)', padding: '2px 6px', borderRadius: '3px' }}>
              {displayArtifact.id}
            </code>
          </p>
        </div>

        <div style={{ display: 'flex', gap: '8px', flexDirection: 'column' }}>
          <button
            onClick={() => onEdit(displayArtifact)}
            disabled={localPreviewVersion !== null}
            style={{
              backgroundColor: localPreviewVersion ? 'var(--neutral-mid)' : 'var(--accent)',
              color: 'white',
              border: 'none',
              padding: '6px 12px',
              borderRadius: '3px',
              cursor: localPreviewVersion ? 'not-allowed' : 'pointer',
              fontSize: '12px',
              opacity: localPreviewVersion ? 0.6 : 1,
            }}
          >
            Edit
          </button>
          <button
            onClick={() => onDelete(displayArtifact.id)}
            disabled={localPreviewVersion !== null}
            style={{
              backgroundColor: localPreviewVersion ? 'var(--neutral-mid)' : 'var(--danger)',
              color: 'white',
              border: 'none',
              padding: '6px 12px',
              borderRadius: '3px',
              cursor: localPreviewVersion ? 'not-allowed' : 'pointer',
              fontSize: '12px',
              opacity: localPreviewVersion ? 0.6 : 1,
            }}
          >
            Delete
          </button>
          {displayArtifact.version > 1 && (
            <button
              onClick={() => setShowVersions(!showVersions)}
              style={{
                backgroundColor: 'var(--success)',
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
      {showVersions && artifact.version > 1 && (
        <div
          style={{
            marginTop: '16px',
            paddingTop: '16px',
            borderTop: '1px solid var(--neutral-soft)',
          }}
        >
          <h4 style={{ margin: '0 0 12px 0', fontSize: '13px', color: 'var(--text)' }}>
            Version History
          </h4>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
            {loadingVersions ? (
              <p style={{ color: 'var(--text-muted)', fontSize: '12px' }}>Loading versions...</p>
            ) : (
              versions.map((v) => (
                <div
                  key={v.version}
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    padding: '8px',
                    backgroundColor: !v.valid_to ? 'var(--neutral-soft)' : 'var(--surface-alt)',
                    borderRadius: '3px',
                    border: '1px solid var(--border)',
                  }}
                >
                  <div style={{ fontSize: '12px' }}>
                    <strong>Version {v.version}</strong>
                    {!v.valid_to && (
                      <span
                        style={{
                          marginLeft: '6px',
                          backgroundColor: 'var(--success)',
                          color: 'white',
                          padding: '2px 6px',
                          borderRadius: '3px',
                          fontSize: '10px',
                        }}
                      >
                        Current
                      </span>
                    )}
                    <div style={{ color: 'var(--text-muted)', fontSize: '11px', marginTop: '2px' }}>
                      Updated: {new Date(v.updated_at).toLocaleString()}
                    </div>
                  </div>
                  {v.valid_to && (
                    <div style={{ display: 'flex', gap: '4px' }}>
                      <button
                        onClick={() => setLocalPreviewVersion(v)}
                        style={{
                          backgroundColor: 'var(--purple-soft)',
                          color: 'white',
                          border: 'none',
                          padding: '4px 8px',
                          borderRadius: '3px',
                          cursor: 'pointer',
                          fontSize: '11px',
                        }}
                      >
                        Preview
                      </button>
                      <button
                        onClick={() => handleVersionRestore(v.version)}
                        style={{
                          backgroundColor: 'var(--success)',
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
                    </div>
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

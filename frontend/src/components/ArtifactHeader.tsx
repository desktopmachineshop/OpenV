import React, { useState, useEffect, useRef } from 'react';
import { Artifact, ArtifactStatus, artifactAPI } from '../api/client';
import { useAlert, useConfirm } from './ui';

// Review state machine (mirrors internal/domain/artifacts/status.go):
// draft <-> in_review -> approved -> superseded. The server is authoritative;
// this map only decides which transition buttons to offer.
const STATUS_META: Record<ArtifactStatus, { label: string; color: string }> = {
  draft: { label: 'Draft', color: 'var(--neutral)' },
  in_review: { label: 'In review', color: 'var(--warning)' },
  approved: { label: 'Approved', color: 'var(--success)' },
  superseded: { label: 'Superseded', color: 'var(--purple-soft)' },
};

const NEXT_STATUSES: Record<ArtifactStatus, ArtifactStatus[]> = {
  draft: ['in_review'],
  in_review: ['draft', 'approved'],
  approved: ['superseded'],
  superseded: [],
};

const TRANSITION_LABELS: Record<ArtifactStatus, string> = {
  draft: 'Return to draft',
  in_review: 'Submit for review',
  approved: 'Approve',
  superseded: 'Mark superseded',
};

// Legacy rows predate the status column; fall back to the attribute mirror.
const statusOf = (artifact: Artifact): ArtifactStatus => {
  const raw = artifact.status || artifact.attributes?.status;
  return raw && raw in STATUS_META ? (raw as ArtifactStatus) : 'draft';
};

interface ArtifactHeaderProps {
  artifact: Artifact;
  onEdit: (artifact: Artifact) => void;
  onDelete: (artifactId: string) => void;
  onRestore?: (artifact: Artifact) => void;
  previewVersion?: Artifact | null;
  onPreviewChange?: (preview: Artifact | null) => void;
  // Optional: parents that track the artifact object can absorb the new
  // version a status change creates. Without it the header keeps its own
  // local status so the chip stays fresh either way.
  onStatusChange?: (artifact: Artifact) => void;
}

export const ArtifactHeader: React.FC<ArtifactHeaderProps> = ({
  artifact,
  onEdit,
  onDelete,
  onRestore,
  onPreviewChange,
  previewVersion,
  onStatusChange,
}) => {
  const confirm = useConfirm();
  const alertDialog = useAlert();
  const [versions, setVersions] = useState<Artifact[]>([artifact]);
  const [showVersions, setShowVersions] = useState(false);
  const [loadingVersions, setLoadingVersions] = useState(false);
  const [localPreviewVersion, setLocalPreviewVersion] = useState<Artifact | null>(previewVersion || null);
  const [status, setStatus] = useState<ArtifactStatus>(statusOf(artifact));
  const [changingStatus, setChangingStatus] = useState(false);

  // Display either preview or current artifact in header
  const displayArtifact = localPreviewVersion || artifact;
  const displayStatus = localPreviewVersion ? statusOf(localPreviewVersion) : status;

  const handleStatusChange = async (next: ArtifactStatus) => {
    if (next === 'approved' || next === 'superseded') {
      const ok = await confirm({
        title: TRANSITION_LABELS[next],
        message:
          next === 'approved'
            ? 'Approve this artifact? Editing it later will return the new version to draft.'
            : 'Mark this artifact as superseded? This is a terminal state.',
        confirmLabel: TRANSITION_LABELS[next],
      });
      if (!ok) return;
    }
    try {
      setChangingStatus(true);
      const response = await artifactAPI.changeStatus(artifact.id, next);
      setStatus(statusOf(response.data));
      onStatusChange?.(response.data);
    } catch (error: any) {
      console.error('Failed to change artifact status', error);
      await alertDialog({
        title: 'Change status',
        message: error?.response?.data?.error || 'Failed to change status.',
      });
    } finally {
      setChangingStatus(false);
    }
  };

  // Update local state when prop changes
  useEffect(() => {
    setLocalPreviewVersion(previewVersion || null);
  }, [previewVersion]);

  // Notify parent when preview changes
  useEffect(() => {
    onPreviewChange?.(localPreviewVersion);
  }, [localPreviewVersion, onPreviewChange]);

  // The effect below should only re-run when the artifact id/version change,
  // but its fallback needs the full artifact object. Read it through a ref so
  // the dep array can stay [artifact.id, artifact.version] without capturing a
  // stale object.
  const artifactRef = useRef(artifact);
  artifactRef.current = artifact;

  // Re-sync local status whenever the artifact prop changes (read through
  // the ref so the dep array can stay primitive, matching the pattern
  // above).
  useEffect(() => {
    setStatus(statusOf(artifactRef.current));
  }, [artifact.id, artifact.version, artifact.status]);

  // Load versions when artifact changes
  useEffect(() => {
    const loadVersions = async () => {
      try {
        setLoadingVersions(true);
        const response = await artifactAPI.getVersions(artifact.id);
        const loadedVersions = response.data || [];
        setVersions(loadedVersions.length > 0 ? loadedVersions : [artifactRef.current]);
      } catch (error) {
        console.error('Failed to load artifact versions', error);
        // If API fails, create a list with just the current artifact so at least History button shows
        setVersions([artifactRef.current]);
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
      await alertDialog({ title: 'Restore version', message: 'This is the current version.' });
      return;
    }

    const ok = await confirm({
      title: 'Restore version',
      message: `Restore version ${version}? The current content is preserved in the history.`,
      confirmLabel: 'Restore',
    });
    if (!ok) {
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
      await alertDialog({ title: 'Restore version', message: 'Failed to restore version.' });
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
                color: 'var(--accent-fg)',
                padding: '4px 8px',
                borderRadius: '3px',
                fontSize: '11px',
              }}
            >
              {displayArtifact.type}
            </span>
            <span
              title={`Review status: ${STATUS_META[displayStatus].label}`}
              style={{
                display: 'inline-block',
                backgroundColor: STATUS_META[displayStatus].color,
                color: 'white',
                padding: '4px 8px',
                borderRadius: '3px',
                fontSize: '11px',
              }}
            >
              {STATUS_META[displayStatus].label}
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
          {!localPreviewVersion && NEXT_STATUSES[status].length > 0 && (
            <div style={{ display: 'flex', gap: '6px', marginBottom: '8px' }}>
              {NEXT_STATUSES[status].map((next) => (
                <button
                  key={next}
                  onClick={() => handleStatusChange(next)}
                  disabled={changingStatus}
                  style={{
                    backgroundColor: 'transparent',
                    color: STATUS_META[next].color,
                    border: `1px solid ${STATUS_META[next].color}`,
                    padding: '3px 10px',
                    borderRadius: '3px',
                    cursor: changingStatus ? 'wait' : 'pointer',
                    fontSize: '11px',
                    opacity: changingStatus ? 0.6 : 1,
                  }}
                >
                  {TRANSITION_LABELS[next]}
                </button>
              ))}
            </div>
          )}
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
              color: 'var(--accent-fg)',
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
                    <span
                      style={{
                        marginLeft: '6px',
                        color: STATUS_META[statusOf(v)].color,
                        fontSize: '10px',
                        fontWeight: 600,
                      }}
                    >
                      {STATUS_META[statusOf(v)].label}
                    </span>
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

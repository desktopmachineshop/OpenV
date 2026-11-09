import React, { useState, useEffect, useRef } from 'react';
import { Artifact, ArtifactStatus, artifactAPI } from '../api/client';
import { Chip, buttonStyle, useAlert, useConfirm } from './ui';
import { useViewport } from '../hooks/useViewport';
import { useReleaseRangeLabel } from './ArtifactReleaseFact';
import { ArtifactStepper, ArtifactStepperProps } from './ArtifactStepper';
import { ArtifactVersionList } from './ArtifactVersionList';
import { MONO, QualityChip } from './artifactCells';
import { NEXT_STATUSES, TRANSITION_LABELS, isForward, useStatusChange } from './artifactStatusChange';
import { QualityFacts, STATUS_META, ago, priorityOf, statusOf, typeLabel, verificationMethodOf } from '../utils/artifactFacts';

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
  /**
   * Stepping to the artifact before or after this one in the document. Absent
   * when there is nothing to step through — one artifact in view, or a
   * workspace that has not received the controls yet.
   */
  nav?: ArtifactStepperProps;
  /**
   * The artifact as a baseline captured it. Its id is the live artifact's,
   * so every action here (Edit, Delete, a status change, History with its
   * Restore) would act on the live one; read-only shows none of them, and
   * neither loads nor counts the live artifact's versions.
   */
  readOnly?: boolean;
  /**
   * When the artifact was deleted, for one opened from its history (a link
   * to it, an Activity line). Its versions are kept (REQ-4), so History
   * still lists and previews them; nothing here can change it.
   */
  deletedAt?: string | null;
  /** The requirement's quality score, shown as a chip beside its status. */
  quality?: QualityFacts;
  /** A phone moves the review transitions into its bottom action bar. */
  hideTransitions?: boolean;
}

export const ArtifactHeader: React.FC<ArtifactHeaderProps> = ({
  artifact,
  onEdit,
  onDelete,
  onRestore,
  onPreviewChange,
  previewVersion,
  onStatusChange,
  nav,
  readOnly = false,
  deletedAt = null,
  quality,
  hideTransitions = false,
}) => {
  const confirm = useConfirm();
  const alertDialog = useAlert();
  const [versions, setVersions] = useState<Artifact[]>([artifact]);
  // On a phone the Edit / Delete / History column moves under the title as
  // a row of full-width buttons instead of squeezing the title.
  const { isPhone, coarsePointer: touch } = useViewport();
  const [showVersions, setShowVersions] = useState(false);
  const [loadingVersions, setLoadingVersions] = useState(false);
  const [localPreviewVersion, setLocalPreviewVersion] = useState<Artifact | null>(previewVersion || null);
  const [status, setStatus] = useState<ArtifactStatus>(statusOf(artifact));

  // Display either preview or current artifact in header
  const displayArtifact = localPreviewVersion || artifact;
  const displayStatus = localPreviewVersion ? statusOf(localPreviewVersion) : status;

  const { change: handleStatusChange, changing: changingStatus } = useStatusChange(artifact.id, (changed) => {
    setStatus(statusOf(changed));
    onStatusChange?.(changed);
  });

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

    // A baseline's artifact (read-only) is the one version the baseline
    // holds: the live artifact's history is not its to count or list.
    if (!readOnly && artifact.version > 1) {
      loadVersions();
    }
  }, [artifact.id, artifact.version, readOnly]);

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

  const meta = STATUS_META[displayStatus];
  const previewing = localPreviewVersion !== null;
  const owner = typeof displayArtifact.attributes?.owner === 'string' ? displayArtifact.attributes.owner : '';
  const priority = priorityOf(displayArtifact);
  const method = verificationMethodOf(displayArtifact);
  const releases = useReleaseRangeLabel(displayArtifact);
  const action = (kind: 'secondary' | 'danger', disabled = false): React.CSSProperties => ({
    ...buttonStyle(kind, { compact: !touch, disabled }),
    ...(touch ? { minHeight: 40 } : {}),
    flex: isPhone ? '1 1 auto' : undefined,
  });
  const facts: Array<[string, React.ReactNode]> = [
    ['Type', typeLabel(displayArtifact.type)],
    ...(priority ? [['Priority', priority] as [string, React.ReactNode]] : []),
    ...(owner ? [['Owner', owner] as [string, React.ReactNode]] : []),
    ...(method ? [['Verify by', method] as [string, React.ReactNode]] : []),
    ...(releases ? [['Releases', releases] as [string, React.ReactNode]] : []),
    [
      'Version',
      `${displayArtifact.version}${!readOnly && displayArtifact.version > 1 ? ` · ${versions.length} total` : ''}`,
    ],
    ['Updated', /^\d+[mhdw]$/.test(ago(displayArtifact.updated_at)) ? `${ago(displayArtifact.updated_at)} ago` : ago(displayArtifact.updated_at)],
  ];

  return (
    <div className="card" style={{ marginBottom: 12, padding: '12px 14px' }}>
      {/* Above everything, in its own row, so it neither fights the title for
          width on a desktop nor lands under the buttons on a phone. */}
      {nav && <ArtifactStepper {...nav} touch={touch} showKeyHints={!touch} />}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
        {/* The stable reference leads: it is the address a reader quotes in a
            review, a test result or a report, and it never changes. */}
        {displayArtifact.ref && (
          <code
            style={{ fontFamily: MONO, fontSize: 12, fontWeight: 600, color: 'var(--accent-text)', background: 'none', padding: 0 }}
            title="Stable reference — unique in this project and never reissued"
          >
            {displayArtifact.ref}
          </code>
        )}
        <span title={`Review status: ${meta.label}`} style={{ display: 'inline-flex' }}>
          <Chip tone={meta.tone}>{meta.label.toLowerCase()}</Chip>
        </span>
        {deletedAt && <Chip tone="danger">deleted</Chip>}
        <QualityChip quality={quality} withBand={isPhone} />
        {priority && <Chip tone="neutral">{priority}</Chip>}
        <span style={{ flex: 1 }} />
        {!readOnly && (
          <span style={{ display: 'flex', gap: 6, flexWrap: 'wrap', width: isPhone ? '100%' : undefined }}>
            {!deletedAt && (
              <button
                type="button"
                data-help-key="requirements.versions"
                onClick={() => onEdit(displayArtifact)}
                disabled={previewing}
                style={action('secondary', previewing)}
              >
                Edit
              </button>
            )}
            {displayArtifact.version > 1 && (
              <button
                type="button"
                data-help-key="requirements.versions"
                aria-expanded={showVersions}
                onClick={() => setShowVersions(!showVersions)}
                style={action('secondary')}
              >
                History
              </button>
            )}
            {!deletedAt && (
              <button
                type="button"
                onClick={() => onDelete(displayArtifact.id)}
                disabled={previewing}
                style={action('danger', previewing)}
              >
                Delete
              </button>
            )}
          </span>
        )}
      </div>
      <h3 style={{ margin: '10px 0 8px', fontSize: isPhone ? 20 : 17, lineHeight: 1.3 }}>{displayArtifact.title}</h3>
      {deletedAt && (
        <p role="note" style={{ margin: '0 0 8px', fontSize: 12, color: 'var(--danger-strong)' }}>
          Deleted {new Date(deletedAt).toLocaleString()}. You are reading its history: History lists every version.
        </p>
      )}
      <dl
        style={{
          margin: 0,
          display: 'grid',
          gridTemplateColumns: isPhone ? '84px minmax(0, 1fr)' : '84px minmax(0, 1fr) 72px minmax(0, 1fr)',
          gap: '4px 8px',
          fontSize: 12,
        }}
      >
        {facts.map(([label, value]) => (
          <React.Fragment key={label}>
            <dt style={{ color: 'var(--text-muted)' }}>{label}</dt>
            <dd style={{ margin: 0, color: 'var(--text)', overflowWrap: 'anywhere' }}>{value}</dd>
          </React.Fragment>
        ))}
        <dt style={{ color: 'var(--text-muted)', gridColumn: 1 }}>UID</dt>
        <dd style={{ margin: 0, gridColumn: isPhone ? undefined : 'span 3' }}>
          <code style={{ fontSize: 12, color: 'var(--text-secondary)', wordBreak: 'break-all' }}>{displayArtifact.id}</code>
        </dd>
      </dl>
      {!readOnly && !deletedAt && !hideTransitions && !localPreviewVersion && NEXT_STATUSES[status].length > 0 && (
        <div style={{ display: 'flex', gap: 6, marginTop: 10, flexWrap: 'wrap' }} data-help-key="review">
          {NEXT_STATUSES[status].map((next) => (
            <button
              key={next}
              type="button"
              onClick={() => handleStatusChange(next)}
              disabled={changingStatus}
              style={{
                ...buttonStyle(isForward(next) ? 'primary' : 'secondary', { compact: !touch }),
                ...(touch ? { minHeight: 40 } : {}),
                cursor: changingStatus ? 'wait' : 'pointer',
                opacity: changingStatus ? 0.6 : 1,
              }}
            >
              {TRANSITION_LABELS[next]}
            </button>
          ))}
        </div>
      )}

      {!readOnly && showVersions && artifact.version > 1 && (
        <ArtifactVersionList
          versions={versions}
          loading={loadingVersions}
          onPreview={(v) => setLocalPreviewVersion(v)}
          onRestore={deletedAt ? undefined : handleVersionRestore}
        />
      )}
    </div>
  );
};

export default ArtifactHeader;

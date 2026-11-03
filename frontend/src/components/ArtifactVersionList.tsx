import React from 'react';
import type { Artifact } from '../api/client';
import { Chip } from './ui/Chip';
import { buttonStyle } from './ui/buttonStyles';
import { STATUS_META, statusOf } from '../utils/artifactFacts';

interface ArtifactVersionListProps {
  versions: Artifact[];
  loading: boolean;
  onPreview: (version: Artifact) => void;
  onRestore: (version: number) => void;
}

// The artifact's versions, newest as the server lists them: each with its
// review status and when it changed, and for a past version Preview (side by
// side with the current one) and Restore.
export const ArtifactVersionList: React.FC<ArtifactVersionListProps> = ({ versions, loading, onPreview, onRestore }) => (
  <div data-help-key="requirements.versions" style={{ marginTop: 14, paddingTop: 12, borderTop: '1px solid var(--border-soft)' }}>
    <h4 style={{ margin: '0 0 10px 0', fontSize: 13, color: 'var(--text)' }}>Version History</h4>
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
      {loading ? (
        <p style={{ color: 'var(--text-muted)', fontSize: 12 }}>Loading versions...</p>
      ) : (
        versions.map((v) => {
          const meta = STATUS_META[statusOf(v)];
          return (
            <div
              key={v.version}
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                gap: 8,
                padding: '8px 10px',
                backgroundColor: !v.valid_to ? 'var(--surface-alt)' : 'var(--surface)',
                borderRadius: 6,
                border: '1px solid var(--border-soft)',
              }}
            >
              <div style={{ fontSize: 12, display: 'flex', flexDirection: 'column', gap: 2 }}>
                <span style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}>
                  <strong>Version {v.version}</strong>
                  <Chip tone={meta.tone}>{meta.label}</Chip>
                  {!v.valid_to && <Chip tone="success">Current</Chip>}
                </span>
                <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>Updated: {new Date(v.updated_at).toLocaleString()}</span>
              </div>
              {v.valid_to && (
                <div style={{ display: 'flex', gap: 4, flex: 'none' }}>
                  <button type="button" onClick={() => onPreview(v)} style={buttonStyle('secondary', { compact: true })}>
                    Preview
                  </button>
                  <button type="button" onClick={() => onRestore(v.version)} style={buttonStyle('secondary', { compact: true })}>
                    Restore
                  </button>
                </div>
              )}
            </div>
          );
        })
      )}
    </div>
  </div>
);

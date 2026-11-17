import React, { useState } from 'react';
import type { Artifact } from '../api/client';
import { useReleaseRangeLabel } from './ArtifactReleaseFact';
import { ago, priorityOf, typeLabel, verificationMethodOf } from '../utils/artifactFacts';

interface ArtifactHeaderFactsProps {
  artifact: Artifact;
  /** How many versions the artifact has, when its history was loaded. */
  versionCount: number;
  readOnly: boolean;
  isPhone: boolean;
  /**
   * One line under the artifact's text, with the full table behind Details.
   * Without it the table is always shown, as the header drew it before.
   */
  folded?: boolean;
}

const updatedLabel = (artifact: Artifact): string => {
  const when = ago(artifact.updated_at);
  return /^\d+[mhdw]$/.test(when) ? `${when} ago` : when;
};

// The facts of an artifact under its title: its type, priority, owner, how it
// is verified, its releases, version and when it last changed, then its UID.
export const ArtifactHeaderFacts: React.FC<ArtifactHeaderFactsProps> = ({ artifact, versionCount, readOnly, isPhone, folded = false }) => {
  const [open, setOpen] = useState(false);
  const owner = typeof artifact.attributes?.owner === 'string' ? artifact.attributes.owner : '';
  const priority = priorityOf(artifact);
  const method = verificationMethodOf(artifact);
  const releases = useReleaseRangeLabel(artifact);
  const facts: Array<[string, React.ReactNode]> = [
    ['Type', typeLabel(artifact.type)],
    ...(priority ? [['Priority', priority] as [string, React.ReactNode]] : []),
    ...(owner ? [['Owner', owner] as [string, React.ReactNode]] : []),
    ...(method ? [['Verify by', method] as [string, React.ReactNode]] : []),
    ...(releases ? [['Releases', releases] as [string, React.ReactNode]] : []),
    ['Version', `${artifact.version}${!readOnly && artifact.version > 1 ? ` · ${versionCount} total` : ''}`],
    ['Updated', updatedLabel(artifact)],
  ];
  const table = (
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
        <code style={{ fontSize: 12, color: 'var(--text-secondary)', wordBreak: 'break-all' }}>{artifact.id}</code>
      </dd>
    </dl>
  );
  if (!folded) return table;
  const line = [
    typeLabel(artifact.type),
    owner,
    method ? `verify by ${method}` : '',
    releases,
    `version ${artifact.version}`,
    `updated ${updatedLabel(artifact)}`,
  ].filter(Boolean);
  return (
    <div style={{ fontSize: 12 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, color: 'var(--text-muted)' }}>
        <span style={{ flex: 1, minWidth: 0 }}>{line.join(' · ')}</span>
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((shown) => !shown)}
          className="compact-action"
          style={{ border: 'none', background: 'none', padding: 0, color: 'var(--accent-text)', font: 'inherit', fontWeight: 600, cursor: 'pointer', flex: 'none' }}
        >
          Details
        </button>
      </div>
      {open && <div style={{ marginTop: 8 }}>{table}</div>}
    </div>
  );
};

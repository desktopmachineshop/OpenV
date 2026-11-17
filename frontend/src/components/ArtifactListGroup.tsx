import React, { useState } from 'react';
import type { Artifact } from '../api/client';

/** One group of the chip search bar's group-by (issue #722): a heading and its rows. */
export interface ArtifactRowGroup {
  key: string;
  label: string;
  rows: Artifact[];
}

// A group's heading inside the requirements table, which folds the rows
// under it; the rows keep their own hierarchy within the group.
export const ArtifactListGroup: React.FC<{ label: string; count: number; compact: boolean; children: React.ReactNode }> = ({
  label,
  count,
  compact,
  children,
}) => {
  const [closed, setClosed] = useState(false);
  return (
    <div role="group" aria-label={label}>
      <button
        type="button"
        aria-expanded={!closed}
        onClick={() => setClosed((c) => !c)}
        className="ov-row-group"
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          width: '100%',
          minHeight: compact ? 36 : 28,
          padding: '0 12px',
          border: 'none',
          borderBottom: '1px solid var(--border-soft)',
          background: 'var(--surface-alt)',
          color: 'var(--text-secondary)',
          fontSize: 12,
          fontWeight: 600,
          textAlign: 'left',
          cursor: 'pointer',
        }}
      >
        <svg width="12" height="12" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={{ transform: closed ? 'rotate(-90deg)' : undefined, flex: 'none' }}>
          <path d="M6 8l4 4 4-4" />
        </svg>
        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{label}</span>
        <span style={{ color: 'var(--text-muted)', fontWeight: 500 }}>{count}</span>
      </button>
      {!closed && children}
    </div>
  );
};

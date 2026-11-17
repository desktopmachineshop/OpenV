import React from 'react';
import { QualityFacts, RowFacts, ago, qualityTone } from '../../utils/artifactFacts';
import { toneColors } from '../../components/ui/Chip';
import { RefText, StatusChip, VVDot } from '../../components/artifactCells';
import type { Artifact } from './shared';

interface PhoneListRowProps {
  artifact: Artifact;
  facts: RowFacts;
  quality?: QualityFacts;
  selected: boolean;
  onSelect: () => void;
  onMenu?: (event: React.MouseEvent) => void;
}

// A phone's row: the reference, status and age on the first line, the title
// on the second, and quality, trace and verification as one quiet line under
// it. The whole row opens the artifact; ⋯ opens its actions.
export const PhoneListRow: React.FC<PhoneListRowProps> = ({ artifact, facts, quality, selected, onSelect, onMenu }) => (
  <div
    role="row"
    aria-selected={selected}
    tabIndex={0}
    className="ov-row"
    onClick={onSelect}
    onKeyDown={(e) => {
      if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) {
        e.preventDefault();
        onSelect();
      }
    }}
    style={{
      display: 'flex',
      alignItems: 'center',
      gap: 4,
      padding: '10px 4px 10px 16px',
      borderBottom: '1px solid var(--border-soft)',
      background: selected ? 'var(--tint-blue)' : 'var(--surface)',
      cursor: 'pointer',
    }}
  >
    <div role="cell" style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 3 }}>
      <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <RefText artifact={artifact} strong />
        <StatusChip artifact={artifact} />
        <span style={{ flex: 1 }} />
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{ago(artifact.updated_at)}</span>
      </span>
      <span style={{ fontSize: 15, fontWeight: 500, color: 'var(--text)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        {artifact.title}
      </span>
      <span style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 12, color: 'var(--text-muted)', flexWrap: 'wrap' }}>
        {quality && (
          <span style={{ fontWeight: 700, color: toneColors(qualityTone(quality.band)).fg }}>Q {quality.score}</span>
        )}
        <span>
          ↑{facts.up} ↓{facts.down}
        </span>
        {facts.vv.status !== 'none' && (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
            <VVDot status={facts.vv.status} size={7} />
            {facts.vv.label}
            {facts.vv.count ? ` ${facts.vv.count}` : ''}
          </span>
        )}
        {facts.suspect > 0 && <span style={{ color: 'var(--warning-text)', fontWeight: 600 }}>suspect link</span>}
      </span>
    </div>
    {onMenu && (
      <button
        type="button"
        aria-label={`Actions for ${artifact.title}`}
        onClick={onMenu}
        style={{ flex: 'none', width: 44, height: 44, padding: 0, border: 'none', borderRadius: 6, background: 'transparent', color: 'var(--text-muted)', fontSize: 20, cursor: 'pointer' }}
      >
        ⋯
      </button>
    )}
  </div>
);

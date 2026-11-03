import React from 'react';
import type { Artifact } from '../api/client';
import { Chip } from './ui/Chip';
import {
  QualityFacts,
  RowFacts,
  STATUS_META,
  VVFacts,
  qualityTone,
  statusOf,
  vvColor,
} from '../utils/artifactFacts';

// The cells every requirements view draws the same way: the table, the tree,
// the layers, the phone list and the detail panel. A status reads as a word
// on a pale tint (Chip), a quality score as "Q 74" coloured by its band,
// trace as ↑ up ↓ down with a warning sign when a link is suspect, and
// verification as a coloured dot beside its word.

export const MONO = "source-code-pro, Menlo, Monaco, Consolas, 'Courier New', monospace";

/** An artifact's stable reference, in the monospace blue a citation reads in. */
export const RefText: React.FC<{ artifact: Pick<Artifact, 'ref' | 'doc_number'>; strong?: boolean; style?: React.CSSProperties }> = ({
  artifact,
  strong,
  style,
}) => (
  <span
    title="Stable reference — cite this; it never changes or gets reused"
    style={{ fontFamily: MONO, fontSize: 12, color: 'var(--accent-text)', fontWeight: strong ? 600 : 400, whiteSpace: 'nowrap', ...style }}
  >
    {artifact.ref || artifact.doc_number || ''}
  </span>
);

// Rows leave the chip untitled: "Review status: …" names the one chip in the
// detail panel's header, which is what a reader (and the e2e pack) looks for.
export const StatusChip: React.FC<{ artifact: Artifact; size?: 'sm' | 'md' }> = ({ artifact, size }) => {
  const meta = STATUS_META[statusOf(artifact)];
  return (
    <Chip tone={meta.tone} size={size}>
      {meta.label.toLowerCase()}
    </Chip>
  );
};

export const QualityChip: React.FC<{ quality?: QualityFacts; size?: 'sm' | 'md'; withBand?: boolean }> = ({
  quality,
  size,
  withBand,
}) => {
  if (!quality) return null;
  const band = quality.band.charAt(0).toUpperCase() + quality.band.slice(1);
  const tip =
    `Requirement quality: ${quality.score}/100 (${band})` +
    ` · ${quality.findingCount} finding${quality.findingCount === 1 ? '' : 's'}`;
  return (
    <Chip tone={qualityTone(quality.band)} size={size} title={tip}>
      Q {quality.score}
      {withBand ? ` · ${band}` : ''}
    </Chip>
  );
};

export const SuspectMark: React.FC<{ count?: number }> = ({ count = 1 }) => (
  <span
    role="img"
    aria-label={count === 1 ? 'Suspect link' : `${count} suspect links`}
    title={count === 1 ? 'A suspect link: the other end changed since it was linked' : `${count} suspect links`}
    style={{ color: 'var(--warning-strong)', display: 'inline-flex' }}
  >
    <svg width="13" height="13" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M10 3l8 14H2z" />
      <path d="M10 8v4M10 14.5v.5" />
    </svg>
  </span>
);

export const TraceCell: React.FC<{ facts: RowFacts }> = ({ facts }) => (
  <span
    style={{ fontSize: 12, color: 'var(--text-secondary)', display: 'inline-flex', alignItems: 'center', gap: 4, whiteSpace: 'nowrap' }}
    title={`${facts.up} up, ${facts.down} down`}
  >
    ↑{facts.up} ↓{facts.down}
    {facts.suspect > 0 && <SuspectMark count={facts.suspect} />}
  </span>
);

export const VVDot: React.FC<{ status: string; size?: number }> = ({ status, size = 8 }) => (
  <span
    aria-hidden="true"
    style={{ flex: 'none', width: size, height: size, borderRadius: '50%', background: vvColor(status), display: 'inline-block' }}
  />
);

export const VVCell: React.FC<{ vv: VVFacts }> = ({ vv }) =>
  vv.status === 'none' ? (
    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>—</span>
  ) : (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-secondary)', whiteSpace: 'nowrap', minWidth: 0 }}>
      <VVDot status={vv.status} />
      <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{vv.label}</span>
      {vv.count && <span style={{ color: 'var(--text-muted)' }}>{vv.count}</span>}
    </span>
  );

/** A thin pass / fail / blocked bar, as the verification sections draw it. */
export const VVBar: React.FC<{ pass: number; fail: number; blocked: number; total: number; height?: number; width?: number | string }> = ({
  pass,
  fail,
  blocked,
  total,
  height = 4,
  width = '100%',
}) => {
  const pct = (n: number) => (total ? `${(n / total) * 100}%` : '0%');
  return (
    <span
      aria-hidden="true"
      style={{ display: 'flex', width, height, borderRadius: 999, overflow: 'hidden', background: 'var(--neutral-soft)', flex: 'none' }}
    >
      <span style={{ width: pct(pass), background: 'var(--success)' }} />
      <span style={{ width: pct(fail), background: 'var(--danger)' }} />
      <span style={{ width: pct(blocked), background: 'var(--warning)' }} />
    </span>
  );
};

/** The small uppercase heading of a section in the detail panel and the cards. */
export const sectionHeadingStyle: React.CSSProperties = {
  margin: '0 0 2px',
  fontSize: 12,
  fontWeight: 600,
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
  color: 'var(--text-muted)',
};

/** A type tag, as the layer and level views mark a card's kind. */
export const TypeTag: React.FC<{ tag: string; strong?: boolean }> = ({ tag, strong }) => (
  <span
    style={{
      fontSize: 12,
      fontWeight: 700,
      borderRadius: 3,
      padding: '0 4px',
      lineHeight: 1.6,
      whiteSpace: 'nowrap',
      background: strong ? 'var(--text-secondary)' : 'var(--surface)',
      color: strong ? 'var(--surface)' : 'var(--text-secondary)',
      border: strong ? '1px solid var(--text-secondary)' : '1px solid var(--neutral-mid)',
    }}
  >
    {tag}
  </span>
);

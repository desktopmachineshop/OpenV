// The small pieces the V&V pages share: the coverage page (VVDashboard),
// its sections and run cards, and a test run (TestRunView). A stacked bar
// of results, a compact stat card, a legend, and the fill colour of a
// rollup or a result. Fills are for bars and swatches only: a status as a
// word is a Chip (components/ui), never a solid pill.
import React from 'react';
import type { TestResult } from '../api/client';

/** The fill of a bar segment or swatch for a rollup or a result. */
export const rollupFill = (status: string): string => {
  switch ((status || '').toLowerCase()) {
    case 'pass':
    case 'verified-manually':
      return 'var(--success)';
    case 'fail':
      return 'var(--danger)';
    case 'blocked':
      return 'var(--warning)';
    case 'unrun':
    case 'not-run':
      return 'var(--neutral)';
    default:
      // uncovered / method-missing: the empty track shows through.
      return 'var(--neutral-soft)';
  }
};

export const FILL = {
  pass: 'var(--success)',
  fail: 'var(--danger)',
  blocked: 'var(--warning)',
  notRun: 'var(--neutral)',
  track: 'var(--neutral-soft)',
  accent: 'var(--accent)',
};

/** The card every block of the V&V pages sits on. */
export const cardStyle: React.CSSProperties = {
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 6,
};

/** A card's title row: a 40 px band with a hairline under it. */
export const cardHeadStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  gap: 8,
  flexWrap: 'wrap',
  minHeight: 40,
  padding: '4px 12px',
  borderBottom: '1px solid var(--border-soft)',
  boxSizing: 'border-box',
};

export const cardTitleStyle: React.CSSProperties = { margin: 0, fontSize: 14, fontWeight: 600, color: 'var(--text)' };

/** A citable reference (REQ-12, TC-3) set in the code face. */
export const refStyle: React.CSSProperties = {
  fontFamily: 'source-code-pro, Menlo, Monaco, Consolas, monospace',
  fontSize: 12,
  color: 'var(--accent-text)',
  whiteSpace: 'nowrap',
};

/** The uppercase header band of a dense table. */
export const tableHeadStyle: React.CSSProperties = {
  background: 'var(--surface-inset)',
  color: 'var(--text-muted)',
  fontSize: 12,
  fontWeight: 600,
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
};

export interface BarSegment {
  value: number;
  color: string;
  label: string;
}

/** A stacked bar; what is not covered by a segment is the grey track. */
export const StackedBar: React.FC<{
  segments: BarSegment[];
  total: number;
  height?: number;
  radius?: number;
  track?: string;
}> = ({ segments, total, height = 10, radius = 3, track = FILL.track }) => (
  <span
    role="img"
    aria-label={segments.map((s) => `${s.label}: ${s.value}`).join(', ') + ` of ${total}`}
    style={{
      display: 'flex',
      height,
      borderRadius: radius,
      overflow: 'hidden',
      background: track,
      minWidth: 0,
    }}
  >
    {total > 0 &&
      segments.map((s) =>
        s.value > 0 ? (
          <span
            key={s.label}
            title={`${s.label}: ${s.value}`}
            style={{ width: `${(Math.min(s.value, total) / total) * 100}%`, background: s.color }}
          />
        ) : null
      )}
  </span>
);

/** A compact figure with a mini bar under it. */
export const StatCard: React.FC<{
  label: string;
  value: React.ReactNode;
  sub?: React.ReactNode;
  /** 0..1, the share the mini bar fills; omitted, no bar. */
  fill?: number;
  barColor?: string;
  valueColor?: string;
  helpKey?: string;
}> = ({ label, value, sub, fill, barColor = FILL.accent, valueColor, helpKey }) => (
  <div
    data-help-key={helpKey}
    style={{ ...cardStyle, padding: '10px 12px', display: 'flex', flexDirection: 'column', gap: 6, minWidth: 0 }}
  >
    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{label}</span>
    <span style={{ display: 'flex', alignItems: 'baseline', gap: 6, flexWrap: 'wrap' }}>
      <b style={{ fontSize: 22, fontWeight: 700, lineHeight: 1.2, color: valueColor || 'var(--text)' }}>{value}</b>
      {sub && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{sub}</span>}
    </span>
    {fill !== undefined && (
      <span style={{ display: 'block', height: 4, borderRadius: 999, background: FILL.track, overflow: 'hidden' }}>
        <span
          style={{
            display: 'block',
            width: `${Math.max(0, Math.min(1, fill)) * 100}%`,
            height: '100%',
            background: barColor,
          }}
        />
      </span>
    )}
  </div>
);

export interface LegendItem {
  label: string;
  color: string;
  count?: number;
  /** The empty track: a hollow swatch. */
  hollow?: boolean;
}

export const Legend: React.FC<{ items: LegendItem[] }> = ({ items }) => (
  <span style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 12px', fontSize: 12, color: 'var(--text-secondary)' }}>
    {items.map((it) => (
      <span key={it.label} style={{ display: 'inline-flex', alignItems: 'center', gap: 4, whiteSpace: 'nowrap' }}>
        <span
          aria-hidden="true"
          style={{
            width: 8,
            height: 8,
            borderRadius: 2,
            background: it.color,
            boxSizing: 'border-box',
            border: it.hollow ? '1px solid var(--neutral-mid)' : undefined,
          }}
        />
        {it.label}
        {it.count !== undefined && <b style={{ fontWeight: 600 }}>{it.count}</b>}
      </span>
    ))}
  </span>
);

export interface ResultCounts {
  pass: number;
  fail: number;
  blocked: number;
  /** Results with an outcome: pass, fail or blocked (not-run is no outcome). */
  recorded: number;
}

/** Counts a run's results by outcome. */
export const countResults = (results: { status: TestResult['status'] | string }[]): ResultCounts => {
  const c: ResultCounts = { pass: 0, fail: 0, blocked: 0, recorded: 0 };
  results.forEach((r) => {
    if (r.status === 'pass') c.pass++;
    else if (r.status === 'fail') c.fail++;
    else if (r.status === 'blocked') c.blocked++;
  });
  c.recorded = c.pass + c.fail + c.blocked;
  return c;
};

/** The pass/fail/blocked segments of a run's bar. */
export const resultSegments = (c: ResultCounts): BarSegment[] => [
  { value: c.pass, color: FILL.pass, label: 'pass' },
  { value: c.fail, color: FILL.fail, label: 'fail' },
  { value: c.blocked, color: FILL.blocked, label: 'blocked' },
];

/** "3 pass · 1 fail · 0 blocked · 4 to go" */
export const resultSummary = (c: ResultCounts, total: number): string => {
  const togo = Math.max(0, total - c.recorded);
  return `${c.pass} pass · ${c.fail} fail · ${c.blocked} blocked${total > 0 ? ` · ${togo} to go` : ''}`;
};

import React from 'react';
import { UserDashboardPoint } from '../../api/client';

// The pieces the Platform admin page's dashboards share: a stat tile, a
// daily bar series, table cell styles, and percent and country formatting.

export const percent = (v: number) => `${Math.round(v * 100)}%`;

let regionNames: Intl.DisplayNames | null = null;
export const countryName = (code: string): string => {
  try {
    regionNames ??= new Intl.DisplayNames(undefined, { type: 'region' });
    return regionNames.of(code) || code;
  } catch {
    return code;
  }
};

export const Stat: React.FC<{ label: string; value: React.ReactNode; hint?: string }> = ({ label, value, hint }) => (
  <div className="card" style={{ padding: '12px 14px', minWidth: 0 }}>
    <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>{label}</div>
    <div style={{ fontSize: 24, fontWeight: 600, color: 'var(--text)', fontVariantNumeric: 'tabular-nums' }}>{value}</div>
    {hint && <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>{hint}</div>}
  </div>
);

// DailyBars is one series of daily counts: a bar per day, each with a
// hover title, and the busiest day named under it.
export const DailyBars: React.FC<{ title: string; points: UserDashboardPoint[] }> = ({ title, points }) => {
  const max = Math.max(1, ...points.map((p) => p.count));
  const width = 600;
  const height = 96;
  const step = width / Math.max(1, points.length);
  const total = points.reduce((sum, p) => sum + p.count, 0);
  return (
    <figure style={{ margin: 0 }}>
      <figcaption style={{ fontSize: 13, fontWeight: 600, marginBottom: 6 }}>
        {title} <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>· last {points.length} days</span>
      </figcaption>
      <svg viewBox={`0 0 ${width} ${height}`} width="100%" height={height} preserveAspectRatio="none" role="img"
        aria-label={`${title}: ${total} in the last ${points.length} days, at most ${max} in one day`}>
        <line x1={0} x2={width} y1={height - 0.5} y2={height - 0.5} stroke="var(--border)" />
        {points.map((p, i) => {
          const h = (p.count / max) * (height - 4);
          return (
            <g key={p.day}>
              <rect x={i * step} y={0} width={step} height={height} fill="transparent">
                <title>{`${p.day}: ${p.count}`}</title>
              </rect>
              {p.count > 0 && (
                <rect x={i * step + 1} y={height - 1 - h} width={Math.max(1, step - 2)} height={h} rx={1}
                  fill="var(--accent)" pointerEvents="none" />
              )}
            </g>
          );
        })}
      </svg>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-muted)' }}>
        <span>{points[0]?.day}</span>
        <span>peak {max}</span>
        <span>{points[points.length - 1]?.day}</span>
      </div>
    </figure>
  );
};

export const th: React.CSSProperties = {
  textAlign: 'left',
  fontSize: 12,
  color: 'var(--text-muted)',
  fontWeight: 600,
  padding: '6px 8px',
  borderBottom: '1px solid var(--border)',
};
export const td: React.CSSProperties = {
  padding: '6px 8px',
  fontSize: 13,
  borderBottom: '1px solid var(--border-soft)',
  fontVariantNumeric: 'tabular-nums',
};

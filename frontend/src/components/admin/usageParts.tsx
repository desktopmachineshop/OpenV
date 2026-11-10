import React from 'react';
import type { UsagePace } from '../../api/client';

// The usage view's small pieces: a weekly sparkline, a pace badge and a
// "last seen" date.

/** One bar per week, oldest first; the pace window is drawn darker. */
export const Sparkline: React.FC<{ weeks: number[]; paceWeeks: number; label: string }> = ({ weeks, paceWeeks, label }) => {
  const max = Math.max(1, ...weeks);
  const w = 84;
  const h = 22;
  const step = w / Math.max(1, weeks.length);
  const total = weeks.reduce((a, b) => a + b, 0);
  return (
    <svg viewBox={`0 0 ${w} ${h}`} width={w} height={h} role="img" aria-label={`${label}: ${total} over ${weeks.length} weeks`}
      style={{ display: 'block', flex: 'none' }}>
      <title>{`${label}, weekly: ${weeks.join(', ')}`}</title>
      <line x1={0} x2={w} y1={h - 0.5} y2={h - 0.5} stroke="var(--border)" />
      {weeks.map((v, i) =>
        v > 0 ? (
          <rect key={i} x={i * step + 1} y={h - 1 - (v / max) * (h - 3)} width={Math.max(1, step - 2)} height={(v / max) * (h - 3)}
            rx={1} fill="var(--accent)" opacity={i >= weeks.length - paceWeeks ? 1 : 0.45} />
        ) : null
      )}
    </svg>
  );
};

const TREND: Record<string, { label: string; color: string; background: string; arrow: string }> = {
  accelerating: { label: 'Accelerating', color: 'var(--success-text)', background: 'var(--tint-green)', arrow: '▲' },
  steady: { label: 'Steady', color: 'var(--text)', background: 'transparent', arrow: '▶' },
  slowing: { label: 'Slowing', color: 'var(--warning-text)', background: 'var(--tint-yellow)', arrow: '▼' },
  new: { label: 'New', color: 'var(--accent)', background: 'var(--tint-blue)', arrow: '●' },
  dormant: { label: 'Gone quiet', color: 'var(--danger-text)', background: 'var(--tint-red)', arrow: '■' },
  idle: { label: 'No activity', color: 'var(--text-muted)', background: 'transparent', arrow: '–' },
};

export const TREND_OPTIONS = Object.entries(TREND).map(([value, t]) => ({ value, label: t.label }));

const signed = (v: number) => `${v >= 0 ? '+' : '−'}${Math.abs(Math.round(v * 100))}%`;

/** The pace: its trend, and the change on the window before when both have activity. */
export const PaceBadge: React.FC<{ pace: UsagePace; paceWeeks: number }> = ({ pace, paceWeeks }) => {
  const t = TREND[pace.trend] || TREND.idle;
  const showChange = pace.trend === 'accelerating' || pace.trend === 'steady' || pace.trend === 'slowing';
  return (
    <span
      title={`${pace.recent} in the last ${paceWeeks} weeks, ${pace.previous} in the ${paceWeeks} before`}
      style={{
        display: 'inline-flex',
        gap: 4,
        alignItems: 'center',
        whiteSpace: 'nowrap',
        fontSize: 12,
        padding: '2px 8px',
        borderRadius: 10,
        border: '1px solid var(--border-soft)',
        color: t.color,
        background: t.background,
      }}
    >
      <span aria-hidden>{t.arrow}</span>
      {t.label}
      {showChange && <span style={{ fontVariantNumeric: 'tabular-nums' }}>{signed(pace.change)}</span>}
    </span>
  );
};

/** "today", "3 days ago", or the date; "never" when there is none. */
export const lastSeen = (day?: string, now: Date = new Date()): string => {
  if (!day) return 'never';
  const then = Date.parse(`${day}T00:00:00Z`);
  const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  const days = Math.round((today - then) / 86400000);
  if (days <= 0) return 'today';
  if (days === 1) return 'yesterday';
  if (days < 30) return `${days} days ago`;
  return new Date(then).toLocaleDateString();
};

import React from 'react';

// The pieces the redesign's pages share: a white panel with a 40 px header,
// a reference in monospace, a dense grid table with an uppercase header row,
// and a stacked pass / fail / blocked bar. Inline styles over the theme's
// tokens, so they follow the dark theme too.

export const MONO = "source-code-pro, Menlo, Monaco, Consolas, 'Courier New', monospace";

export const panelStyle: React.CSSProperties = {
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  minWidth: 0,
};

export const refStyle: React.CSSProperties = {
  fontFamily: MONO,
  fontSize: 12,
  color: 'var(--accent-text)',
};

/** The uppercase label over a small panel ("Satisfies · 3"). */
export const eyebrowStyle: React.CSSProperties = {
  margin: 0,
  fontSize: 12,
  fontWeight: 600,
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
  color: 'var(--text-muted)',
};

export const mutedStyle: React.CSSProperties = { fontSize: 12, color: 'var(--text-muted)' };

/** A link drawn without the global underline: an action or a row title. */
export const plainLink: React.CSSProperties = { textDecoration: 'none', color: 'var(--accent-text)' };

/** A link or button big enough to tap on a phone (32 px), centred in its row. */
export const tapTarget: React.CSSProperties = { display: 'inline-flex', alignItems: 'center', minHeight: 32, minWidth: 32 };

/** The redesign's dense type: 13 px on a computer, 14 px on a phone. */
export const pageType = (phone: boolean): React.CSSProperties => ({ fontSize: phone ? 14 : 13, lineHeight: 1.45 });

/** A panel's header row: its title with an optional count, and an action on the right. */
export const PanelHeader: React.FC<{
  title: React.ReactNode;
  count?: number;
  action?: React.ReactNode;
  id?: string;
}> = ({ title, count, action, id }) => (
  <div
    style={{
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'space-between',
      gap: 8,
      minHeight: 40,
      padding: '0 12px',
      borderBottom: '1px solid var(--border-soft)',
    }}
  >
    <h2 id={id} style={{ margin: 0, fontSize: 14, fontWeight: 600 }}>
      {title}
      {count !== undefined && (
        <span style={{ color: 'var(--text-muted)', fontWeight: 500 }}> {count}</span>
      )}
    </h2>
    {action && <span style={{ fontSize: 12 }}>{action}</span>}
  </div>
);

/** The header row of a grid table: the columns' template and their names. */
export const GridHead: React.FC<{ columns: string; names: React.ReactNode[] }> = ({ columns, names }) => (
  <div
    role="row"
    style={{
      display: 'grid',
      gridTemplateColumns: columns,
      gap: 8,
      alignItems: 'center',
      minHeight: 30,
      padding: '0 12px',
      background: 'var(--surface-alt)',
      color: 'var(--text-muted)',
      fontSize: 12,
      fontWeight: 600,
      textTransform: 'uppercase',
      letterSpacing: '0.04em',
    }}
  >
    {names.map((n, i) => (
      <span role="columnheader" key={i}>
        {n}
      </span>
    ))}
  </div>
);

export const gridRowStyle = (columns: string, selected = false): React.CSSProperties => ({
  display: 'grid',
  gridTemplateColumns: columns,
  gap: 8,
  alignItems: 'center',
  minHeight: 34,
  padding: '0 12px',
  borderTop: '1px solid var(--border-soft)',
  background: selected ? 'var(--tint-blue)' : 'var(--surface)',
  color: 'var(--text)',
});

export const ellipsis: React.CSSProperties = {
  minWidth: 0,
  whiteSpace: 'nowrap',
  overflow: 'hidden',
  textOverflow: 'ellipsis',
};

/** A stacked bar of pass, fail and blocked out of a total; the rest is the track. */
export const ResultBar: React.FC<{ pass: number; fail: number; blocked: number; total: number; height?: number }> = ({
  pass,
  fail,
  blocked,
  total,
  height = 6,
}) => {
  const pct = (n: number) => (total > 0 ? `${(n / total) * 100}%` : '0%');
  return (
    <span
      role="img"
      aria-label={`${pass} pass, ${fail} fail, ${blocked} blocked of ${total}`}
      style={{ display: 'flex', height, borderRadius: height / 2, overflow: 'hidden', background: 'var(--neutral-soft)' }}
    >
      <span style={{ width: pct(pass), background: 'var(--success)' }} />
      <span style={{ width: pct(fail), background: 'var(--danger)' }} />
      <span style={{ width: pct(blocked), background: 'var(--warning)' }} />
    </span>
  );
};

/** A thin labelled meter: "Traced ▬▬▬▬── 91%". */
export const Meter: React.FC<{ label: string; value: number; color: string }> = ({ label, value, color }) => (
  <span style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-muted)' }}>
    <span style={{ width: 48 }}>{label}</span>
    <span
      style={{ flex: 1, height: 4, borderRadius: 999, background: 'var(--neutral-soft)', overflow: 'hidden', display: 'flex' }}
    >
      <span style={{ width: `${value}%`, background: color }} />
    </span>
    <span style={{ width: 32, textAlign: 'right' }}>{value}%</span>
  </span>
);

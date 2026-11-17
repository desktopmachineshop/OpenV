import type React from 'react';

// Inline button styles for the pages that draw their own buttons rather than
// using the .button classes (toolbars, row actions). One hierarchy, as the
// design system asks: one primary action per area in the accent blue, quiet
// outlined secondary actions beside it, and destructive actions as a red
// outline until confirmed, so a toolbar reads as one thing to do and its
// alternatives rather than a row of colours.

export type ButtonKind = 'primary' | 'secondary' | 'danger' | 'agent' | 'ghost';

const BASE: React.CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  gap: 6,
  minHeight: 32,
  padding: '0 12px',
  borderRadius: 6,
  fontSize: 13,
  fontWeight: 600,
  lineHeight: 1.2,
  cursor: 'pointer',
  whiteSpace: 'nowrap',
  textDecoration: 'none',
};

const KINDS: Record<ButtonKind, React.CSSProperties> = {
  primary: { background: 'var(--accent-strong)', color: '#fff', border: '1px solid var(--accent-strong)' },
  secondary: { background: 'var(--surface)', color: 'var(--text-secondary)', border: '1px solid var(--border)' },
  danger: { background: 'var(--surface)', color: 'var(--danger-strong)', border: '1px solid var(--tint-red-border)' },
  agent: { background: 'var(--tint-purple)', color: 'var(--purple-strong)', border: '1px solid var(--tint-purple-border)' },
  ghost: { background: 'transparent', color: 'var(--text-secondary)', border: '1px solid transparent' },
};

const DISABLED: React.CSSProperties = {
  background: 'var(--surface-alt)',
  color: 'var(--text-muted)',
  border: '1px solid var(--border-soft)',
  cursor: 'not-allowed',
};

/** The inline style of a button of this kind; disabled greys any kind out. */
export const buttonStyle = (kind: ButtonKind, opts: { disabled?: boolean; compact?: boolean } = {}): React.CSSProperties => ({
  ...BASE,
  ...KINDS[kind],
  ...(opts.compact ? { minHeight: 26, padding: '0 8px', fontSize: 12 } : {}),
  ...(opts.disabled ? DISABLED : {}),
});

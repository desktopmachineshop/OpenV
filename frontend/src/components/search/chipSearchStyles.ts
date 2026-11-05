import type React from 'react';

// Shared inline styles of the chip search bar and its panel, on the theme's
// tokens so both read in light and dark mode.

export const menuItem = (on = false): React.CSSProperties => ({
  display: 'flex',
  alignItems: 'center',
  gap: 8,
  width: '100%',
  minHeight: 30,
  padding: '4px 8px',
  border: 'none',
  borderRadius: 4,
  background: 'transparent',
  color: 'var(--text)',
  fontSize: 13,
  fontWeight: on ? 600 : 400,
  textAlign: 'left',
  cursor: 'pointer',
});

export const columnHeading: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: 6,
  margin: '0 0 6px',
  padding: '0 8px',
  fontSize: 11,
  fontWeight: 700,
  letterSpacing: '0.04em',
  textTransform: 'uppercase',
  color: 'var(--text-muted)',
};

export const smallInput: React.CSSProperties = {
  width: '100%',
  boxSizing: 'border-box',
  minHeight: 30,
  padding: '4px 8px',
  border: '1px solid var(--border)',
  borderRadius: 4,
  background: 'var(--surface)',
  color: 'var(--text)',
  fontSize: 13,
};

export const check = (on: boolean): React.CSSProperties => ({
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  flex: 'none',
  width: 14,
  height: 14,
  borderRadius: 3,
  border: `1px solid ${on ? 'var(--accent)' : 'var(--neutral-mid)'}`,
  background: on ? 'var(--accent)' : 'transparent',
  color: 'var(--surface)',
  fontSize: 10,
  lineHeight: 1,
});

export const divider: React.CSSProperties = { height: 1, margin: '6px 0', background: 'var(--border-soft)' };

/** Moves focus to the next or previous menu item inside root, wrapping. */
export function stepMenuFocus(root: HTMLElement | null, delta: 1 | -1): void {
  if (!root) return;
  const items = Array.from(root.querySelectorAll<HTMLElement>('[role^="menuitem"]'));
  if (items.length === 0) return;
  const at = items.indexOf(document.activeElement as HTMLElement);
  const next = at < 0 ? (delta > 0 ? 0 : items.length - 1) : (at + delta + items.length) % items.length;
  items[next].focus();
}

import React from 'react';

/** One row of a token menu. */
export interface TokenMenuRow {
  /** React key, and what identifies the row to the caller. */
  key: string;
  /** The token itself: a reference, or a handle. */
  primary: string;
  /** What it is, in muted text after the token. */
  secondary?: string;
}

interface TokenMenuProps {
  rows: TokenMenuRow[];
  /** Index of the row arrow keys have landed on. */
  highlight: number;
  onHighlight: (index: number) => void;
  onChoose: (index: number) => void;
  'aria-label'?: string;
}

/**
 * The menu that opens under a textarea while a token is being typed — "#" and
 * "##" for references, "@" and "@@" for people.
 *
 * Shared so the description editor and the note composer offer the same thing
 * in the same shape: someone who has learnt one has learnt the other. It is
 * presentational only; which tokens are on offer, and what choosing one does,
 * belong to the caller.
 *
 * It positions itself against the nearest positioned ancestor, so the caller
 * wraps its textarea in a relatively positioned element.
 */
export const TokenMenu: React.FC<TokenMenuProps> = ({
  rows,
  highlight,
  onHighlight,
  onChoose,
  'aria-label': ariaLabel,
}) => {
  if (rows.length === 0) return null;
  return (
    <div
      role="listbox"
      aria-label={ariaLabel}
      style={{
        position: 'absolute',
        left: 8,
        right: 8,
        zIndex: 20,
        background: 'var(--surface)',
        border: '1px solid var(--border)',
        borderRadius: 4,
        boxShadow: '0 2px 8px rgba(0,0,0,0.15)',
        maxHeight: 220,
        overflowY: 'auto',
      }}
    >
      {rows.map((row, i) => (
        // onMouseDown, not onClick: blur fires first and would close the menu
        // before a click could land.
        <div
          key={row.key}
          role="option"
          aria-selected={i === highlight}
          onMouseDown={(e) => {
            e.preventDefault();
            onChoose(i);
          }}
          onMouseEnter={() => onHighlight(i)}
          style={{
            padding: '6px 10px',
            cursor: 'pointer',
            fontSize: 12,
            background: i === highlight ? 'var(--tint-blue)' : 'transparent',
          }}
        >
          <span style={{ fontWeight: 700 }}>{row.primary}</span>
          {row.secondary && (
            <span style={{ color: 'var(--text-muted)' }}> · {row.secondary}</span>
          )}
        </div>
      ))}
    </div>
  );
};

export default TokenMenu;

import React, { useEffect, useRef, useState } from 'react';
import { useViewport } from '../../hooks/useViewport';

/** One entry of an action menu. */
export interface ActionMenuItem {
  key: string;
  label: string;
  onSelect: () => void;
  /** A destructive action: drawn in the danger colour, and kept last by the caller. */
  danger?: boolean;
  disabled?: boolean;
  /** Why the entry is disabled, as its tooltip. */
  hint?: string;
}

interface ActionMenuProps {
  /** Accessible name of the trigger and of the menu it opens. */
  label: string;
  items: ActionMenuItem[];
  /** What the trigger shows; the ⋯ glyph when absent. */
  children?: React.ReactNode;
  /** Style of the trigger button, so it matches the buttons beside it. */
  triggerStyle?: React.CSSProperties;
  /** Class of the trigger button, where a stylesheet draws it (a tab of the bottom bar). */
  triggerClassName?: string;
}

/**
 * The actions that do not earn a button of their own, behind one trigger:
 * a popover under the trigger on a computer, a sheet rising from the bottom
 * of the screen on a phone, where the thumb is. Escape, a click outside and
 * choosing an entry close it.
 */
export const ActionMenu: React.FC<ActionMenuProps> = ({ label, items, children, triggerStyle, triggerClassName }) => {
  const { isPhone } = useViewport();
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    if (!open) return undefined;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    const onDown = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    window.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onDown);
    return () => {
      window.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onDown);
    };
  }, [open]);

  if (items.length === 0) return null;
  return (
    <span ref={root} style={{ position: 'relative', display: 'inline-flex' }}>
      <button
        type="button"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        title={label}
        onClick={() => setOpen((shown) => !shown)}
        style={triggerStyle}
        className={triggerClassName}
      >
        {children ?? '⋯'}
      </button>
      {open && isPhone && <span className="ov-action-scrim" aria-hidden="true" onClick={() => setOpen(false)} />}
      {open && (
        <span role="menu" aria-label={label} className={isPhone ? 'ov-action-menu is-sheet safe-area-bottom' : 'ov-action-menu'}>
          {items.map((item) => (
            <button
              key={item.key}
              type="button"
              role="menuitem"
              disabled={item.disabled}
              title={item.disabled ? item.hint : undefined}
              className={`ov-action-item${item.danger ? ' is-danger' : ''}`}
              onClick={() => {
                setOpen(false);
                item.onSelect();
              }}
            >
              {item.label}
            </button>
          ))}
        </span>
      )}
    </span>
  );
};

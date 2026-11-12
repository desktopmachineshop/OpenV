import React, { useEffect, useRef, useState } from 'react';
import { buttonStyle } from '../../components/ui';

// The requirements toolbar behind one button (ui-refresh, on a wide screen):
// the baseline picker with compare, capture and delete, test-case drafting
// and the download. The button says which copy is on screen, the working
// copy or a baseline, so it replaces the chip that said so. Its items are
// the Toolbar's own; a click that lands in one of its buttons closes it, as
// in the phone's actions sheet.
interface ToolbarMenuProps {
  isBaselineView: boolean;
  toolbarActions: React.ReactNode;
}

export const ToolbarMenu: React.FC<ToolbarMenuProps> = ({ isBaselineView, toolbarActions }) => {
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

  return (
    <span ref={root} style={{ position: 'relative', display: 'inline-flex', flex: 'none' }}>
      <button
        type="button"
        aria-haspopup="true"
        aria-expanded={open}
        onClick={() => setOpen((shown) => !shown)}
        title="Baselines, test drafting and download"
        style={{
          ...buttonStyle('secondary'),
          height: 30,
          minHeight: 30,
          ...(isBaselineView ? { borderColor: 'var(--tint-yellow-border)', background: 'var(--tint-yellow)', color: 'var(--warning-text)' } : {}),
        }}
      >
        {isBaselineView ? 'Baseline' : 'Working copy'}
        <span aria-hidden="true" style={{ fontSize: 12, marginLeft: 2 }}>
          ▾
        </span>
      </button>
      {open && (
        <div
          role="group"
          aria-label="Baselines, test drafting and download"
          className="ov-toolbar-menu action-sheet"
          onClick={(e) => {
            // Any button in the menu is a one-shot action: close on use.
            if ((e.target as HTMLElement).closest('button')) setOpen(false);
          }}
        >
          <label style={{ fontSize: 12 }}>Baseline</label>
          {toolbarActions}
        </div>
      )}
    </span>
  );
};

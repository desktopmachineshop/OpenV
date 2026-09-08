import React, { useEffect } from 'react';

interface SheetProps {
  /** Accessible name of the sheet (the panel's own heading repeats it). */
  label: string;
  onClose: () => void;
  children: React.ReactNode;
}

/**
 * Full-screen sheet for a side pane that has no room beside the main pane
 * on a compact viewport (an agent editor, a run's detail, a crew node's
 * settings). The pane keeps its own close button; the sheet only adds the
 * surface, the Escape key and the safe-area padding.
 */
export const Sheet: React.FC<SheetProps> = ({ label, onClose, children }) => {
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [onClose]);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={label}
      className="safe-area-top safe-area-bottom"
      style={{
        position: 'fixed',
        inset: 0,
        // Above the project nav drawer (900), below dialogs (2000).
        zIndex: 950,
        background: 'var(--bg-app)',
        overflowY: 'auto',
        padding: 12,
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      {children}
    </div>
  );
};

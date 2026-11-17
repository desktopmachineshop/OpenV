import React, { useEffect } from 'react';
import { useViewport } from '../../hooks/useViewport';
import { useUiRefresh } from '../../hooks/useUiRefresh';
import { dialogCardStyle } from './dialogCard';

interface ModalProps {
  title?: React.ReactNode;
  width?: number;
  onClose: () => void;
  children: React.ReactNode;
  /** Extra styles for the inner card (e.g. maxHeight overrides). */
  cardStyle?: React.CSSProperties;
}

/**
 * Shared modal shell: token-styled overlay + card, closes on overlay click
 * and Escape. Render it conditionally — mounting the component opens it.
 *
 * On a phone in the refreshed interface (ui-refresh) it is a sheet the
 * height of the screen: a form has the whole screen to itself, its title
 * stays at the top beside a Close button, and its fields scroll under it.
 */
const SHEET: React.CSSProperties = {
  width: '100%',
  maxWidth: 'none',
  height: '100%',
  maxHeight: 'none',
  borderRadius: 0,
  border: 'none',
  margin: 0,
  padding: '0 16px calc(16px + env(safe-area-inset-bottom))',
  overflowY: 'auto',
};
const SHEET_HEAD: React.CSSProperties = {
  position: 'sticky',
  top: 0,
  zIndex: 1,
  display: 'flex',
  alignItems: 'center',
  gap: 8,
  margin: '0 -16px 12px',
  padding: 'calc(6px + env(safe-area-inset-top)) 6px 6px 16px',
  background: 'var(--surface)',
  borderBottom: '1px solid var(--border-soft)',
};
const SHEET_CLOSE: React.CSSProperties = {
  width: 44,
  height: 44,
  flex: 'none',
  border: 'none',
  background: 'none',
  color: 'var(--text-secondary)',
  fontSize: 18,
  cursor: 'pointer',
};

export const Modal: React.FC<ModalProps> = ({ title, width = 520, onClose, children, cardStyle }) => {
  const { isPhone } = useViewport();
  // A short dialog (a choice, a confirmation) stays a card; a form gets the screen.
  const sheet = useUiRefresh() && isPhone && width >= 480;
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
      onClick={onClose}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'var(--overlay)',
        display: 'flex',
        alignItems: sheet ? 'stretch' : 'center',
        justifyContent: 'center',
        zIndex: 2000,
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        className="card"
        onClick={(e) => e.stopPropagation()}
        style={{
          ...dialogCardStyle(isPhone, width),
          background: 'var(--surface)',
          ...cardStyle,
          ...(sheet ? SHEET : {}),
        }}
      >
        {sheet ? (
          <div style={SHEET_HEAD}>
            <h3 style={{ margin: 0, flex: 1, minWidth: 0, color: 'var(--text)', fontSize: 16 }}>{title}</h3>
            <button type="button" aria-label="Close dialog" onClick={onClose} style={SHEET_CLOSE}>
              ✕
            </button>
          </div>
        ) : (
          title != null && <h3 style={{ marginTop: 0, color: 'var(--text)', fontSize: 16 }}>{title}</h3>
        )}
        {children}
      </div>
    </div>
  );
};

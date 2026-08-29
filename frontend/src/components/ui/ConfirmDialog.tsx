import React, { useEffect, useRef } from 'react';

export interface ConfirmDialogProps {
  title?: string;
  message: React.ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Style the confirm button as a destructive action. */
  danger?: boolean;
  /** Hide the cancel button (alert-style dialog with a single OK button). */
  hideCancel?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * Token-styled replacement for window.confirm / alert. Render it
 * conditionally — mounting the component opens it. Enter activates the
 * focused button (and confirms when focus is not on a button), Escape
 * cancels, Tab cycles between the buttons, and focus starts on the
 * confirm button.
 */
export const ConfirmDialog: React.FC<ConfirmDialogProps> = ({
  title,
  message,
  confirmLabel,
  cancelLabel = 'Cancel',
  danger = false,
  hideCancel = false,
  onConfirm,
  onCancel,
}) => {
  const boxRef = useRef<HTMLDivElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    confirmRef.current?.focus();
  }, []);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation();
      onCancel();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      // Enter must activate the *focused* button, not blindly confirm — a
      // keyboard user who tabbed to Cancel and pressed Enter would otherwise
      // fire the (possibly destructive) confirm action. When focus is on a
      // button we replicate native button activation deterministically;
      // Enter anywhere else in the dialog still confirms.
      const target = e.target instanceof HTMLElement ? e.target.closest('button') : null;
      if (target) {
        target.click();
      } else {
        onConfirm();
      }
    } else if (e.key === 'Tab') {
      // Keep focus inside the dialog (there are at most two buttons).
      const focusable = boxRef.current?.querySelectorAll<HTMLButtonElement>('button');
      if (!focusable || focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  };

  return (
    <div
      onClick={onCancel}
      onKeyDown={handleKeyDown}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'var(--overlay)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 3000,
      }}
    >
      <div
        ref={boxRef}
        role="alertdialog"
        aria-modal="true"
        className="card"
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 420,
          maxWidth: 'calc(100vw - 40px)',
          background: 'var(--surface)',
          borderRadius: 8,
          padding: 22,
          margin: 0,
        }}
      >
        {title && <h3 style={{ marginTop: 0, marginBottom: 10, color: 'var(--text)', fontSize: 16 }}>{title}</h3>}
        <div style={{ fontSize: 13.5, color: 'var(--text-body)', lineHeight: 1.5, marginBottom: 18 }}>
          {message}
        </div>
        <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
          {!hideCancel && (
            <button
              type="button"
              className="button-secondary"
              style={{ width: 'auto', padding: '8px 16px', fontSize: 13 }}
              onClick={onCancel}
            >
              {cancelLabel}
            </button>
          )}
          <button
            type="button"
            ref={confirmRef}
            style={{
              width: 'auto',
              padding: '8px 16px',
              fontSize: 13,
              background: danger ? 'var(--danger)' : 'var(--accent)',
              color: '#fff',
              border: 'none',
              borderRadius: 4,
              cursor: 'pointer',
              fontWeight: 600,
            }}
            onClick={onConfirm}
          >
            {confirmLabel || (hideCancel ? 'OK' : 'Confirm')}
          </button>
        </div>
      </div>
    </div>
  );
};

import React, { useEffect } from 'react';

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
 */
export const Modal: React.FC<ModalProps> = ({ title, width = 520, onClose, children, cardStyle }) => {
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
        alignItems: 'center',
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
          width,
          maxWidth: 'calc(100vw - 40px)',
          maxHeight: 'calc(100vh - 60px)',
          overflowY: 'auto',
          background: 'var(--surface)',
          borderRadius: 8,
          padding: 24,
          margin: 0,
          ...cardStyle,
        }}
      >
        {title != null && <h3 style={{ marginTop: 0, color: 'var(--text)', fontSize: 16 }}>{title}</h3>}
        {children}
      </div>
    </div>
  );
};

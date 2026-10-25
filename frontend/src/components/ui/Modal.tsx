import React, { useEffect } from 'react';
import { useViewport } from '../../hooks/useViewport';
import { dialogCardStyle } from './dialogCard';
import { HelpLink } from './HelpLink';
import { useHelpContext } from '../../hooks/useHelp';

interface ModalProps {
  title?: React.ReactNode;
  width?: number;
  onClose: () => void;
  children: React.ReactNode;
  /** Extra styles for the inner card (e.g. maxHeight overrides). */
  cardStyle?: React.CSSProperties;
  /** The manual's topic for the dialog: while it is open the help panel
   *  follows it, and a ? beside the title opens it (hooks/useHelp.tsx). */
  help?: { key: string; label: string; fragment?: string };
}

/**
 * Shared modal shell: token-styled overlay + card, closes on overlay click
 * and Escape. Render it conditionally — mounting the component opens it.
 */
export const Modal: React.FC<ModalProps> = ({ title, width = 520, onClose, children, cardStyle, help }) => {
  const { isPhone } = useViewport();
  useHelpContext(help?.key, { fragment: help?.fragment, dialog: true });
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
          ...dialogCardStyle(isPhone, width),
          background: 'var(--surface)',
          ...cardStyle,
        }}
      >
        {title != null && (
          <h3 style={{ marginTop: 0, color: 'var(--text)', fontSize: 16 }}>
            {title}
            {help && <HelpLink helpKey={help.key} fragment={help.fragment} label={help.label} />}
          </h3>
        )}
        {children}
      </div>
    </div>
  );
};

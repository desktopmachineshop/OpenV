import React from 'react';

interface ErrorBannerProps {
  /** Renders nothing when falsy, so it can be used unconditionally. */
  message: string;
  /** When provided, a × dismiss button is shown. */
  onDismiss?: () => void;
  style?: React.CSSProperties;
}

/**
 * Unified error display: token-styled tinted banner with an optional
 * dismiss button. Replaces the ad-hoc bare red-text patterns.
 */
export const ErrorBanner: React.FC<ErrorBannerProps> = ({ message, onDismiss, style }) => {
  if (!message) return null;
  return (
    <div
      role="alert"
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: 10,
        background: 'var(--tint-red)',
        border: '1px solid var(--tint-red-border)',
        color: 'var(--danger-strong)',
        borderRadius: 4,
        padding: '8px 12px',
        marginBottom: 10,
        fontSize: 13,
        ...style,
      }}
    >
      <span style={{ flex: 1, lineHeight: 1.5, overflowWrap: 'anywhere' }}>{message}</span>
      {onDismiss && (
        <button
          onClick={onDismiss}
          aria-label="Dismiss error"
          title="Dismiss"
          style={{
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            color: 'var(--danger-strong)',
            fontSize: 15,
            lineHeight: 1,
            padding: '1px 2px',
            flexShrink: 0,
          }}
        >
          ×
        </button>
      )}
    </div>
  );
};

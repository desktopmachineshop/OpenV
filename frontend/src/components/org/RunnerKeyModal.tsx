import React, { useState } from 'react';
import { useViewport } from '../../hooks/useViewport';
import { dialogCardStyle } from '../ui/dialogCard';

interface RunnerKeyModalProps {
  title: string;
  plaintext: string;
  onClose: () => void;
}

// Shows a freshly created worker/runner key exactly once, with a copy button
// and the agentd setup snippet. The plaintext is never retrievable again.
export const RunnerKeyModal: React.FC<RunnerKeyModalProps> = ({ title, plaintext, onClose }) => {
  const [copied, setCopied] = useState(false);
  const { isPhone } = useViewport();

  const copyKey = async () => {
    try {
      await navigator.clipboard.writeText(plaintext);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard unavailable — user can select the text manually.
    }
  };

  const setupSnippet = `bin\\agentd.exe --api http://localhost:8080 --worker-key ${plaintext}`;

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
        className="card"
        onClick={(e) => e.stopPropagation()}
        style={{ ...dialogCardStyle(isPhone, 560), background: 'var(--surface)' }}
      >
        <h3 style={{ marginTop: 0, color: 'var(--text)' }}>{title}</h3>
        <div
          style={{
            background: 'var(--tint-yellow)',
            border: '1px solid var(--warning)',
            color: 'var(--warning-text)',
            padding: '10px 14px',
            borderRadius: 4,
            fontSize: 13,
            marginBottom: 14,
          }}
        >
          This key is shown only once. Store it somewhere safe before closing.
        </div>
        {/* The key wraps on narrow screens so every character is visible;
            the Copy button drops under it rather than squeezing it. */}
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 16, flexWrap: 'wrap' }}>
          <code
            style={{
              flex: '1 1 240px',
              minWidth: 0,
              background: 'var(--code-block-bg)',
              color: 'var(--code-block-text)',
              padding: '10px 12px',
              borderRadius: 4,
              fontSize: 13,
              wordBreak: 'break-all',
              whiteSpace: isPhone ? 'normal' : 'nowrap',
              overflowX: 'auto',
            }}
          >
            {plaintext}
          </code>
          <button className="button" style={{ padding: '8px 14px', width: 'auto', flexShrink: 0 }} onClick={copyKey}>
            {copied ? 'Copied!' : 'Copy'}
          </button>
        </div>
        <div style={{ fontSize: 13, color: 'var(--text)', marginBottom: 6, fontWeight: 600 }}>
          Runner setup
        </div>
        <pre
          style={{
            background: 'var(--surface-alt)',
            border: '1px solid var(--border-soft)',
            borderRadius: 4,
            padding: '10px 12px',
            fontSize: 12,
            overflowX: 'auto',
            margin: '0 0 6px',
          }}
        >
          {setupSnippet}
        </pre>
        <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 16 }}>
          Replace http://localhost:8080 with the address of the OpenV server the runner should
          connect to.
        </div>
        <div style={{ textAlign: 'right' }}>
          <button className="button-secondary button" style={{ width: 'auto' }} onClick={onClose}>
            Done — I saved the key
          </button>
        </div>
      </div>
    </div>
  );
};

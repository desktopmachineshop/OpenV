import React, { useState } from 'react';

interface RunnerKeyModalProps {
  title: string;
  plaintext: string;
  onClose: () => void;
}

// Shows a freshly created worker/runner key exactly once, with a copy button
// and the agentd setup snippet. The plaintext is never retrievable again.
export const RunnerKeyModal: React.FC<RunnerKeyModalProps> = ({ title, plaintext, onClose }) => {
  const [copied, setCopied] = useState(false);

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
        background: 'rgba(44, 62, 80, 0.55)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 2000,
      }}
    >
      <div
        className="card"
        onClick={(e) => e.stopPropagation()}
        style={{ width: 560, maxWidth: '90vw', background: '#fff', borderRadius: 8, padding: 24, margin: 0 }}
      >
        <h3 style={{ marginTop: 0, color: '#2c3e50' }}>{title}</h3>
        <div
          style={{
            background: '#fef5e7',
            border: '1px solid #f39c12',
            color: '#9c6a0b',
            padding: '10px 14px',
            borderRadius: 4,
            fontSize: 13,
            marginBottom: 14,
          }}
        >
          This key is shown only once. Store it somewhere safe before closing.
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 16 }}>
          <code
            style={{
              flex: 1,
              background: '#2c3e50',
              color: '#ecf0f1',
              padding: '10px 12px',
              borderRadius: 4,
              fontSize: 13,
              overflowX: 'auto',
              whiteSpace: 'nowrap',
            }}
          >
            {plaintext}
          </code>
          <button className="button" style={{ padding: '8px 14px', width: 'auto' }} onClick={copyKey}>
            {copied ? 'Copied!' : 'Copy'}
          </button>
        </div>
        <div style={{ fontSize: 13, color: '#2c3e50', marginBottom: 6, fontWeight: 600 }}>
          Runner setup
        </div>
        <pre
          style={{
            background: '#f8f9fa',
            border: '1px solid #eee',
            borderRadius: 4,
            padding: '10px 12px',
            fontSize: 12,
            overflowX: 'auto',
            margin: '0 0 6px',
          }}
        >
          {setupSnippet}
        </pre>
        <div style={{ fontSize: 12, color: '#7f8c8d', marginBottom: 16 }}>
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

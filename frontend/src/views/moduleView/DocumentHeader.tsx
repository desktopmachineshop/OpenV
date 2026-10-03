import React from 'react';
import type { Setter } from './shared';

// The requirements document's header row: its title and, beside it, the
// toolbar on a wide screen or, stacked, the ⋯ button that opens the phone
// actions sheet instead.
// Props-only (refactor plan F7): the ModuleView shell owns the state, the
// effects, the loads and the handlers.
interface DocumentHeaderProps {
  stacked: boolean;
  toolsOpen: boolean;
  setToolsOpen: Setter<boolean>;
  toolbarActions: React.ReactNode;
}

export const DocumentHeader: React.FC<DocumentHeaderProps> = ({ stacked, toolsOpen, setToolsOpen, toolbarActions }) => (
  <div style={{ display: 'flex', alignItems: 'center', gap: '10px', padding: stacked ? '8px 12px 6px' : '16px 20px 12px', flexWrap: 'wrap', flexShrink: 0 }}>
    <h2 style={{ color: 'var(--text)', margin: 0, fontSize: stacked ? 20 : undefined }}>Requirements</h2>
    <div style={{ flex: 1 }} />
    {stacked ? (
      <button
        type="button"
        aria-label="Requirements actions"
        aria-expanded={toolsOpen}
        onClick={() => setToolsOpen(true)}
        title="Baselines, test drafting and download"
        style={{
          width: 44,
          height: 44,
          border: '1px solid var(--border)',
          borderRadius: 6,
          background: 'var(--surface)',
          color: 'var(--text)',
          fontSize: 22,
          lineHeight: 1,
          cursor: 'pointer',
          flexShrink: 0,
        }}
      >
        ⋯
      </button>
    ) : (
      toolbarActions
    )}
  </div>
);

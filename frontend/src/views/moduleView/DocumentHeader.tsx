import React from 'react';
import { Chip } from '../../components/ui';
import type { Setter } from './shared';

// The requirements header row: the title and whether the live project or a
// baseline is on screen, then, on a wide screen, the toolbar with the one
// primary action (+ New Artifact) at its end; stacked, the ⋯ button that
// opens the phone actions sheet instead.
// Props-only (refactor plan F7): the ModuleView shell owns the state, the
// effects, the loads and the handlers.
interface DocumentHeaderProps {
  stacked: boolean;
  toolsOpen: boolean;
  setToolsOpen: Setter<boolean>;
  toolbarActions: React.ReactNode;
  /** The create button, the header's primary action on a wide screen. */
  newButton?: React.ReactNode;
  isBaselineView: boolean;
}

export const DocumentHeader: React.FC<DocumentHeaderProps> = ({
  stacked,
  toolsOpen,
  setToolsOpen,
  toolbarActions,
  newButton,
  isBaselineView,
}) => (
  <div
    style={{
      display: 'flex',
      alignItems: 'center',
      gap: 10,
      padding: stacked ? '8px 12px 6px' : '10px 20px',
      flexWrap: 'wrap',
      flexShrink: 0,
      background: 'var(--surface)',
      borderBottom: stacked ? 'none' : '1px solid var(--border)',
    }}
  >
    <h2 style={{ color: 'var(--text)', margin: 0, fontSize: stacked ? 20 : 18 }}>Requirements</h2>
    <Chip tone={isBaselineView ? 'warning' : 'neutral'} title={isBaselineView ? 'A baseline: read-only' : 'The live project'}>
      {isBaselineView ? 'Baseline' : 'Working copy'}
    </Chip>
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
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
        {toolbarActions}
        {newButton}
      </div>
    )}
  </div>
);

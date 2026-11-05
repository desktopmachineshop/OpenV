import React from 'react';

export type StackedPane = 'tree' | 'document' | 'notes';

/**
 * The phone and tablet's pane switcher under the requirements toolbar: one
 * pane at a time, Tree, Document and (while the Notes panel exists) Notes.
 * With History in the artifact's pane (#725) there is no Notes pane.
 */
export const StackedPaneTabs: React.FC<{
  panes: readonly StackedPane[];
  shown: StackedPane;
  onShow: (pane: StackedPane) => void;
}> = ({ panes, shown, onShow }) => (
  <div
    role="tablist"
    aria-label="Requirements panes"
    data-help-key="requirements.layout"
    style={{ display: 'flex', margin: '0 12px 8px', border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden', flexShrink: 0 }}
  >
    {panes.map((pane) => {
      const active = shown === pane;
      const label = pane === 'tree' ? 'Tree' : pane === 'document' ? 'Document' : 'Notes';
      return (
        <button
          key={pane}
          type="button"
          role="tab"
          aria-selected={active}
          onClick={() => onShow(pane)}
          style={{
            flex: 1,
            minHeight: 44,
            border: 'none',
            borderLeft: pane === 'tree' ? 'none' : '1px solid var(--border)',
            background: active ? 'var(--neutral-soft)' : 'var(--surface)',
            color: active ? 'var(--text)' : 'var(--text-muted)',
            fontSize: 14,
            fontWeight: active ? 600 : 400,
            cursor: 'pointer',
          }}
        >
          {label}
        </button>
      );
    })}
  </div>
);

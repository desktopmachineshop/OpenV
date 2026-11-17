import React, { useEffect } from 'react';
import { buttonStyle } from '../../components/ui';

// Small pieces of the requirements page's header that the ModuleView shell
// places: the create button, and for the refreshed page on a phone
// (ui-refresh) the ⋯ button that opens the actions sheet from the list's bar
// and the way back from an open artifact to the list.

interface NewArtifactButtonProps {
  isCreating: boolean;
  stacked: boolean;
  onToggle: () => void;
}

/** + New Artifact, the page's one primary action; Cancel while its form is open. */
export const NewArtifactButton: React.FC<NewArtifactButtonProps> = ({ isCreating, stacked, onToggle }) => (
  <button
    type="button"
    onClick={onToggle}
    style={{ ...(isCreating ? buttonStyle('secondary') : buttonStyle('primary')), ...(stacked ? { minHeight: 40 } : { height: 30, minHeight: 30 }) }}
    data-help-key="requirements.create"
  >
    {isCreating ? 'Cancel' : '+ New Artifact'}
  </button>
);

/** The ⋯ button of the phone's list bar: baselines, test drafting and download. */
export const ToolsButton: React.FC<{ toolsOpen: boolean; onOpen: () => void }> = ({ toolsOpen, onOpen }) => (
  <button
    type="button"
    aria-label="Requirements actions"
    aria-expanded={toolsOpen}
    onClick={onOpen}
    title="Baselines, test drafting and download"
    style={{ ...buttonStyle('secondary'), minHeight: 40, minWidth: 40, padding: 0, flex: 'none', fontSize: 18, lineHeight: 1 }}
  >
    ⋯
  </button>
);

/** The way back from an open artifact to the list, on a phone. */
export const BackToList: React.FC<{ onBack: () => void }> = ({ onBack }) => (
  <div style={{ flexShrink: 0, display: 'flex', alignItems: 'center', background: 'var(--surface)', borderBottom: '1px solid var(--border)' }}>
    <button
      type="button"
      aria-label="Back to the list"
      onClick={onBack}
      style={{ display: 'flex', alignItems: 'center', gap: 6, minHeight: 44, padding: '0 14px 0 8px', border: 'none', background: 'none', color: 'var(--accent-text)', fontSize: 14, fontWeight: 600, cursor: 'pointer' }}
    >
      <svg width="18" height="18" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M12 4l-6 6 6 6" />
      </svg>
      Requirements
    </button>
  </div>
);

/**
 * While an artifact is open on a phone it is a screen of its own: the shell
 * hides its row of pages and its bottom bar (index.css, .ov-pushed), so the
 * artifact's own action bar is the only bar under it. Mounted while the
 * artifact is open; it draws nothing.
 */
export const PushedScreen: React.FC = () => {
  useEffect(() => {
    const root = document.documentElement;
    root.classList.add('ov-pushed');
    return () => root.classList.remove('ov-pushed');
  }, []);
  return null;
};

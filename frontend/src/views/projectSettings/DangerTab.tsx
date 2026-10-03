import React from 'react';

// The Danger Zone tab of project settings: deleting the project.
// Props-only (refactor plan F6): the ProjectSettings shell owns the state, the
// loads and the handlers, so a tab switch keeps unsaved input.
interface DangerTabProps {
  deleting: boolean;
  handleDeleteProject: () => void;
}

export const DangerTab: React.FC<DangerTabProps> = ({ deleting, handleDeleteProject }) => (
  <div className="card" style={{ border: '1px solid var(--danger)' }}>
    <h3 style={{ color: 'var(--danger)' }}>Danger zone</h3>
    <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
      Deleting the project permanently removes all artifacts, links, baselines, test runs, work
      items and interview data. This cannot be undone.
    </p>
    <button
      onClick={handleDeleteProject}
      disabled={deleting}
      style={{
        background: 'var(--danger)',
        color: '#fff',
        border: 'none',
        padding: '10px 20px',
        borderRadius: 4,
        cursor: 'pointer',
        fontSize: 14,
        width: 'auto',
      }}
    >
      {deleting ? 'Deleting…' : 'Delete this project'}
    </button>
  </div>
);

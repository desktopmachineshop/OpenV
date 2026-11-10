import React, { useState } from 'react';
import { AgentDef, agentsAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';

// The workspace's archived agents, under the hierarchy. An archived agent is
// out of the team and cannot run, and its runs and proposals are kept (a
// delete would take them with it); Unarchive puts it back where it was.

interface Props {
  agents: AgentDef[];
  /** Workspace admins unarchive; everyone else reads the list. */
  canEdit: boolean;
  onChanged: () => void;
  onEdit: (agent: AgentDef) => void;
  onError: (message: string) => void;
}

export const ArchivedAgents: React.FC<Props> = ({ agents, canEdit, onChanged, onEdit, onError }) => {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState('');
  if (agents.length === 0) return null;
  const unarchive = async (agent: AgentDef) => {
    setBusy(agent.slug);
    try {
      await agentsAPI.update(agent.slug, { ...agent, archived: false });
      onChanged();
    } catch (err) {
      onError(apiErrorMessage(err, `Failed to unarchive ${agent.name}`));
    } finally {
      setBusy('');
    }
  };
  return (
    <section aria-labelledby="archived-agents" data-help-key="agents.archived" style={{ marginTop: 24 }}>
      <button
        type="button"
        id="archived-agents"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', fontSize: 14, fontWeight: 600, color: 'var(--text)' }}
      >
        {open ? '▾' : '▸'} Archived ({agents.length})
      </button>
      {open && (
        <ul style={{ listStyle: 'none', margin: '8px 0 0', padding: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
          {agents.map((a) => (
            <li
              key={a.id}
              style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '8px 12px', border: '1px solid var(--border-soft)', borderRadius: 6 }}
            >
              <span style={{ flex: 1, minWidth: 0 }}>
                <b style={{ fontWeight: 600 }}>{a.name}</b>
                {a.description && <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)' }}>{a.description}</span>}
              </span>
              {canEdit && (
                <>
                  <button type="button" className="button-secondary" onClick={() => onEdit(a)}>
                    Edit
                  </button>
                  <button type="button" className="button-secondary" disabled={busy === a.slug} onClick={() => unarchive(a)}>
                    Unarchive
                  </button>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
};

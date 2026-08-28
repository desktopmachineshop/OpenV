import React, { useState } from 'react';
import { orgsAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';

interface CreateOrgModalProps {
  onClose: () => void;
  /** Called after the new workspace has been created and activated. */
  onCreated?: (orgId: string) => void;
}

// Small modal to create a company workspace. On success it refreshes the org
// list, activates the new workspace and switches the app to it.
export const CreateOrgModal: React.FC<CreateOrgModalProps> = ({ onClose, onCreated }) => {
  const { setOrgs, setActiveOrgId } = useAppStore();
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    setError('');
    try {
      const created = await orgsAPI.create(name.trim());
      const newOrgId = created.data.id;
      // Persist server-side default, refresh the list, then switch locally.
      try {
        await orgsAPI.activate(newOrgId);
      } catch {
        // non-fatal — local switch still applies the header
      }
      try {
        const listed = await orgsAPI.list();
        setOrgs(listed.data.orgs || []);
      } catch {
        // keep whatever we have; the created org is still usable
      }
      setActiveOrgId(newOrgId);
      onCreated?.(newOrgId);
      onClose();
    } catch (err: any) {
      setError(`Failed to create workspace: ${apiErrorMessage(err)}`);
      setBusy(false);
    }
  };

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
        style={{ width: 400, background: 'var(--surface)', borderRadius: 8, padding: 24, margin: 0 }}
      >
        <h3 style={{ marginTop: 0, color: 'var(--text)' }}>Create a company workspace</h3>
        <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
          A company workspace has its own projects, members, teams, AI providers and worker keys.
          You will be its admin.
        </p>
        <form onSubmit={submit}>
          <div className="form-group">
            <label style={{ fontSize: 12 }}>Company name</label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Acme Robotics"
              autoFocus
            />
          </div>
          {error && (
            <div style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 12 }}>{String(error)}</div>
          )}
          <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
            <button
              type="button"
              className="button-secondary button"
              style={{ width: 'auto' }}
              onClick={onClose}
              disabled={busy}
            >
              Cancel
            </button>
            <button type="submit" className="button" style={{ width: 'auto' }} disabled={busy || !name.trim()}>
              {busy ? 'Creating…' : 'Create workspace'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};

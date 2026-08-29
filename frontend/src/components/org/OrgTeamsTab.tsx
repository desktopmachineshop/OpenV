import React, { useCallback, useEffect, useState } from 'react';
import { Org, OrgMember, OrgTeam, orgTeamsAPI, orgsAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { ErrorBanner, useConfirm, usePrompt } from '../ui';

interface OrgTeamsTabProps {
  org: Org;
  isAdmin: boolean;
}

// People-teams within a workspace: groups of workspace members that can be
// granted access to projects as a unit.
export const OrgTeamsTab: React.FC<OrgTeamsTabProps> = ({ org, isAdmin }) => {
  const confirm = useConfirm();
  const prompt = usePrompt();
  const [teams, setTeams] = useState<OrgTeam[]>([]);
  const [orgMembers, setOrgMembers] = useState<OrgMember[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  // Create form
  const [showCreate, setShowCreate] = useState(false);
  const [newName, setNewName] = useState('');
  const [newDesc, setNewDesc] = useState('');
  const [creating, setCreating] = useState(false);

  // Per-team add-member selection
  const [addSelection, setAddSelection] = useState<Record<string, string>>({});

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [teamsRes, membersRes] = await Promise.all([
        orgTeamsAPI.list(org.id),
        orgsAPI.members.list(org.id),
      ]);
      setTeams(teamsRes.data || []);
      setOrgMembers(membersRes.data || []);
    } catch (err: any) {
      setError(`Failed to load teams: ${apiErrorMessage(err)}`);
    } finally {
      setLoading(false);
    }
  }, [org.id]);

  useEffect(() => {
    load();
  }, [load]);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newName.trim()) return;
    setCreating(true);
    setError('');
    try {
      await orgTeamsAPI.create(org.id, { name: newName.trim(), description: newDesc.trim() });
      setNewName('');
      setNewDesc('');
      setShowCreate(false);
      await load();
    } catch (err: any) {
      setError(`Failed to create team: ${apiErrorMessage(err)}`);
    } finally {
      setCreating(false);
    }
  };

  const handleRename = async (team: OrgTeam) => {
    const name = await prompt({
      title: 'Rename team',
      label: 'Team name',
      defaultValue: team.name,
    });
    if (!name || !name.trim() || name.trim() === team.name) return;
    try {
      await orgTeamsAPI.update(team.id, { name: name.trim() });
      await load();
    } catch (err: any) {
      setError(`Failed to rename team: ${apiErrorMessage(err)}`);
    }
  };

  const handleDelete = async (team: OrgTeam) => {
    const ok = await confirm({
      title: 'Delete team',
      message: `Delete the team "${team.name}"? Its project access grants are removed too.`,
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) return;
    try {
      await orgTeamsAPI.remove(team.id);
      setTeams(teams.filter((t) => t.id !== team.id));
      setError('');
    } catch (err: any) {
      setError(`Failed to delete team: ${apiErrorMessage(err)}`);
    }
  };

  const handleAddMember = async (team: OrgTeam) => {
    const userId = addSelection[team.id];
    if (!userId) return;
    try {
      await orgTeamsAPI.addMember(team.id, userId);
      setAddSelection({ ...addSelection, [team.id]: '' });
      await load();
    } catch (err: any) {
      setError(`Failed to add member to team: ${apiErrorMessage(err)}`);
    }
  };

  const handleRemoveMember = async (team: OrgTeam, member: OrgMember) => {
    const label = member.user_name || member.user_email || 'this member';
    const ok = await confirm({
      title: 'Remove team member',
      message: `Remove ${label} from "${team.name}"?`,
      confirmLabel: 'Remove',
      danger: true,
    });
    if (!ok) return;
    try {
      await orgTeamsAPI.removeMember(team.id, member.user_id);
      await load();
    } catch (err: any) {
      setError(`Failed to remove member from team: ${apiErrorMessage(err)}`);
    }
  };

  const candidatesFor = (team: OrgTeam): OrgMember[] => {
    const inTeam = new Set((team.members || []).map((m) => m.user_id));
    return orgMembers.filter((m) => !inTeam.has(m.user_id));
  };

  return (
    <>
      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />

      <div className="card">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 }}>
          <h3 style={{ marginBottom: 0 }}>Teams</h3>
          {isAdmin && (
            <button
              className="button-secondary"
              style={{ padding: '6px 14px', background: 'var(--accent)' }}
              onClick={() => setShowCreate(!showCreate)}
            >
              {showCreate ? 'Cancel' : '+ New team'}
            </button>
          )}
        </div>
        <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
          Teams group workspace members so you can grant project access to everyone at once from a
          project's Access settings.
        </p>

        {isAdmin && showCreate && (
          <form onSubmit={handleCreate} style={{ background: 'var(--surface-alt)', borderRadius: 4, padding: 14, marginBottom: 14 }}>
            <div className="form-group">
              <label>Name *</label>
              <input
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="e.g. Firmware"
                autoFocus
              />
            </div>
            <div className="form-group">
              <label>Description</label>
              <input
                value={newDesc}
                onChange={(e) => setNewDesc(e.target.value)}
                placeholder="What this team works on (optional)"
              />
            </div>
            <button type="submit" className="button" disabled={creating || !newName.trim()}>
              {creating ? 'Creating…' : 'Create team'}
            </button>
          </form>
        )}

        {loading ? (
          <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading teams…</div>
        ) : teams.length === 0 ? (
          <div style={{ color: 'var(--neutral)', fontSize: 13 }}>No teams yet.</div>
        ) : (
          teams.map((team) => {
            const isOpen = !!expanded[team.id];
            const candidates = candidatesFor(team);
            return (
              <div
                key={team.id}
                style={{ border: '1px solid var(--border-soft)', borderRadius: 4, marginBottom: 8, overflow: 'hidden' }}
              >
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                    padding: '10px 12px',
                    cursor: 'pointer',
                    background: isOpen ? 'var(--surface-alt)' : 'var(--surface)',
                  }}
                  onClick={() => setExpanded({ ...expanded, [team.id]: !isOpen })}
                >
                  <span style={{ fontSize: 11, color: 'var(--text-muted)', width: 12 }}>{isOpen ? '▼' : '▶'}</span>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <span style={{ fontWeight: 600, fontSize: 14, color: 'var(--text)' }}>{team.name}</span>
                    {team.description && (
                      <span style={{ fontSize: 12, color: 'var(--text-muted)', marginLeft: 10 }}>{team.description}</span>
                    )}
                  </div>
                  <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                    {(team.members || []).length} member{(team.members || []).length === 1 ? '' : 's'}
                  </span>
                  {isAdmin && (
                    <>
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          handleRename(team);
                        }}
                        style={{ background: 'none', border: 'none', color: 'var(--accent)', cursor: 'pointer', fontSize: 12, width: 'auto', padding: 2 }}
                      >
                        Rename
                      </button>
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          handleDelete(team);
                        }}
                        style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 12, width: 'auto', padding: 2 }}
                      >
                        Delete
                      </button>
                    </>
                  )}
                </div>
                {isOpen && (
                  <div style={{ padding: '10px 12px', borderTop: '1px solid var(--border-soft)' }}>
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginBottom: isAdmin ? 12 : 0 }}>
                      {(team.members || []).length === 0 && (
                        <span style={{ fontSize: 13, color: 'var(--neutral)' }}>No members in this team.</span>
                      )}
                      {(team.members || []).map((m) => (
                        <span
                          key={m.user_id}
                          style={{
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: 6,
                            padding: '3px 10px',
                            borderRadius: 12,
                            background: 'var(--neutral-soft)',
                            fontSize: 12,
                            color: 'var(--text)',
                          }}
                        >
                          {m.user_name || m.user_email}
                          {isAdmin && (
                            <button
                              onClick={() => handleRemoveMember(team, m)}
                              title="Remove from team"
                              style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 11, width: 'auto', padding: 0 }}
                            >
                              ✕
                            </button>
                          )}
                        </span>
                      ))}
                    </div>
                    {isAdmin && (
                      <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                        <select
                          value={addSelection[team.id] || ''}
                          onChange={(e) => setAddSelection({ ...addSelection, [team.id]: e.target.value })}
                          style={{ padding: '5px 8px', fontSize: 13, width: 'auto', minWidth: 200 }}
                          disabled={candidates.length === 0}
                        >
                          <option value="">
                            {candidates.length === 0 ? 'Everyone is already in this team' : '-- Add a member --'}
                          </option>
                          {candidates.map((m) => (
                            <option key={m.user_id} value={m.user_id}>
                              {m.user_name || m.user_email}
                            </option>
                          ))}
                        </select>
                        <button
                          className="button"
                          style={{ padding: '6px 14px', width: 'auto' }}
                          disabled={!addSelection[team.id]}
                          onClick={() => handleAddMember(team)}
                        >
                          Add
                        </button>
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
    </>
  );
};

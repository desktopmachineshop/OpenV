import React, { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  membersAPI,
  orgTeamsAPI,
  projectAPI,
  projectTeamAccessAPI,
  repoConnectionsAPI,
  OrgTeam,
  Project,
  ProjectMember,
  RepoConnection,
  TeamGrant,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';

type Tab = 'members' | 'repos' | 'agents' | 'danger';

const TABS: { key: Tab; label: string }[] = [
  { key: 'members', label: 'Access' },
  { key: 'repos', label: 'Repositories' },
  { key: 'agents', label: 'Agents' },
  { key: 'danger', label: 'Danger Zone' },
];

const th: React.CSSProperties = {
  textAlign: 'left',
  fontSize: 12,
  color: '#7f8c8d',
  padding: '8px 10px',
  borderBottom: '1px solid #eee',
};

const td: React.CSSProperties = {
  padding: '8px 10px',
  fontSize: 13,
  color: '#2c3e50',
  borderBottom: '1px solid #f5f5f5',
};

interface RepoForm {
  id: string;
  name: string;
  remote_url: string;
  default_branch: string;
}

const emptyRepoForm: RepoForm = { id: '', name: '', remote_url: '', default_branch: 'main' };

export const ProjectSettings: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const navigate = useNavigate();
  const currentUser = useAppStore((s) => s.currentUser);
  const activeOrgId = useAppStore((s) => s.activeOrgId);

  const [tab, setTab] = useState<Tab>('members');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  // Members
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [membersLoading, setMembersLoading] = useState(true);
  const [addEmail, setAddEmail] = useState('');
  const [addRole, setAddRole] = useState('editor');
  const [addingMember, setAddingMember] = useState(false);

  // Team access
  const [teamGrants, setTeamGrants] = useState<TeamGrant[]>([]);
  const [teamGrantsLoading, setTeamGrantsLoading] = useState(true);
  const [orgTeams, setOrgTeams] = useState<OrgTeam[]>([]);
  const [grantTeamId, setGrantTeamId] = useState('');
  const [grantRole, setGrantRole] = useState('editor');
  const [granting, setGranting] = useState(false);

  // Repositories
  const [repos, setRepos] = useState<RepoConnection[]>([]);
  const [reposLoading, setReposLoading] = useState(true);
  const [repoForm, setRepoForm] = useState<RepoForm>(emptyRepoForm);
  const [showRepoForm, setShowRepoForm] = useState(false);
  const [savingRepo, setSavingRepo] = useState(false);
  // Per-user local path drafts, keyed by repo connection id.
  const [myPaths, setMyPaths] = useState<Record<string, string>>({});
  const [savingMyPath, setSavingMyPath] = useState('');

  // Agents (per-project agent auth)
  const [project, setProject] = useState<Project | null>(null);
  const [savingAuth, setSavingAuth] = useState(false);

  // Danger
  const [deleting, setDeleting] = useState(false);

  const flash = (msg: string) => {
    setNotice(msg);
    window.setTimeout(() => setNotice(''), 2500);
  };

  const loadMembers = useCallback(async () => {
    if (!projectId) return;
    setMembersLoading(true);
    try {
      const res = await membersAPI.list(projectId);
      setMembers(res.data || []);
    } catch (err: any) {
      setError(`Failed to load members: ${apiErrorMessage(err)}`);
    } finally {
      setMembersLoading(false);
    }
  }, [projectId]);

  const loadRepos = useCallback(async () => {
    if (!projectId) return;
    setReposLoading(true);
    try {
      const res = await repoConnectionsAPI.list(projectId);
      const list = res.data || [];
      setRepos(list);
      setMyPaths(Object.fromEntries(list.map((r) => [r.id, r.my_local_path || ''])));
    } catch (err: any) {
      setError(`Failed to load repositories: ${apiErrorMessage(err)}`);
    } finally {
      setReposLoading(false);
    }
  }, [projectId]);

  const loadProject = useCallback(async () => {
    if (!projectId) return;
    try {
      const res = await projectAPI.get(projectId);
      setProject(res.data);
    } catch {
      // Non-fatal: the Agents tab just shows a loading state.
    }
  }, [projectId]);

  const loadTeamAccess = useCallback(async () => {
    if (!projectId) return;
    setTeamGrantsLoading(true);
    try {
      const res = await projectTeamAccessAPI.list(projectId);
      setTeamGrants(res.data || []);
    } catch (err: any) {
      setError(`Failed to load team access: ${apiErrorMessage(err)}`);
    } finally {
      setTeamGrantsLoading(false);
    }
  }, [projectId]);

  const loadOrgTeams = useCallback(async () => {
    if (!activeOrgId) {
      setOrgTeams([]);
      return;
    }
    try {
      const res = await orgTeamsAPI.list(activeOrgId);
      setOrgTeams(res.data || []);
    } catch {
      // Non-fatal: the add-team dropdown is simply empty.
      setOrgTeams([]);
    }
  }, [activeOrgId]);

  useEffect(() => {
    loadMembers();
    loadRepos();
    loadTeamAccess();
    loadOrgTeams();
    loadProject();
  }, [loadMembers, loadRepos, loadTeamAccess, loadOrgTeams, loadProject]);

  // -------------------------------------------------------------------------
  // Members handlers
  // -------------------------------------------------------------------------

  const handleAddMember = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!projectId || !addEmail.trim()) return;
    setAddingMember(true);
    setError('');
    try {
      await membersAPI.add(projectId, addEmail.trim(), addRole);
      setAddEmail('');
      flash('Member added.');
      await loadMembers();
    } catch (err: any) {
      if (err.response?.status === 404) {
        setError(
          `No account exists for "${addEmail.trim()}". They need to sign up first — once they have an account, add them here by the same email.`
        );
      } else {
        setError(`Failed to add member: ${apiErrorMessage(err)}`);
      }
    } finally {
      setAddingMember(false);
    }
  };

  const handleSetRole = async (member: ProjectMember, role: string) => {
    if (!projectId) return;
    try {
      await membersAPI.setRole(projectId, member.user_id, role);
      setMembers(members.map((m) => (m.user_id === member.user_id ? { ...m, role: role as ProjectMember['role'] } : m)));
      setError('');
    } catch (err: any) {
      setError(`Failed to change role: ${apiErrorMessage(err)}`);
    }
  };

  const handleRemoveMember = async (member: ProjectMember) => {
    if (!projectId) return;
    const label = member.user_name || member.user_email || 'this member';
    if (!window.confirm(`Remove ${label} from the project?`)) return;
    try {
      await membersAPI.remove(projectId, member.user_id);
      setMembers(members.filter((m) => m.user_id !== member.user_id));
      setError('');
    } catch (err: any) {
      setError(`Failed to remove member: ${apiErrorMessage(err)}`);
    }
  };

  // -------------------------------------------------------------------------
  // Repository handlers
  // -------------------------------------------------------------------------

  const handleSaveRepo = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!projectId || !repoForm.name.trim() || !repoForm.remote_url.trim()) return;
    setSavingRepo(true);
    setError('');
    try {
      const payload = {
        name: repoForm.name.trim(),
        remote_url: repoForm.remote_url.trim(),
        default_branch: repoForm.default_branch.trim() || 'main',
      };
      if (repoForm.id) {
        await repoConnectionsAPI.update(repoForm.id, payload);
        flash('Repository updated.');
      } else {
        await repoConnectionsAPI.create(projectId, payload);
        flash('Repository connected.');
      }
      setRepoForm(emptyRepoForm);
      setShowRepoForm(false);
      await loadRepos();
    } catch (err: any) {
      setError(`Failed to save repository: ${apiErrorMessage(err)}`);
    } finally {
      setSavingRepo(false);
    }
  };

  const handleDeleteRepo = async (repo: RepoConnection) => {
    if (!window.confirm(`Remove repository connection "${repo.name}"?`)) return;
    try {
      await repoConnectionsAPI.remove(repo.id);
      setRepos(repos.filter((r) => r.id !== repo.id));
      setError('');
    } catch (err: any) {
      setError(`Failed to remove repository: ${apiErrorMessage(err)}`);
    }
  };

  const handleSaveMyPath = async (repo: RepoConnection) => {
    setSavingMyPath(repo.id);
    setError('');
    try {
      const res = await repoConnectionsAPI.setMyPath(repo.id, (myPaths[repo.id] || '').trim());
      setRepos(repos.map((r) => (r.id === repo.id ? { ...r, my_local_path: res.data.my_local_path } : r)));
      flash(res.data.my_local_path ? 'Your local path saved.' : 'Your local path cleared.');
    } catch (err: any) {
      setError(`Failed to save your local path: ${apiErrorMessage(err)}`);
    } finally {
      setSavingMyPath('');
    }
  };

  const handleSetAgentAuth = async (mode: 'user-account' | 'api-key') => {
    if (!projectId || !project) return;
    setSavingAuth(true);
    setError('');
    try {
      const res = await projectAPI.update(projectId, { agent_auth: mode });
      setProject(res.data);
      flash('Agent authentication updated.');
    } catch (err: any) {
      setError(`Failed to update agent authentication: ${apiErrorMessage(err)}`);
    } finally {
      setSavingAuth(false);
    }
  };

  // -------------------------------------------------------------------------
  // Team access handlers
  // -------------------------------------------------------------------------

  const handleGrantTeam = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!projectId || !grantTeamId) return;
    setGranting(true);
    setError('');
    try {
      await projectTeamAccessAPI.grant(projectId, grantTeamId, grantRole);
      setGrantTeamId('');
      flash('Team access granted.');
      await loadTeamAccess();
    } catch (err: any) {
      setError(`Failed to grant team access: ${apiErrorMessage(err)}`);
    } finally {
      setGranting(false);
    }
  };

  const handleSetTeamRole = async (grant: TeamGrant, role: string) => {
    if (!projectId) return;
    try {
      await projectTeamAccessAPI.grant(projectId, grant.org_team_id, role);
      setTeamGrants(
        teamGrants.map((g) => (g.org_team_id === grant.org_team_id ? { ...g, role } : g))
      );
      setError('');
    } catch (err: any) {
      setError(`Failed to change team role: ${apiErrorMessage(err)}`);
    }
  };

  const handleRevokeTeam = async (grant: TeamGrant) => {
    if (!projectId) return;
    if (!window.confirm(`Revoke "${grant.team_name}" access to this project?`)) return;
    try {
      await projectTeamAccessAPI.revoke(projectId, grant.org_team_id);
      setTeamGrants(teamGrants.filter((g) => g.org_team_id !== grant.org_team_id));
      setError('');
    } catch (err: any) {
      setError(`Failed to revoke team access: ${apiErrorMessage(err)}`);
    }
  };

  const grantableTeams = orgTeams.filter(
    (t) => !teamGrants.some((g) => g.org_team_id === t.id)
  );

  // -------------------------------------------------------------------------
  // Danger zone
  // -------------------------------------------------------------------------

  const handleDeleteProject = async () => {
    if (!projectId) return;
    if (!window.confirm('Delete this project and ALL of its artifacts, links and history? This cannot be undone.')) return;
    if (!window.confirm('Really delete? This is permanent.')) return;
    setDeleting(true);
    try {
      await projectAPI.delete(projectId);
      navigate('/projects');
    } catch (err: any) {
      setError(`Failed to delete project: ${apiErrorMessage(err)}`);
      setDeleting(false);
    }
  };

  // -------------------------------------------------------------------------

  return (
    <div style={{ padding: 24, maxWidth: 900, margin: '0 auto' }}>
      <h2 style={{ color: '#2c3e50', marginBottom: 16 }}>Project settings</h2>

      <div style={{ display: 'flex', gap: 4, borderBottom: '2px solid #ddd', marginBottom: 20 }}>
        {TABS.map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            style={{
              background: 'none',
              border: 'none',
              borderBottom: tab === t.key ? '2px solid #3498db' : '2px solid transparent',
              marginBottom: -2,
              padding: '10px 16px',
              fontSize: 14,
              fontWeight: tab === t.key ? 600 : 400,
              color: tab === t.key ? '#2c3e50' : t.key === 'danger' ? '#e74c3c' : '#7f8c8d',
              cursor: 'pointer',
              width: 'auto',
            }}
          >
            {t.label}
          </button>
        ))}
      </div>

      {error && (
        <div
          style={{
            background: '#fdecea',
            border: '1px solid #e74c3c',
            color: '#c0392b',
            padding: '10px 14px',
            borderRadius: 4,
            marginBottom: 16,
            fontSize: 13,
          }}
        >
          {error}{' '}
          <button
            onClick={() => setError('')}
            style={{ background: 'none', border: 'none', color: '#c0392b', cursor: 'pointer', width: 'auto', padding: 0 }}
          >
            ✕
          </button>
        </div>
      )}
      {notice && (
        <div
          style={{
            background: '#eafaf1',
            border: '1px solid #27ae60',
            color: '#1e8449',
            padding: '10px 14px',
            borderRadius: 4,
            marginBottom: 16,
            fontSize: 13,
          }}
        >
          {notice}
        </div>
      )}

      {tab === 'members' && (
        <>
          <div className="card">
            <h3>People</h3>
            {membersLoading ? (
              <div style={{ color: '#7f8c8d', fontSize: 13 }}>Loading members…</div>
            ) : (
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    <th style={th}>Member</th>
                    <th style={th}>Email</th>
                    <th style={{ ...th, width: 140 }}>Role</th>
                    <th style={{ ...th, width: 80 }}></th>
                  </tr>
                </thead>
                <tbody>
                  {members.map((m) => (
                    <tr key={m.user_id}>
                      <td style={td}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          {m.avatar_url ? (
                            <img src={m.avatar_url} alt="" style={{ width: 28, height: 28, borderRadius: '50%' }} />
                          ) : (
                            <div
                              style={{
                                width: 28,
                                height: 28,
                                borderRadius: '50%',
                                background: '#3498db',
                                color: '#fff',
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                fontSize: 13,
                                fontWeight: 700,
                              }}
                            >
                              {(m.user_name || m.user_email || '?').charAt(0).toUpperCase()}
                            </div>
                          )}
                          <span>
                            {m.user_name || '—'}
                            {currentUser && m.user_id === currentUser.id && (
                              <span style={{ color: '#7f8c8d', fontSize: 12 }}> (you)</span>
                            )}
                          </span>
                        </div>
                      </td>
                      <td style={td}>{m.user_email || '—'}</td>
                      <td style={td}>
                        <select
                          value={m.role}
                          onChange={(e) => handleSetRole(m, e.target.value)}
                          style={{ padding: '5px 8px', fontSize: 13 }}
                        >
                          <option value="owner">owner</option>
                          <option value="editor">editor</option>
                          <option value="viewer">viewer</option>
                        </select>
                      </td>
                      <td style={{ ...td, textAlign: 'right' }}>
                        <button
                          onClick={() => handleRemoveMember(m)}
                          style={{ background: 'none', border: 'none', color: '#e74c3c', cursor: 'pointer', fontSize: 12, width: 'auto', padding: 2 }}
                        >
                          Remove
                        </button>
                      </td>
                    </tr>
                  ))}
                  {members.length === 0 && (
                    <tr>
                      <td style={{ ...td, color: '#95a5a6' }} colSpan={4}>
                        No members found.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            )}
          </div>

          <div className="card">
            <h3>Add member</h3>
            <p style={{ fontSize: 13, color: '#7f8c8d' }}>
              The person must already have an OpenV account — invite them to sign up first, then add
              their email here.
            </p>
            <form onSubmit={handleAddMember} style={{ display: 'flex', gap: 10, alignItems: 'flex-end', flexWrap: 'wrap' }}>
              <div style={{ flex: 1, minWidth: 220 }}>
                <label style={{ fontSize: 12 }}>Email</label>
                <input
                  type="email"
                  value={addEmail}
                  onChange={(e) => setAddEmail(e.target.value)}
                  placeholder="teammate@example.com"
                />
              </div>
              <div style={{ width: 130 }}>
                <label style={{ fontSize: 12 }}>Role</label>
                <select value={addRole} onChange={(e) => setAddRole(e.target.value)}>
                  <option value="owner">owner</option>
                  <option value="editor">editor</option>
                  <option value="viewer">viewer</option>
                </select>
              </div>
              <button type="submit" className="button" disabled={addingMember || !addEmail.trim()}>
                {addingMember ? 'Adding…' : 'Add member'}
              </button>
            </form>
          </div>

          <div className="card">
            <h3>Teams</h3>
            <p style={{ fontSize: 13, color: '#7f8c8d' }}>
              Grant a workspace team access to this project. Manage teams themselves in{' '}
              <Link to="/org/settings">Workspace settings</Link>.
            </p>
            {teamGrantsLoading ? (
              <div style={{ color: '#7f8c8d', fontSize: 13 }}>Loading team access…</div>
            ) : (
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    <th style={th}>Team</th>
                    <th style={{ ...th, width: 140 }}>Role</th>
                    <th style={{ ...th, width: 80 }}></th>
                  </tr>
                </thead>
                <tbody>
                  {teamGrants.map((g) => (
                    <tr key={g.org_team_id}>
                      <td style={{ ...td, fontWeight: 600 }}>{g.team_name}</td>
                      <td style={td}>
                        <select
                          value={g.role}
                          onChange={(e) => handleSetTeamRole(g, e.target.value)}
                          style={{ padding: '5px 8px', fontSize: 13 }}
                        >
                          <option value="owner">owner</option>
                          <option value="editor">editor</option>
                          <option value="viewer">viewer</option>
                        </select>
                      </td>
                      <td style={{ ...td, textAlign: 'right' }}>
                        <button
                          onClick={() => handleRevokeTeam(g)}
                          style={{ background: 'none', border: 'none', color: '#e74c3c', cursor: 'pointer', fontSize: 12, width: 'auto', padding: 2 }}
                        >
                          Revoke
                        </button>
                      </td>
                    </tr>
                  ))}
                  {teamGrants.length === 0 && (
                    <tr>
                      <td style={{ ...td, color: '#95a5a6' }} colSpan={3}>
                        No teams have access to this project yet.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            )}

            <form
              onSubmit={handleGrantTeam}
              style={{ display: 'flex', gap: 10, alignItems: 'flex-end', flexWrap: 'wrap', marginTop: 14 }}
            >
              <div style={{ flex: 1, minWidth: 220 }}>
                <label style={{ fontSize: 12 }}>Team</label>
                <select
                  value={grantTeamId}
                  onChange={(e) => setGrantTeamId(e.target.value)}
                  disabled={grantableTeams.length === 0}
                >
                  <option value="">
                    {grantableTeams.length === 0
                      ? 'No workspace teams left to add'
                      : '-- Select a team --'}
                  </option>
                  {grantableTeams.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name}
                    </option>
                  ))}
                </select>
              </div>
              <div style={{ width: 130 }}>
                <label style={{ fontSize: 12 }}>Role</label>
                <select value={grantRole} onChange={(e) => setGrantRole(e.target.value)}>
                  <option value="owner">owner</option>
                  <option value="editor">editor</option>
                  <option value="viewer">viewer</option>
                </select>
              </div>
              <button type="submit" className="button" disabled={granting || !grantTeamId}>
                {granting ? 'Granting…' : 'Grant access'}
              </button>
            </form>

            <p style={{ fontSize: 12, color: '#7f8c8d', marginTop: 14, marginBottom: 0 }}>
              Workspace admins always have owner access. A person's effective role is the highest of
              their direct grant and any team grants.
            </p>
          </div>
        </>
      )}

      {tab === 'repos' && (
        <>
          <div className="card">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 }}>
              <h3 style={{ marginBottom: 0 }}>Repository connections</h3>
              <button
                className="button-secondary"
                style={{ padding: '6px 14px', background: '#3498db' }}
                onClick={() => {
                  setRepoForm(emptyRepoForm);
                  setShowRepoForm(!showRepoForm);
                }}
              >
                {showRepoForm ? 'Cancel' : '+ Connect repository'}
              </button>
            </div>
            <p style={{ fontSize: 13, color: '#7f8c8d' }}>
              Repositories give coding agents somewhere to work. The connection identifies the
              GitHub repository; each member points it at their own clone below. Credentials come
              from the host machine's git configuration.
            </p>

            {showRepoForm && (
              <form onSubmit={handleSaveRepo} style={{ background: '#f8f9fa', borderRadius: 4, padding: 14, marginBottom: 14 }}>
                <div className="form-group">
                  <label>Name *</label>
                  <input
                    value={repoForm.name}
                    onChange={(e) => setRepoForm({ ...repoForm, name: e.target.value })}
                    placeholder="e.g. main-app"
                    autoFocus
                  />
                </div>
                <div className="form-group">
                  <label>Repository URL *</label>
                  <input
                    value={repoForm.remote_url}
                    onChange={(e) => setRepoForm({ ...repoForm, remote_url: e.target.value })}
                    placeholder="https://github.com/org/repo.git"
                  />
                </div>
                <div className="form-group">
                  <label>Default branch</label>
                  <input
                    value={repoForm.default_branch}
                    onChange={(e) => setRepoForm({ ...repoForm, default_branch: e.target.value })}
                    placeholder="main"
                  />
                </div>
                <button
                  type="submit"
                  className="button"
                  disabled={savingRepo || !repoForm.name.trim() || !repoForm.remote_url.trim()}
                >
                  {savingRepo ? 'Saving…' : repoForm.id ? 'Update repository' : 'Connect repository'}
                </button>
              </form>
            )}

            {reposLoading ? (
              <div style={{ color: '#7f8c8d', fontSize: 13 }}>Loading repositories…</div>
            ) : repos.length === 0 ? (
              <div style={{ color: '#95a5a6', fontSize: 13 }}>No repositories connected yet.</div>
            ) : (
              repos.map((r) => (
                <div
                  key={r.id}
                  style={{
                    border: '1px solid #eee',
                    borderRadius: 4,
                    padding: '10px 12px',
                    marginBottom: 8,
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ fontWeight: 600, fontSize: 14, color: '#2c3e50' }}>{r.name}</div>
                      <div style={{ fontSize: 12, color: '#7f8c8d', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                        {r.remote_url || 'no repository URL — edit to add one'}
                        {r.default_branch ? ` · ${r.default_branch}` : ''}
                      </div>
                    </div>
                    <button
                      className="button-secondary"
                      style={{ padding: '5px 12px', fontSize: 12 }}
                      onClick={() => {
                        setRepoForm({
                          id: r.id,
                          name: r.name,
                          remote_url: r.remote_url,
                          default_branch: r.default_branch || 'main',
                        });
                        setShowRepoForm(true);
                      }}
                    >
                      Edit
                    </button>
                    <button
                      onClick={() => handleDeleteRepo(r)}
                      style={{ background: 'none', border: 'none', color: '#e74c3c', cursor: 'pointer', fontSize: 12, width: 'auto', padding: 2 }}
                    >
                      Remove
                    </button>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 8 }}>
                    <label style={{ fontSize: 12, color: '#7f8c8d', whiteSpace: 'nowrap', marginBottom: 0 }}>
                      Your local path
                    </label>
                    <input
                      value={myPaths[r.id] ?? ''}
                      onChange={(e) => setMyPaths({ ...myPaths, [r.id]: e.target.value })}
                      placeholder="where this repo lives on YOUR machine"
                      style={{ flex: 1, padding: '5px 8px', fontSize: 12 }}
                    />
                    <button
                      className="button-secondary"
                      style={{ padding: '5px 12px', fontSize: 12, width: 'auto' }}
                      onClick={() => handleSaveMyPath(r)}
                      disabled={savingMyPath === r.id || (myPaths[r.id] ?? '') === (r.my_local_path || '')}
                    >
                      {savingMyPath === r.id ? 'Saving…' : 'Save'}
                    </button>
                  </div>
                </div>
              ))
            )}
            {!reposLoading && repos.length > 0 && (
              <p style={{ fontSize: 12, color: '#7f8c8d', marginTop: 10, marginBottom: 0 }}>
                Agents run on each member's own machine, so “your local path” tells your runner
                where this repo lives for <b>you</b>. Without one set, your runs clone the remote
                URL instead.
              </p>
            )}
          </div>

          <div className="card" style={{ background: '#eaf4fc', border: '1px solid #3498db' }}>
            <p style={{ fontSize: 13, color: '#2c3e50', marginBottom: 0 }}>
              AI providers &amp; worker keys moved →{' '}
              <Link to="/org/settings">Workspace settings</Link>. They now apply to the whole
              workspace, not a single project.
            </p>
          </div>
        </>
      )}

      {tab === 'agents' && (
        <div className="card">
          <h3>Agent authentication</h3>
          <p style={{ fontSize: 13, color: '#7f8c8d' }}>
            How agent runs in this project authenticate with their AI provider. This only picks the
            credential and routing — sign-ins themselves live in each member's user settings (the
            user menu, bottom left).
          </p>
          {!project ? (
            <div style={{ color: '#7f8c8d', fontSize: 13 }}>Loading project…</div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              <label
                style={{
                  display: 'flex',
                  gap: 10,
                  alignItems: 'flex-start',
                  border: '1px solid',
                  borderColor: project.agent_auth !== 'api-key' ? '#3498db' : '#eee',
                  borderRadius: 4,
                  padding: '10px 12px',
                  cursor: 'pointer',
                  marginBottom: 0,
                  fontWeight: 400,
                }}
              >
                <input
                  type="radio"
                  name="agent-auth"
                  style={{ width: 'auto', marginTop: 3 }}
                  checked={project.agent_auth !== 'api-key'}
                  disabled={savingAuth}
                  onChange={() => handleSetAgentAuth('user-account')}
                />
                <span style={{ fontSize: 13, color: '#2c3e50' }}>
                  <b>User account</b>
                  <br />
                  <span style={{ color: '#7f8c8d', fontSize: 12 }}>
                    Runs use each member's own CLI sign-in on their machine (set up in user
                    settings). No sign-in happens here — this just routes runs to the launcher's
                    local login.
                  </span>
                </span>
              </label>
              <label
                style={{
                  display: 'flex',
                  gap: 10,
                  alignItems: 'flex-start',
                  border: '1px solid',
                  borderColor: project.agent_auth === 'api-key' ? '#3498db' : '#eee',
                  borderRadius: 4,
                  padding: '10px 12px',
                  cursor: 'pointer',
                  marginBottom: 0,
                  fontWeight: 400,
                }}
              >
                <input
                  type="radio"
                  name="agent-auth"
                  style={{ width: 'auto', marginTop: 3 }}
                  checked={project.agent_auth === 'api-key'}
                  disabled={savingAuth}
                  onChange={() => handleSetAgentAuth('api-key')}
                />
                <span style={{ fontSize: 13, color: '#2c3e50' }}>
                  <b>API key</b>
                  <br />
                  <span style={{ color: '#7f8c8d', fontSize: 12 }}>
                    Overrides members' local sign-ins: runs use the workspace's API key (the
                    provider's key environment variable, configured in Workspace settings → AI
                    providers, set on the runner host). For now runs still execute through each
                    member's OpenV connector.
                  </span>
                </span>
              </label>
            </div>
          )}
        </div>
      )}

      {tab === 'danger' && (
        <div className="card" style={{ border: '1px solid #e74c3c' }}>
          <h3 style={{ color: '#e74c3c' }}>Danger zone</h3>
          <p style={{ fontSize: 13, color: '#7f8c8d' }}>
            Deleting the project permanently removes all artifacts, links, baselines, test runs, work
            items and interview data. This cannot be undone.
          </p>
          <button
            onClick={handleDeleteProject}
            disabled={deleting}
            style={{
              background: '#e74c3c',
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
      )}
    </div>
  );
};

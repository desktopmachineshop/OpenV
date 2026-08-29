import React, { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import {
  attributeDefinitionAPI,
  membersAPI,
  metaAPI,
  orgTeamsAPI,
  projectAPI,
  projectTeamAccessAPI,
  repoConnectionsAPI,
  ArtifactTypeDef,
  AttributeDataType,
  AttributeDefinition,
  OrgTeam,
  Project,
  ProjectMember,
  RepoConnection,
  TeamGrant,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { ErrorBanner, useConfirm } from '../components/ui';

type Tab = 'members' | 'repos' | 'agents' | 'attributes' | 'danger';

const TABS: { key: Tab; label: string }[] = [
  { key: 'members', label: 'Access' },
  { key: 'repos', label: 'Repositories' },
  { key: 'agents', label: 'Agents' },
  { key: 'attributes', label: 'Attributes' },
  { key: 'danger', label: 'Danger Zone' },
];

const ATTRIBUTE_DATA_TYPES: AttributeDataType[] = ['text', 'number', 'date', 'enum', 'boolean'];

interface AttributeForm {
  key: string;
  label: string;
  data_type: AttributeDataType;
  enum_values: string;
  applies_to_type: string;
  required: boolean;
}

const emptyAttributeForm: AttributeForm = {
  key: '',
  label: '',
  data_type: 'text',
  enum_values: '',
  applies_to_type: '',
  required: false,
};

const th: React.CSSProperties = {
  textAlign: 'left',
  fontSize: 12,
  color: 'var(--text-muted)',
  padding: '8px 10px',
  borderBottom: '1px solid var(--border-soft)',
};

const td: React.CSSProperties = {
  padding: '8px 10px',
  fontSize: 13,
  color: 'var(--text)',
  borderBottom: '1px solid var(--surface-inset)',
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
  const confirm = useConfirm();

  // The active tab lives in the URL (?tab=…) so refreshes and deep links keep
  // it; unknown values fall back to the first tab.
  const [searchParams, setSearchParams] = useSearchParams();
  const tabParam = searchParams.get('tab');
  const tab: Tab = TABS.some((t) => t.key === tabParam) ? (tabParam as Tab) : TABS[0].key;
  const setTab = (next: Tab) =>
    setSearchParams(
      (prev) => {
        prev.set('tab', next);
        return prev;
      },
      { replace: true }
    );
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

  // Attribute definitions (issue #219): project-scoped typed attributes.
  const [attrDefs, setAttrDefs] = useState<AttributeDefinition[]>([]);
  const [attrDefsLoading, setAttrDefsLoading] = useState(true);
  const [attrForm, setAttrForm] = useState<AttributeForm>(emptyAttributeForm);
  const [savingAttr, setSavingAttr] = useState(false);
  const [artifactTypes, setArtifactTypes] = useState<ArtifactTypeDef[]>([]);

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

  const loadAttrDefs = useCallback(async () => {
    if (!projectId) return;
    setAttrDefsLoading(true);
    try {
      const res = await attributeDefinitionAPI.listByProject(projectId);
      setAttrDefs(res.data || []);
    } catch (err: any) {
      setError(`Failed to load attribute definitions: ${apiErrorMessage(err)}`);
    } finally {
      setAttrDefsLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    loadMembers();
    loadRepos();
    loadTeamAccess();
    loadOrgTeams();
    loadProject();
    loadAttrDefs();
    metaAPI
      .artifactTypes()
      .then((res) => setArtifactTypes(res.data || []))
      .catch(() => setArtifactTypes([]));
  }, [loadMembers, loadRepos, loadTeamAccess, loadOrgTeams, loadProject, loadAttrDefs]);

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
    const ok = await confirm({
      title: 'Remove member',
      message: `Remove ${label} from the project?`,
      confirmLabel: 'Remove',
      danger: true,
    });
    if (!ok) return;
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
    const ok = await confirm({
      title: 'Remove repository',
      message: `Remove repository connection "${repo.name}"?`,
      confirmLabel: 'Remove',
      danger: true,
    });
    if (!ok) return;
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
  // Attribute definition handlers (issue #219)
  // -------------------------------------------------------------------------

  const handleAddAttribute = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!projectId || !attrForm.key.trim()) return;
    setSavingAttr(true);
    setError('');
    try {
      await attributeDefinitionAPI.create({
        project_id: projectId,
        key: attrForm.key.trim(),
        label: attrForm.label.trim() || attrForm.key.trim(),
        data_type: attrForm.data_type,
        enum_values:
          attrForm.data_type === 'enum'
            ? attrForm.enum_values
                .split(',')
                .map((v) => v.trim())
                .filter(Boolean)
            : [],
        applies_to_type: attrForm.applies_to_type,
        required: attrForm.required,
      });
      setAttrForm(emptyAttributeForm);
      await loadAttrDefs();
      flash('Attribute added.');
    } catch (err: any) {
      setError(`Failed to add attribute: ${apiErrorMessage(err)}`);
    } finally {
      setSavingAttr(false);
    }
  };

  const handleDeleteAttribute = async (def: AttributeDefinition) => {
    const ok = await confirm({
      title: 'Delete attribute',
      message: `Delete the "${def.label || def.key}" attribute definition? Values already stored on artifacts are left untouched.`,
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) return;
    setError('');
    try {
      await attributeDefinitionAPI.remove(def.id);
      await loadAttrDefs();
      flash('Attribute deleted.');
    } catch (err: any) {
      setError(`Failed to delete attribute: ${apiErrorMessage(err)}`);
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
    const ok = await confirm({
      title: 'Revoke team access',
      message: `Revoke "${grant.team_name}" access to this project?`,
      confirmLabel: 'Revoke',
      danger: true,
    });
    if (!ok) return;
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
    const first = await confirm({
      title: 'Delete project',
      message: 'Delete this project and ALL of its artifacts, links and history? This cannot be undone.',
      confirmLabel: 'Delete project',
      danger: true,
    });
    if (!first) return;
    const second = await confirm({
      title: 'Delete project',
      message: 'Really delete? This is permanent.',
      confirmLabel: 'Yes, delete permanently',
      danger: true,
    });
    if (!second) return;
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
      <h2 style={{ color: 'var(--text)', marginBottom: 16 }}>Project settings</h2>

      <div style={{ display: 'flex', gap: 4, borderBottom: '2px solid var(--border)', marginBottom: 20 }}>
        {TABS.map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            style={{
              background: 'none',
              border: 'none',
              borderBottom: tab === t.key ? '2px solid var(--accent)' : '2px solid transparent',
              marginBottom: -2,
              padding: '10px 16px',
              fontSize: 14,
              fontWeight: tab === t.key ? 600 : 400,
              color: tab === t.key ? 'var(--text)' : t.key === 'danger' ? 'var(--danger)' : 'var(--text-muted)',
              cursor: 'pointer',
              width: 'auto',
            }}
          >
            {t.label}
          </button>
        ))}
      </div>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />
      {notice && (
        <div
          style={{
            background: 'var(--tint-green)',
            border: '1px solid var(--success)',
            color: 'var(--success-text)',
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
              <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading members…</div>
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
                                background: 'var(--accent)',
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
                              <span style={{ color: 'var(--text-muted)', fontSize: 12 }}> (you)</span>
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
                          style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 12, width: 'auto', padding: 2 }}
                        >
                          Remove
                        </button>
                      </td>
                    </tr>
                  ))}
                  {members.length === 0 && (
                    <tr>
                      <td style={{ ...td, color: 'var(--neutral)' }} colSpan={4}>
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
            <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
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
            <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
              Grant a workspace team access to this project. Manage teams themselves in{' '}
              <Link to="/org/settings">Workspace settings</Link>.
            </p>
            {teamGrantsLoading ? (
              <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading team access…</div>
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
                          style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 12, width: 'auto', padding: 2 }}
                        >
                          Revoke
                        </button>
                      </td>
                    </tr>
                  ))}
                  {teamGrants.length === 0 && (
                    <tr>
                      <td style={{ ...td, color: 'var(--neutral)' }} colSpan={3}>
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

            <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 14, marginBottom: 0 }}>
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
                style={{ padding: '6px 14px', background: 'var(--accent)' }}
                onClick={() => {
                  setRepoForm(emptyRepoForm);
                  setShowRepoForm(!showRepoForm);
                }}
              >
                {showRepoForm ? 'Cancel' : '+ Connect repository'}
              </button>
            </div>
            <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
              Repositories give coding agents somewhere to work. The connection identifies the
              GitHub repository; each member points it at their own clone below. Credentials come
              from the host machine's git configuration.
            </p>

            {showRepoForm && (
              <form onSubmit={handleSaveRepo} style={{ background: 'var(--surface-alt)', borderRadius: 4, padding: 14, marginBottom: 14 }}>
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
              <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading repositories…</div>
            ) : repos.length === 0 ? (
              <div style={{ color: 'var(--neutral)', fontSize: 13 }}>No repositories connected yet.</div>
            ) : (
              repos.map((r) => (
                <div
                  key={r.id}
                  style={{
                    border: '1px solid var(--border-soft)',
                    borderRadius: 4,
                    padding: '10px 12px',
                    marginBottom: 8,
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--text)' }}>{r.name}</div>
                      <div style={{ fontSize: 12, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis' }}>
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
                      style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 12, width: 'auto', padding: 2 }}
                    >
                      Remove
                    </button>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 8 }}>
                    <label style={{ fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap', marginBottom: 0 }}>
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
              <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 10, marginBottom: 0 }}>
                Agents run on each member's own machine, so “your local path” tells your runner
                where this repo lives for <b>you</b>. Without one set, your runs clone the remote
                URL instead.
              </p>
            )}
          </div>

          <div className="card" style={{ background: 'var(--tint-blue)', border: '1px solid var(--accent)' }}>
            <p style={{ fontSize: 13, color: 'var(--text)', marginBottom: 0 }}>
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
          <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            How agent runs in this project authenticate with their AI provider. This only picks the
            credential and routing — sign-ins themselves live in each member's user settings (the
            user menu, bottom left).
          </p>
          {!project ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading project…</div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              <label
                style={{
                  display: 'flex',
                  gap: 10,
                  alignItems: 'flex-start',
                  border: '1px solid',
                  borderColor: project.agent_auth !== 'api-key' ? 'var(--accent)' : 'var(--border-soft)',
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
                <span style={{ fontSize: 13, color: 'var(--text)' }}>
                  <b>User account</b>
                  <br />
                  <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
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
                  borderColor: project.agent_auth === 'api-key' ? 'var(--accent)' : 'var(--border-soft)',
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
                <span style={{ fontSize: 13, color: 'var(--text)' }}>
                  <b>API key</b>
                  <br />
                  <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
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

      {tab === 'attributes' && (
        <div className="card">
          <h3>Custom attributes</h3>
          <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            Define extra typed fields for this project's artifacts. They appear as inputs in the
            artifact editor and are validated on save. Project attributes override a workspace-wide
            attribute with the same key and type.
          </p>

          {attrDefsLoading ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>
          ) : attrDefs.length === 0 ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 12 }}>
              No project attributes defined yet.
            </div>
          ) : (
            <table style={{ width: '100%', borderCollapse: 'collapse', marginBottom: 16 }}>
              <thead>
                <tr>
                  <th style={th}>Key</th>
                  <th style={th}>Label</th>
                  <th style={th}>Type</th>
                  <th style={th}>Applies to</th>
                  <th style={th}>Required</th>
                  <th style={th}></th>
                </tr>
              </thead>
              <tbody>
                {attrDefs.map((def) => (
                  <tr key={def.id}>
                    <td style={td}>
                      <code>{def.key}</code>
                    </td>
                    <td style={td}>{def.label}</td>
                    <td style={td}>
                      {def.data_type}
                      {def.data_type === 'enum' && def.enum_values.length > 0 && (
                        <span style={{ color: 'var(--text-muted)' }}> ({def.enum_values.join(', ')})</span>
                      )}
                    </td>
                    <td style={td}>{def.applies_to_type || 'All types'}</td>
                    <td style={td}>{def.required ? 'Yes' : 'No'}</td>
                    <td style={{ ...td, textAlign: 'right' }}>
                      <button
                        className="button-secondary"
                        style={{ padding: '4px 8px', fontSize: 12, color: 'var(--danger)' }}
                        onClick={() => handleDeleteAttribute(def)}
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          <form onSubmit={handleAddAttribute} style={{ display: 'flex', flexWrap: 'wrap', gap: 10, alignItems: 'flex-end' }}>
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label htmlFor="attr-key">Key</label>
              <input
                id="attr-key"
                type="text"
                value={attrForm.key}
                onChange={(e) => setAttrForm((f) => ({ ...f, key: e.target.value }))}
                placeholder="priority"
                required
              />
            </div>
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label htmlFor="attr-label">Label</label>
              <input
                id="attr-label"
                type="text"
                value={attrForm.label}
                onChange={(e) => setAttrForm((f) => ({ ...f, label: e.target.value }))}
                placeholder="Priority"
              />
            </div>
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label htmlFor="attr-type">Type</label>
              <select
                id="attr-type"
                value={attrForm.data_type}
                onChange={(e) => setAttrForm((f) => ({ ...f, data_type: e.target.value as AttributeDataType }))}
              >
                {ATTRIBUTE_DATA_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
            </div>
            {attrForm.data_type === 'enum' && (
              <div className="form-group" style={{ marginBottom: 0 }}>
                <label htmlFor="attr-enum">Options (comma-separated)</label>
                <input
                  id="attr-enum"
                  type="text"
                  value={attrForm.enum_values}
                  onChange={(e) => setAttrForm((f) => ({ ...f, enum_values: e.target.value }))}
                  placeholder="low, medium, high"
                />
              </div>
            )}
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label htmlFor="attr-applies">Applies to</label>
              <select
                id="attr-applies"
                value={attrForm.applies_to_type}
                onChange={(e) => setAttrForm((f) => ({ ...f, applies_to_type: e.target.value }))}
              >
                <option value="">All types</option>
                {artifactTypes.map((t) => (
                  <option key={t.value} value={t.value}>
                    {t.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="form-group" style={{ marginBottom: 0, display: 'flex', alignItems: 'center', gap: 6 }}>
              <input
                id="attr-required"
                type="checkbox"
                checked={attrForm.required}
                onChange={(e) => setAttrForm((f) => ({ ...f, required: e.target.checked }))}
                style={{ width: 'auto' }}
              />
              <label htmlFor="attr-required" style={{ marginBottom: 0 }}>
                Required
              </label>
            </div>
            <button type="submit" className="button" disabled={savingAttr || !attrForm.key.trim()}>
              {savingAttr ? 'Adding…' : 'Add attribute'}
            </button>
          </form>
        </div>
      )}

      {tab === 'danger' && (
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
      )}
    </div>
  );
};

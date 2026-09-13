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
  shareLinkAPI,
  ArtifactTypeDef,
  AttributeDataType,
  AttributeDefinition,
  OrgTeam,
  Project,
  ProjectMember,
  RepoConnection,
  ShareLink,
  ShareLinkRole,
  TeamGrant,
  Party,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { ErrorBanner, useConfirm } from '../components/ui';
import { QualityRulesEditor } from '../components/QualityRulesEditor';
import { Avatar } from '../components/Avatar';
import { useFeature } from '../hooks/useFeature';

type Tab = 'general' | 'members' | 'repos' | 'agents' | 'attributes' | 'quality' | 'danger';

const TABS: { key: Tab; label: string }[] = [
  { key: 'general', label: 'General' },
  { key: 'members', label: 'Access' },
  { key: 'repos', label: 'Repositories' },
  { key: 'agents', label: 'Agents' },
  { key: 'attributes', label: 'Attributes' },
  { key: 'quality', label: 'Quality rules' },
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
  // Both reach stable-channel workspaces at their next stable release.
  const flowDown = useFeature('flow-down');
  const ownersOn = useFeature('artifact-owners');
  const shareLinksOn = useFeature('share-links');

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
  // Editing quality rules needs project editor rights. Platform admins and
  // members with no row here (workspace admins reach every project) fall
  // through to the server's own check, which is the authority.
  const myRole = members.find((m) => m.user_id === currentUser?.id)?.role;
  const canEditRules = !myRole || myRole !== 'viewer' || Boolean(currentUser?.is_admin);
  const [membersLoading, setMembersLoading] = useState(true);
  const [addEmail, setAddEmail] = useState('');
  const [addRole, setAddRole] = useState('editor');
  const [addingMember, setAddingMember] = useState(false);

  // Share links (REQ-149)
  const [shareLinks, setShareLinks] = useState<ShareLink[]>([]);
  const [shareLinksLoading, setShareLinksLoading] = useState(true);
  const [shareRole, setShareRole] = useState<ShareLinkRole>('public');
  const [shareLabel, setShareLabel] = useState('');
  const [shareExpires, setShareExpires] = useState('');
  const [sharing, setSharing] = useState(false);
  // The link just minted, shown once: the token is stored hashed.
  const [freshLink, setFreshLink] = useState<ShareLink | null>(null);
  const [copied, setCopied] = useState(false);

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

  // General (REQ-144, REQ-147): the parent project and the reference parties.
  const [allProjects, setAllProjects] = useState<Project[]>([]);
  const [savingParent, setSavingParent] = useState(false);
  const [parties, setParties] = useState<Party[]>([]);
  const [partyName, setPartyName] = useState('');
  const [partyNote, setPartyNote] = useState('');
  const [savingParties, setSavingParties] = useState(false);

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
    // The other projects of the workspace, for the parent picker and the
    // list of child projects; the parties this project recognises as owners.
    try {
      const res = await projectAPI.list();
      setAllProjects(res.data || []);
    } catch {
      setAllProjects([]);
    }
    try {
      const res = await projectAPI.parties(projectId);
      setParties(res.data?.parties || []);
    } catch {
      setParties([]);
    }
  }, [projectId]);

  // Projects that cannot be this one's parent: itself and everything filed
  // under it, since placing it under a descendant would close a loop.
  const descendantIds = (() => {
    const out = new Set<string>([projectId || '']);
    let grew = true;
    while (grew) {
      grew = false;
      for (const p of allProjects) {
        if (p.parent_project_id && out.has(p.parent_project_id) && !out.has(p.id)) {
          out.add(p.id);
          grew = true;
        }
      }
    }
    return out;
  })();
  const parentCandidates = allProjects.filter(
    (p) => !descendantIds.has(p.id) && (!project || p.org_id === project.org_id)
  );
  const childProjects = allProjects.filter((p) => p.parent_project_id === projectId);

  const handleSetParent = async (parentId: string) => {
    if (!projectId || !project) return;
    setSavingParent(true);
    setError('');
    try {
      const res = await projectAPI.update(projectId, { parent_project_id: parentId });
      setProject(res.data);
      flash(parentId ? 'Parent project set.' : 'Parent project cleared.');
    } catch (err: any) {
      setError(`Failed to set the parent project: ${apiErrorMessage(err)}`);
    } finally {
      setSavingParent(false);
    }
  };

  const saveParties = async (next: Party[]) => {
    if (!projectId) return;
    setSavingParties(true);
    setError('');
    try {
      const res = await projectAPI.setParties(
        projectId,
        next.filter((p) => !p.default)
      );
      setParties(res.data?.parties || []);
      flash('Parties saved.');
    } catch (err: any) {
      setError(`Failed to save parties: ${apiErrorMessage(err)}`);
    } finally {
      setSavingParties(false);
    }
  };

  const handleAddParty = async (e: React.FormEvent) => {
    e.preventDefault();
    const name = partyName.trim();
    if (!name) return;
    await saveParties([...parties, { name, note: partyNote.trim() }]);
    setPartyName('');
    setPartyNote('');
  };

  const loadShareLinks = useCallback(async () => {
    if (!projectId) return;
    setShareLinksLoading(true);
    try {
      const res = await shareLinkAPI.list(projectId);
      setShareLinks(res.data || []);
    } catch (err: any) {
      // Only an owner may list links; anybody else sees the section empty.
      if (err?.response?.status !== 403) setError(`Failed to load share links: ${apiErrorMessage(err)}`);
      setShareLinks([]);
    } finally {
      setShareLinksLoading(false);
    }
  }, [projectId]);

  const handleCreateShareLink = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!projectId) return;
    setSharing(true);
    setError('');
    setCopied(false);
    try {
      const expiresAt = shareExpires ? new Date(shareExpires + 'T23:59:59').toISOString() : undefined;
      const res = await shareLinkAPI.create(projectId, shareRole, shareLabel.trim(), expiresAt);
      setFreshLink(res.data);
      setShareLabel('');
      setShareExpires('');
      await loadShareLinks();
    } catch (err: any) {
      setError(`Failed to create the share link: ${apiErrorMessage(err)}`);
    } finally {
      setSharing(false);
    }
  };

  const handleRevokeShareLink = async (link: ShareLink) => {
    const ok = await confirm({
      title: 'Revoke share link',
      message: `Anyone holding "${link.label || link.role}" will lose access. Revoke it?`,
      confirmLabel: 'Revoke',
    });
    if (!ok) return;
    setError('');
    try {
      await shareLinkAPI.revoke(link.id);
      if (freshLink?.id === link.id) setFreshLink(null);
      await loadShareLinks();
    } catch (err: any) {
      setError(`Failed to revoke the share link: ${apiErrorMessage(err)}`);
    }
  };

  const copyFreshLink = async () => {
    if (!freshLink?.url) return;
    try {
      await navigator.clipboard.writeText(freshLink.url);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

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
    loadShareLinks();
    loadOrgTeams();
    loadProject();
    loadAttrDefs();
    metaAPI
      .artifactTypes()
      .then((res) => setArtifactTypes(res.data || []))
      .catch(() => setArtifactTypes([]));
  }, [loadMembers, loadRepos, loadTeamAccess, loadShareLinks, loadOrgTeams, loadProject, loadAttrDefs]);

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

      <div className="tab-strip" role="tablist">
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

      {tab === 'general' && !flowDown && !ownersOn && (
        <div className="card">
          <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: 0 }}>
            Parent projects and reference parties reach stable-channel workspaces at their next
            stable release. Switch the workspace to nightly, or preview the next release, in
            workspace settings to use them now.
          </p>
        </div>
      )}

      {tab === 'general' && flowDown && (
          <div className="card">
            <h3>Parent project</h3>
            <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
              A subsystem or supplier project sits under the system it belongs to. Requirements
              here can then <em>refine</em> the parent's requirements, and the parent's V&amp;V
              rolls those refinements up. A supplier works in the child project with editor
              rights there and viewer rights on the parent.
            </p>
            {!project ? (
              <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading project…</div>
            ) : (
              <div className="form-group" style={{ maxWidth: 420 }}>
                <label htmlFor="parent-project">This project refines</label>
                <select
                  id="parent-project"
                  value={project.parent_project_id || ''}
                  disabled={savingParent}
                  onChange={(e) => handleSetParent(e.target.value)}
                >
                  <option value="">None (top-level project)</option>
                  {parentCandidates.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              </div>
            )}
            {childProjects.length > 0 && (
              <div style={{ fontSize: 13, color: 'var(--text)' }}>
                <div style={{ color: 'var(--text-muted)', marginBottom: 4 }}>Child projects</div>
                <ul style={{ margin: 0, paddingLeft: '1.2em' }}>
                  {childProjects.map((p) => (
                    <li key={p.id}>
                      <Link to={`/projects/${p.id}/settings?tab=general`}>{p.name}</Link>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
      )}

      {tab === 'general' && ownersOn && (
          <div className="card">
            <h3>Reference parties</h3>
            <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
              Who can own an artifact here besides the members: the organisations, teams and
              suppliers this project works with. The workspace's own company is always first.
              An artifact's owner is picked from this list and the members, and a download can be
              narrowed to one owner's share of the project.
            </p>
            <table style={{ width: '100%', borderCollapse: 'collapse', marginBottom: 12 }}>
              <thead>
                <tr>
                  <th style={th}>Party</th>
                  <th style={th}>Note</th>
                  <th style={th} />
                </tr>
              </thead>
              <tbody>
                {parties.map((p) => (
                  <tr key={p.name}>
                    <td style={td}>
                      {p.name}
                      {p.default && (
                        <span style={{ marginLeft: 8, fontSize: 11, color: 'var(--text-muted)' }}>
                          this workspace
                        </span>
                      )}
                    </td>
                    <td style={td}>{p.note || ''}</td>
                    <td style={{ ...td, textAlign: 'right' }}>
                      {!p.default && (
                        <button
                          type="button"
                          className="btn-secondary"
                          style={{ width: 'auto', padding: '4px 10px', fontSize: 12 }}
                          disabled={savingParties}
                          onClick={() => saveParties(parties.filter((q) => q.name !== p.name))}
                        >
                          Remove
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <form onSubmit={handleAddParty} style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              <input
                placeholder="Party name, e.g. Landing gear supplier"
                value={partyName}
                onChange={(e) => setPartyName(e.target.value)}
                style={{ flex: '1 1 200px' }}
              />
              <input
                placeholder="Note (optional)"
                value={partyNote}
                onChange={(e) => setPartyNote(e.target.value)}
                style={{ flex: '2 1 240px' }}
              />
              <button type="submit" className="btn-primary" style={{ width: 'auto' }} disabled={savingParties || !partyName.trim()}>
                Add party
              </button>
            </form>
          </div>
      )}

      {tab === 'members' && (
        <>
          <div className="card">
            <h3>People</h3>
            {membersLoading ? (
              <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading members…</div>
            ) : (
              <div className="table-scroll">
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
                          <Avatar src={m.avatar_url} name={m.user_name || m.user_email} />
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
                          <option value="reviewer">reviewer</option>
                          <option value="viewer">viewer</option>
                        </select>
                      </td>
                      <td style={{ ...td, textAlign: 'right' }}>
                        <button
                          onClick={() => handleRemoveMember(m)}
                          style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 13, width: 'auto', padding: '6px 8px', minHeight: 36 }}
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
              </div>
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
                  <option value="reviewer">reviewer</option>
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
              <div className="table-scroll">
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
                          <option value="reviewer">reviewer</option>
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
              </div>
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
                  <option value="reviewer">reviewer</option>
                  <option value="viewer">viewer</option>
                </select>
              </div>
              <button type="submit" className="button" disabled={granting || !grantTeamId}>
                {granting ? 'Granting…' : 'Grant access'}
              </button>
            </form>

            <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 14, marginBottom: 0 }}>
              Workspace admins always have owner access. A person's effective role is the highest of
              their direct grant and any team grants. A reviewer reads everything and adds notes,
              comments and mentions, but cannot change the text.
            </p>
          </div>

          {shareLinksOn && (
          <div className="card" style={{ marginTop: 16 }}>
            <h3>Share links</h3>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 0 }}>
              A <strong>public</strong> link opens the live project, read only, for anyone who holds
              it, with no account. A <strong>reviewer</strong> link asks the holder to sign in and
              makes them a reviewer. Links unfurl with a preview when pasted into Slack, Discord,
              LinkedIn and the like. Revoke a link to close it.
            </p>
            {freshLink?.url && (
              <div
                style={{
                  border: '1px solid var(--accent)',
                  borderRadius: 6,
                  padding: 12,
                  marginBottom: 14,
                  background: 'var(--accent-soft, rgba(44,142,240,0.08))',
                }}
              >
                <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 6 }}>
                  Your {freshLink.role} link is ready. Copy it now: it is not shown again.
                </div>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
                  <input
                    readOnly
                    value={freshLink.url}
                    onFocus={(e) => e.currentTarget.select()}
                    style={{ flex: 1, minWidth: 220, fontSize: 12 }}
                    aria-label="Share link"
                  />
                  <button type="button" className="button" onClick={copyFreshLink} style={{ width: 'auto' }}>
                    {copied ? 'Copied' : 'Copy link'}
                  </button>
                </div>
              </div>
            )}
            <form
              onSubmit={handleCreateShareLink}
              style={{ display: 'flex', gap: 10, alignItems: 'flex-end', flexWrap: 'wrap', marginBottom: 14 }}
            >
              <div style={{ width: 150 }}>
                <label style={{ fontSize: 12 }}>Access</label>
                <select value={shareRole} onChange={(e) => setShareRole(e.target.value as ShareLinkRole)}>
                  <option value="public">public, view only</option>
                  <option value="reviewer">reviewer</option>
                </select>
              </div>
              <div style={{ flex: 1, minWidth: 160 }}>
                <label style={{ fontSize: 12 }}>Label</label>
                <input
                  value={shareLabel}
                  onChange={(e) => setShareLabel(e.target.value)}
                  placeholder="Who this link is for"
                />
              </div>
              <div style={{ width: 160 }}>
                <label style={{ fontSize: 12 }}>Expires (optional)</label>
                <input type="date" value={shareExpires} onChange={(e) => setShareExpires(e.target.value)} />
              </div>
              <button type="submit" className="button" disabled={sharing} style={{ width: 'auto' }}>
                {sharing ? 'Creating…' : 'Create link'}
              </button>
            </form>
            {shareLinksLoading ? (
              <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading share links…</div>
            ) : shareLinks.length === 0 ? (
              <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>No share links yet.</div>
            ) : (
              <div className="table-scroll">
                <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                  <thead>
                    <tr>
                      <th style={th}>Label</th>
                      <th style={{ ...th, width: 100 }}>Access</th>
                      <th style={{ ...th, width: 120 }}>Created</th>
                      <th style={{ ...th, width: 120 }}>Expires</th>
                      <th style={{ ...th, width: 90 }}></th>
                    </tr>
                  </thead>
                  <tbody>
                    {shareLinks.map((l) => {
                      const revoked = !!l.revoked_at;
                      const expired = !!l.expires_at && new Date(l.expires_at) < new Date();
                      return (
                        <tr key={l.id} style={{ opacity: revoked || expired ? 0.55 : 1 }}>
                          <td style={td}>{l.label || '—'}</td>
                          <td style={td}>{l.role}</td>
                          <td style={td}>{new Date(l.created_at).toLocaleDateString()}</td>
                          <td style={td}>
                            {revoked
                              ? 'revoked'
                              : l.expires_at
                                ? `${expired ? 'expired ' : ''}${new Date(l.expires_at).toLocaleDateString()}`
                                : 'never'}
                          </td>
                          <td style={{ ...td, textAlign: 'right' }}>
                            {!revoked && (
                              <button
                                onClick={() => handleRevokeShareLink(l)}
                                style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 13, width: 'auto', padding: '6px 8px', minHeight: 36 }}
                              >
                                Revoke
                              </button>
                            )}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </div>
          )}
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
                      style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 13, width: 'auto', padding: '6px 8px', minHeight: 36 }}
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
            <div className="table-scroll">
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
            </div>
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

      {tab === 'quality' && (
        <div className="card">
          <h3>Requirement quality rules</h3>
          <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            How this project's requirements are worded and judged. Set nothing and the project
            follows its workspace's house style (Workspace settings → Quality rules); override only
            what this project needs to do differently. Agents read these rules before they draft,
            and the quality badge scores against them.
          </p>
          {projectId && (
            <QualityRulesEditor
              level="project"
              id={projectId}
              canEdit={canEditRules}
              onSaved={() => flash('Quality rules saved')}
            />
          )}
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

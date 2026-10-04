import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
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
import { useFeature } from '../hooks/useFeature';
import { QualityRulesHeld, emptyQualityRulesHeld } from '../components/QualityRulesEditor';
import { AttributeForm, RepoForm, emptyAttributeForm, emptyRepoForm } from './projectSettings/shared';
import { GeneralTab } from './projectSettings/GeneralTab';
import { MembersTab } from './projectSettings/MembersTab';
import { ReposTab } from './projectSettings/ReposTab';
import { AgentsTab } from './projectSettings/AgentsTab';
import { AttributesTab } from './projectSettings/AttributesTab';
import { QualityTab } from './projectSettings/QualityTab';
import { DangerTab } from './projectSettings/DangerTab';

type Tab = 'general' | 'members' | 'repos' | 'agents' | 'attributes' | 'quality' | 'danger';

// The WAI-ARIA tabs pattern: each tab names the one panel, which is labelled
// by the open tab. No other tab strip in the app moves on arrow keys, so
// these keep the browser's Tab order.
const tabId = (key: Tab) => `project-settings-tab-${key}`;
const PANEL_ID = 'project-settings-panel';

const TABS: { key: Tab; label: string }[] = [
  { key: 'general', label: 'General' },
  { key: 'members', label: 'Access' },
  { key: 'repos', label: 'Repositories' },
  { key: 'agents', label: 'Agents' },
  { key: 'attributes', label: 'Attributes' },
  { key: 'quality', label: 'Quality rules' },
  { key: 'danger', label: 'Danger Zone' },
];

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

  // Quality rules: the editor's rules and unsaved draft, loaded on the
  // tab's first visit.
  const [qualityRules, setQualityRules] = useState<QualityRulesHeld>(emptyQualityRulesHeld);

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
    loadOrgTeams();
    loadProject();
    loadAttrDefs();
    metaAPI
      .artifactTypes()
      .then((res) => setArtifactTypes(res.data || []))
      .catch(() => setArtifactTypes([]));
  }, [loadMembers, loadRepos, loadTeamAccess, loadOrgTeams, loadProject, loadAttrDefs]);

  // Share links only exist behind their feature key, so nothing is asked for
  // while it is off; the gates can arrive after the page, so this follows it.
  useEffect(() => {
    if (shareLinksOn) loadShareLinks();
  }, [shareLinksOn, loadShareLinks]);

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
            role="tab"
            id={tabId(t.key)}
            aria-selected={tab === t.key}
            aria-controls={PANEL_ID}
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

      <div role="tabpanel" id={PANEL_ID} aria-labelledby={tabId(tab)}>
        {tab === 'general' && (
          <GeneralTab
            flowDown={flowDown}
            ownersOn={ownersOn}
            project={project}
            parentCandidates={parentCandidates}
            childProjects={childProjects}
            savingParent={savingParent}
            handleSetParent={handleSetParent}
            parties={parties}
            savingParties={savingParties}
            saveParties={saveParties}
            handleAddParty={handleAddParty}
            partyName={partyName}
            setPartyName={setPartyName}
            partyNote={partyNote}
            setPartyNote={setPartyNote}
          />
        )}

        {tab === 'members' && (
          <MembersTab
            currentUser={currentUser}
            membersLoading={membersLoading}
            members={members}
            handleSetRole={handleSetRole}
            handleRemoveMember={handleRemoveMember}
            handleAddMember={handleAddMember}
            addEmail={addEmail}
            setAddEmail={setAddEmail}
            addRole={addRole}
            setAddRole={setAddRole}
            addingMember={addingMember}
            teamGrantsLoading={teamGrantsLoading}
            teamGrants={teamGrants}
            handleSetTeamRole={handleSetTeamRole}
            handleRevokeTeam={handleRevokeTeam}
            handleGrantTeam={handleGrantTeam}
            grantableTeams={grantableTeams}
            grantTeamId={grantTeamId}
            setGrantTeamId={setGrantTeamId}
            grantRole={grantRole}
            setGrantRole={setGrantRole}
            granting={granting}
            shareLinksOn={shareLinksOn}
            shareLinksLoading={shareLinksLoading}
            shareLinks={shareLinks}
            handleRevokeShareLink={handleRevokeShareLink}
            freshLink={freshLink}
            copied={copied}
            copyFreshLink={copyFreshLink}
            handleCreateShareLink={handleCreateShareLink}
            shareRole={shareRole}
            setShareRole={setShareRole}
            shareLabel={shareLabel}
            setShareLabel={setShareLabel}
            shareExpires={shareExpires}
            setShareExpires={setShareExpires}
            sharing={sharing}
          />
        )}

        {tab === 'repos' && (
          <ReposTab
            reposLoading={reposLoading}
            repos={repos}
            repoForm={repoForm}
            setRepoForm={setRepoForm}
            showRepoForm={showRepoForm}
            setShowRepoForm={setShowRepoForm}
            savingRepo={savingRepo}
            handleSaveRepo={handleSaveRepo}
            handleDeleteRepo={handleDeleteRepo}
            myPaths={myPaths}
            setMyPaths={setMyPaths}
            savingMyPath={savingMyPath}
            handleSaveMyPath={handleSaveMyPath}
          />
        )}

        {tab === 'agents' && (
          <AgentsTab project={project} savingAuth={savingAuth} handleSetAgentAuth={handleSetAgentAuth} />
        )}

        {tab === 'attributes' && (
          <AttributesTab
            attrDefsLoading={attrDefsLoading}
            attrDefs={attrDefs}
            handleDeleteAttribute={handleDeleteAttribute}
            attrForm={attrForm}
            setAttrForm={setAttrForm}
            artifactTypes={artifactTypes}
            savingAttr={savingAttr}
            handleAddAttribute={handleAddAttribute}
          />
        )}

        {tab === 'quality' && (
          <QualityTab
            projectId={projectId}
            canEditRules={canEditRules}
            flash={flash}
            qualityRules={qualityRules}
            setQualityRules={setQualityRules}
          />
        )}

        {tab === 'danger' && <DangerTab deleting={deleting} handleDeleteProject={handleDeleteProject} />}
      </div>
    </div>
  );
};

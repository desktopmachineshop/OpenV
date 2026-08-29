import React, { useEffect, useState } from 'react';
import { NavLink, Outlet, useNavigate, useParams } from 'react-router-dom';
import { projectAPI, Project } from '../api/client';
import { useAppStore } from '../state/store';
import { GlobalSearch } from './GlobalSearch';
import { HelpSidebar } from './HelpSidebar';
import { NotificationBell } from './NotificationBell';
import { OrgSwitcher } from './OrgSwitcher';
import { UserMenu } from './UserMenu';

interface NavItem {
  to: string;
  label: string;
  end?: boolean;
}

// The sidebar is grouped by what the user is doing: defining the product,
// verifying it, planning the work, and running agents (issue #97).
const navSections: { label?: string; items: NavItem[] }[] = [
  {
    label: 'Define',
    items: [
      { to: '', label: 'Overview', end: true },
      { to: 'guided', label: 'Guided Definition' },
      { to: 'requirements', label: 'Requirements' },
      { to: 'interviews', label: 'Interviews' },
    ],
  },
  {
    label: 'Verify',
    items: [
      { to: 'vv', label: 'V&V' },
      { to: 'matrix', label: 'Traceability' },
      { to: 'review', label: 'Review Queue' },
    ],
  },
  {
    label: 'Plan',
    items: [{ to: 'board', label: 'Board' }],
  },
  {
    label: 'Agents',
    items: [
      { to: 'agents', label: 'Agents' },
      { to: 'crew', label: 'Crew' },
      { to: 'automations', label: 'Automations' },
      { to: 'agent-runs', label: 'Runs' },
    ],
  },
  {
    items: [
      { to: 'activity', label: 'Activity' },
      { to: 'settings', label: 'Settings' },
    ],
  },
];

// ProjectLayout syncs the URL param into the store and renders the app
// shell (left nav + top bar) around the active module.
export const ProjectLayout: React.FC = () => {
  const { projectId } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const { setProjectId, orgs, activeOrgId, setActiveOrgId } = useAppStore();
  const [project, setProject] = useState<Project | null>(null);

  useEffect(() => {
    if (!projectId) return;
    setProjectId(projectId);
    projectAPI
      .get(projectId)
      .then((res) => setProject(res.data))
      .catch(() => navigate('/projects'));
  }, [projectId, setProjectId, navigate]);

  // Deep link into a project from another workspace: silently switch the
  // active workspace when the project belongs to an org the user is in.
  // (If they aren't a member, the project fetch 403s and redirects above.)
  //
  // Child views can render (and fetch) before this switch resolves, so their
  // first requests may still carry the previous workspace's X-Org-ID header.
  // That is harmless for project-keyed endpoints — the backend authorizes
  // those against the project itself and ignores the header — but any fetch
  // of an org-scoped resource (agents, agent runs, automations, crews, org
  // teams, events) must include activeOrgId in its effect deps so it refetches
  // once the switch lands (issues #99/#111/#112).
  useEffect(() => {
    if (!project?.org_id || orgs.length === 0) return;
    if (project.org_id !== activeOrgId && orgs.some((o) => o.id === project.org_id)) {
      setActiveOrgId(project.org_id, { clearProjects: false });
    }
  }, [project, orgs, activeOrgId, setActiveOrgId]);

  return (
    <div style={{ display: 'flex', height: '100vh', overflow: 'hidden' }}>
      <aside
        style={{
          width: 200,
          minWidth: 200,
          background: 'var(--sidebar-bg)',
          color: 'var(--sidebar-text)',
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <div style={{ padding: '16px 14px', borderBottom: '1px solid var(--sidebar-border)' }}>
          <OrgSwitcher variant="dark" />
          <div
            style={{
              fontSize: 12,
              color: 'var(--sidebar-text-faint)',
              marginTop: 6,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              cursor: 'pointer',
            }}
            title="All projects"
            onClick={() => navigate('/projects')}
          >
            {project?.name || '…'}
          </div>
          <div style={{ marginTop: 8, display: 'flex', alignItems: 'center', gap: 6 }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <GlobalSearch />
            </div>
            <NotificationBell variant="dark" />
          </div>
        </div>
        <nav style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
          {navSections.map((section, i) => (
            <div key={section.label || `section-${i}`} style={{ marginTop: i === 0 ? 0 : 10 }}>
              {section.label && (
                <div
                  style={{
                    padding: '4px 16px',
                    fontSize: 10,
                    fontWeight: 700,
                    letterSpacing: 1,
                    textTransform: 'uppercase',
                    color: 'var(--sidebar-text-faint)',
                  }}
                >
                  {section.label}
                </div>
              )}
              {section.items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.end}
                  style={({ isActive }) => ({
                    display: 'block',
                    padding: '9px 16px',
                    color: isActive ? 'var(--accent-fg)' : 'var(--sidebar-text-dim)',
                    background: isActive ? 'var(--accent)' : 'transparent',
                    textDecoration: 'none',
                    fontSize: 14,
                  })}
                >
                  {item.label}
                </NavLink>
              ))}
            </div>
          ))}
          <div style={{ borderTop: '1px solid var(--sidebar-border)', margin: '8px 0' }} />
          {/* Plain anchor (not NavLink): the manual opens in a new tab so it
              doesn't navigate the user away from their work (issue #162). */}
          <a
            href="/manual"
            target="_blank"
            rel="noopener"
            title="Open the user manual in a new tab"
            style={{
              display: 'block',
              padding: '9px 16px',
              color: 'var(--sidebar-text-dim)',
              textDecoration: 'none',
              fontSize: 14,
            }}
          >
            Help
          </a>
        </nav>
        <div style={{ borderTop: '1px solid var(--sidebar-border)', padding: 12 }}>
          <UserMenu variant="dark" />
        </div>
      </aside>
      <main style={{ flex: 1, overflow: 'auto', background: 'var(--bg-app)' }}>
        <Outlet />
      </main>
      {/* Floating context-aware help — mounted once here so the ? button is
          available on every project page (issue #162). */}
      <HelpSidebar />
    </div>
  );
};

import React, { useEffect, useState } from 'react';
import { NavLink, Outlet, useLocation, useNavigate, useParams } from 'react-router-dom';
import { guidedAPI, projectAPI, Project } from '../api/client';
import { hasWizardProgress } from './wizard/assistantSession';
import {
  PanelMode,
  loadPanelMode,
  nextPanelMode,
  panelIsOpen,
  panelModeLabel,
  panelTakesSpace,
  savePanelMode,
} from './panelMode';
import {
  NavSectionState,
  activeNavPath,
  loadNavSections,
  navSectionIsOpen,
  saveNavSections,
  sectionHasActive,
  toggleNavSection,
} from './navSections';
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
      { to: 'impact', label: 'Impact' },
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
  const location = useLocation();
  const { setProjectId, orgs, activeOrgId, setActiveOrgId } = useAppStore();
  const [project, setProject] = useState<Project | null>(null);
  // Once a guided definition has been worked on, the nav entry disappears —
  // the wizard is then reached only through the Overview page's adaptive CTA
  // (Resume / Modify). Rechecked on every route change so starting or
  // committing a session updates the menu without a reload.
  //
  // "Worked on", not "exists": the V&V Assistant's conversation is stored on a
  // guided session, so opening the notes chat in a project that never ran the
  // wizard creates one. That must not take the wizard out of the nav, so a
  // session still on step one with nothing entered does not count.
  const [hasGuidedSession, setHasGuidedSession] = useState(false);
  // How much room the project menu takes: pinned, auto-hiding on hover, or
  // hidden. Remembered per person so the choice survives a reload.
  const [navMode, setNavMode] = useState<PanelMode>(() => loadPanelMode('project-nav'));
  const [navHovered, setNavHovered] = useState(false);
  // Which menu groups are expanded. They start collapsed — see navSections.ts.
  const [navOpenSections, setNavOpenSections] = useState<NavSectionState>(() => loadNavSections());

  useEffect(() => {
    if (!projectId) return;
    let cancelled = false;
    guidedAPI
      .list(projectId)
      .then((res) => {
        if (!cancelled) setHasGuidedSession(hasWizardProgress(res.data || []));
      })
      .catch(() => {
        /* menu visibility only — keep the last known state */
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, location.pathname]);

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

  const activePath = activeNavPath(location.pathname, projectId || '');
  const open = panelIsOpen(navMode, navHovered);
  const takesSpace = panelTakesSpace(navMode);

  return (
    <div style={{ display: 'flex', height: '100vh', overflow: 'hidden' }}>
      {/* The edge strip is what brings a hidden or auto-hiding nav back: with
          the nav gone there would otherwise be nothing left to click. */}
      {!takesSpace && (
        <div
          onMouseEnter={() => setNavHovered(true)}
          onMouseLeave={() => setNavHovered(false)}
          style={{
            width: 10,
            minWidth: 10,
            background: 'var(--sidebar-bg)',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'flex-start',
            justifyContent: 'center',
            paddingTop: 12,
            color: 'var(--sidebar-text-dim)',
            fontSize: 10,
          }}
          title={`Project menu: ${panelModeLabel(navMode)} — click the pin inside to change`}
          onClick={() => setNavHovered(true)}
        >
          ›
        </div>
      )}
      <aside
        onMouseEnter={() => navMode === 'autohide' && setNavHovered(true)}
        onMouseLeave={() => navMode === 'autohide' && setNavHovered(false)}
        style={{
          width: open ? 200 : 0,
          minWidth: open ? 200 : 0,
          background: 'var(--sidebar-bg)',
          color: 'var(--sidebar-text)',
          display: open ? 'flex' : 'none',
          flexDirection: 'column',
          // An auto-hiding nav floats over the document instead of reflowing
          // it every time the pointer crosses the edge.
          ...(takesSpace
            ? {}
            : { position: 'fixed', left: 10, top: 0, bottom: 0, zIndex: 900, boxShadow: '2px 0 8px rgba(0,0,0,0.25)' }),
        }}
      >
        <div style={{ padding: '16px 14px', borderBottom: '1px solid var(--sidebar-border)', flexShrink: 0 }}>
          <OrgSwitcher variant="dark" />
          {/* Explicit way back to the workspace's project list — the project
              name alone read as a label, not a link. */}
          <div
            style={{
              fontSize: 12,
              color: 'var(--sidebar-text-dim)',
              marginTop: 8,
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              gap: 6,
            }}
            title="Back to all projects in this workspace"
            onClick={() => navigate('/projects')}
          >
            <span aria-hidden>←</span> All projects
          </div>
          <div
            style={{
              fontSize: 13,
              fontWeight: 600,
              color: 'var(--sidebar-text)',
              marginTop: 6,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
            title={project?.name || ''}
          >
            {project?.name || '…'}
          </div>
          <div style={{ marginTop: 8 }}>
            <GlobalSearch />
          </div>
        </div>
        {/* minHeight: 0 is what lets this shrink below its content in the
            column, so a menu taller than the window scrolls here instead of
            pushing the account controls off the bottom. */}
        <nav style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '8px 0' }}>
          {navSections.map((section, i) => {
            const items = section.items.filter(
              (item) => item.to !== 'guided' || !hasGuidedSession
            );
            // An unlabelled group has no header to click, so it never collapses.
            const label = section.label;
            const expanded = label
              ? navSectionIsOpen(navOpenSections, label, sectionHasActive(items, activePath))
              : true;
            return (
              <div key={label || `section-${i}`} style={{ marginTop: i === 0 ? 0 : 10 }}>
                {label && (
                  <button
                    type="button"
                    aria-expanded={expanded}
                    onClick={() => {
                      const next = toggleNavSection(navOpenSections, label, expanded);
                      setNavOpenSections(next);
                      saveNavSections(next);
                    }}
                    title={`${expanded ? 'Collapse' : 'Expand'} ${label}`}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 6,
                      width: '100%',
                      padding: '6px 16px',
                      background: 'none',
                      border: 'none',
                      textAlign: 'left',
                      fontSize: 10,
                      fontWeight: 700,
                      letterSpacing: 1,
                      textTransform: 'uppercase',
                      color: 'var(--sidebar-text-faint)',
                      cursor: 'pointer',
                    }}
                  >
                    <span aria-hidden style={{ fontSize: 8, width: 8 }}>
                      {expanded ? '▾' : '▸'}
                    </span>
                    {label}
                  </button>
                )}
                {expanded &&
                  items.map((item) => (
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
            );
          })}
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
        <div style={{ borderTop: '1px solid var(--sidebar-border)', padding: 12, flexShrink: 0 }}>
          {/* The bell lives with the account controls: both are about the
              person using OpenV rather than the project they are in. */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <UserMenu variant="dark" />
            </div>
            <NotificationBell variant="dark" />
          </div>
          <button
            onClick={() => {
              const next = nextPanelMode(navMode);
              setNavMode(next);
              savePanelMode('project-nav', next);
              setNavHovered(false);
            }}
            title={`Project menu: ${panelModeLabel(navMode)} — click for ${panelModeLabel(
              nextPanelMode(navMode)
            )}`}
            style={{
              marginTop: 8,
              width: '100%',
              background: 'none',
              border: '1px solid var(--sidebar-border)',
              borderRadius: 4,
              color: 'var(--sidebar-text-dim)',
              fontSize: 11,
              padding: '4px 6px',
              cursor: 'pointer',
            }}
          >
            Menu: {panelModeLabel(navMode)}
          </button>
        </div>
      </aside>
      <main style={{ flex: 1, minWidth: 0, minHeight: 0, overflow: 'auto', background: 'var(--bg-app)' }}>
        <Outlet />
      </main>
      {/* Floating context-aware help — mounted once here so the ? button is
          available on every project page (issue #162). */}
      <HelpSidebar />
    </div>
  );
};

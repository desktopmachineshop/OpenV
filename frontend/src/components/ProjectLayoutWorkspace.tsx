import React, { useEffect } from 'react';
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom';
import { useAppStore } from '../state/store';
import { useFeature } from '../hooks/useFeature';
import { useViewport } from '../hooks/useViewport';
import { NavIcon, SHELL_ICONS, groupIconPath, pageIconPath } from './ProjectLayoutIcons';
import { REFRESH_GROUPS, loadLastPages } from './ProjectLayoutNav';
import { TAB_BAR_HEIGHT } from './ProjectLayoutTabs';
import { ActionMenu } from './ui';
import { lastProject } from '../appShortcuts';
import { useUiRefresh } from '../hooks/useUiRefresh';
import { TopBar } from './ProjectLayoutRail';
import { WORKSPACE_RUNS_FEATURE } from './agents/workspaceRuns';
import { TODOS_FEATURE } from '../features';
import { MY_TODOS_PATH } from './todos/todoText';
import { NotificationBell } from './NotificationBell';
import { OrgSwitcher } from './OrgSwitcher';
import { ProjectSwitcher } from './ProjectLayoutSwitcher';
import { UserMenu } from './UserMenu';

// The compact layout on the pages that belong to the workspace rather than a
// project: Projects, workspace settings, My to-dos, workspace runs, What's new and
// platform admin. They draw the same frame as a project's pages, so the app
// has one interface: the rail on the left (here the workspace's own pages,
// Projects first as it is on a project's rail) and the same top bar, with the
// workspace switcher and the project switcher, where a project page names
// its project and these name "All projects".
//
// The pages keep their own layout below the bar; the rail is fixed to the
// left edge and the page is padded past it (body.ov-has-rail in index.css),
// so no page is restructured to sit beside it. On a phone there is no rail:
// the dark phone header, with both switchers, help, the bell and the account.

const WORKSPACE_ICONS = {
  runs: pageIconPath('agent-runs'),
  todos: pageIconPath('todos'),
  settings: pageIconPath('settings'),
  news: 'M4 4h12v12H4zM7 8h6M7 11h6M7 14h3',
  admin: 'M10 2l7 3v5c0 4-3 7-7 8-4-1-7-4-7-8V5z',
};

/** The workspace's own pages, as the rail lists them and the phone's More offers them. */
const useWorkspacePages = (): { to: string; label: string; icon: string }[] => {
  const runs = useFeature(WORKSPACE_RUNS_FEATURE);
  const todos = useFeature(TODOS_FEATURE);
  const isAdmin = useAppStore((s) => Boolean(s.currentUser?.is_admin));
  return [
    ...(todos ? [{ to: MY_TODOS_PATH, label: 'My to-dos', icon: WORKSPACE_ICONS.todos }] : []),
    ...(runs ? [{ to: '/org/runs', label: 'Runs', icon: WORKSPACE_ICONS.runs }] : []),
    { to: '/whats-new', label: 'News', icon: WORKSPACE_ICONS.news },
    ...(isAdmin ? [{ to: '/admin', label: 'Admin', icon: WORKSPACE_ICONS.admin }] : []),
  ];
};

/** Where a group's tab leads from a workspace page: a group's first page, unless one was read there last. */
const GROUP_START: Record<string, string> = { Plan: '', Define: 'requirements', Verify: 'vv' };

/**
 * The bar along the bottom of a workspace page on a phone, in the refreshed
 * interface (ui-refresh): the same Home, Plan, Define, Verify, More as inside
 * a project, so the bar does not change shape when a project is left. Home is
 * this page. A group's tab opens the project last opened, at the page last
 * read in that group; with no project opened yet there is nowhere for it to
 * lead, and it says so. More holds the workspace's own pages.
 */
const WorkspacePhoneBar: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const pages = useWorkspacePages();
  const project = lastProject();
  const last = loadLastPages();
  useEffect(() => {
    document.body.classList.add('ov-has-tabbar');
    document.documentElement.style.setProperty('--ov-tabbar-height', `${TAB_BAR_HEIGHT}px`);
    return () => {
      document.body.classList.remove('ov-has-tabbar');
      document.documentElement.style.removeProperty('--ov-tabbar-height');
    };
  }, []);
  const home = location.pathname === '/projects';
  return (
    <nav className="ov-tabbar ov-tabbar-fixed safe-area-bottom" aria-label="Workspace tabs" data-help-key="start.navigation">
      <Link to="/projects" className={`ov-tabbar-item${home ? ' active' : ''}`} aria-current={home ? 'page' : undefined}>
        <NavIcon d={pageIconPath('')} size={20} />
        <span>Home</span>
      </Link>
      {REFRESH_GROUPS.slice(0, 3).map((group) =>
        project ? (
          <Link key={group} to={`/projects/${project}/${last[group] ?? GROUP_START[group]}`.replace(/\/$/, '')} className="ov-tabbar-item" title={`${group}, in the project you last opened`}>
            <NavIcon d={groupIconPath(group)} size={20} />
            <span>{group}</span>
          </Link>
        ) : (
          <span key={group} className="ov-tabbar-item is-disabled" aria-disabled="true" title="Open a project first">
            <NavIcon d={groupIconPath(group)} size={20} />
            <span>{group}</span>
          </span>
        )
      )}
      <ActionMenu
        label="More"
        triggerClassName={`ov-tabbar-item${home ? '' : ' active'}`}
        items={[...pages, { to: '/org/settings', label: 'Workspace settings', icon: '' }].map((page) => ({
          key: page.to,
          label: page.label,
          onSelect: () => navigate(page.to),
        }))}
      >
        <NavIcon d={SHELL_ICONS.more} size={20} />
        <span>More</span>
      </ActionMenu>
    </nav>
  );
};

const WorkspaceRail: React.FC = () => {
  const items = useWorkspacePages();
  return (
    <aside className="ov-rail ov-rail-fixed" aria-label="Workspace navigation">
      <NavLink to="/projects" end className="ov-rail-item" title="Home: what needs you, the workspace's activity and its projects">
        <NavIcon d={pageIconPath('')} />
        <span>Home</span>
      </NavLink>
      <nav className="ov-rail-nav" aria-label="Workspace pages" data-help-key="start.navigation">
        {items.map((item) => (
          <NavLink key={item.to} to={item.to} className="ov-rail-item">
            <NavIcon d={item.icon} />
            <span>{item.label}</span>
          </NavLink>
        ))}
        <span className="ov-rail-rule" aria-hidden="true" />
        <NavLink to="/org/settings" className="ov-rail-item">
          <NavIcon d={WORKSPACE_ICONS.settings} />
          <span>Settings</span>
        </NavLink>
      </nav>
    </aside>
  );
};

/**
 * A workspace page's bar on a phone: the dark band of a project page's phone
 * header, with the workspace switcher above the project switcher (which reads
 * All projects here), then help, the bell and the account. The bar of the
 * wide layout has no room for both switchers beside the controls at 390 px.
 */
const PhoneWorkspaceHeader: React.FC<{ helpOpen: boolean; onHelpToggle: () => void }> = ({
  helpOpen,
  onHelpToggle,
}) => (
  <header className="safe-area-top ov-phone-header ov-phone-header-ws">
    <div className="ov-phone-title">
      <OrgSwitcher variant="dark" />
      <ProjectSwitcher current={null} dark />
    </div>
    <button
      type="button"
      aria-label="Help"
      aria-expanded={helpOpen}
      data-help-ui=""
      onClick={onHelpToggle}
      title="Help for this page"
      className="ov-phone-icon"
    >
      {/* The same size and weight as the bell beside it. */}
      <NavIcon d={SHELL_ICONS.help} size={20} />
    </button>
    <NotificationBell variant="dark" />
    {/* The avatar alone, its menu opening downward: the dark variant is the
        sidebar footer's full-width row with the name. It fills its button
        where the two icons sit inside theirs, so it stands off a little to
        keep the three evenly spaced. */}
    <span className="ov-phone-account">
      <UserMenu variant="light" />
    </span>
  </header>
);

interface WorkspaceFrameProps {
  /** The page's name in the bar; omitted on the Projects page, which the switcher names. */
  pageLabel: string;
  helpOpen: boolean;
  onHelpToggle: () => void;
}

/** The rail and the top bar of a workspace page, drawn above the page's own content. */
export const WorkspaceFrame: React.FC<WorkspaceFrameProps> = ({ pageLabel, helpOpen, onHelpToggle }) => {
  const { isCompact } = useViewport();
  const location = useLocation();
  const refresh = useUiRefresh();
  useEffect(() => {
    if (isCompact) return undefined;
    document.body.classList.add('ov-has-rail');
    return () => document.body.classList.remove('ov-has-rail');
  }, [isCompact]);
  const label = location.pathname === '/projects' ? '' : pageLabel;
  // A phone names the page in the page's own heading below the bar.
  if (isCompact) {
    return (
      <>
        <PhoneWorkspaceHeader helpOpen={helpOpen} onHelpToggle={onHelpToggle} />
        {refresh && <WorkspacePhoneBar />}
      </>
    );
  }
  return (
    <>
      <WorkspaceRail />
      <TopBar
        project={null}
        pageLabel={label}
        helpOpen={helpOpen}
        onHelpToggle={onHelpToggle}
      />
    </>
  );
};

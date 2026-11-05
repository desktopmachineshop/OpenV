import React, { useEffect } from 'react';
import { NavLink, useLocation } from 'react-router-dom';
import { useAppStore } from '../state/store';
import { useFeature } from '../hooks/useFeature';
import { useViewport } from '../hooks/useViewport';
import { NavIcon, pageIconPath } from './ProjectLayoutIcons';
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

const WorkspaceRail: React.FC = () => {
  const runs = useFeature(WORKSPACE_RUNS_FEATURE);
  const todos = useFeature(TODOS_FEATURE);
  const isAdmin = useAppStore((s) => Boolean(s.currentUser?.is_admin));
  const items: { to: string; label: string; icon: string }[] = [
    ...(todos ? [{ to: MY_TODOS_PATH, label: 'My to-dos', icon: WORKSPACE_ICONS.todos }] : []),
    ...(runs ? [{ to: '/org/runs', label: 'Runs', icon: WORKSPACE_ICONS.runs }] : []),
    { to: '/whats-new', label: 'News', icon: WORKSPACE_ICONS.news },
    ...(isAdmin ? [{ to: '/admin', label: 'Admin', icon: WORKSPACE_ICONS.admin }] : []),
  ];
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
      ?
    </button>
    <NotificationBell variant="dark" />
    {/* The avatar alone, its menu opening downward: the dark variant is the
        sidebar footer's full-width row with the name. */}
    <UserMenu variant="light" />
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
  useEffect(() => {
    if (isCompact) return undefined;
    document.body.classList.add('ov-has-rail');
    return () => document.body.classList.remove('ov-has-rail');
  }, [isCompact]);
  const label = location.pathname === '/projects' ? '' : pageLabel;
  // A phone names the page in the page's own heading below the bar.
  if (isCompact) return <PhoneWorkspaceHeader helpOpen={helpOpen} onHelpToggle={onHelpToggle} />;
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

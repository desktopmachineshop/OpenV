import React, { useEffect, useRef, useState } from 'react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';
import type { Project } from '../api/client';
import type { NavGroup, NavSection } from '../pages';
import { activeNavPath, sectionHasActive } from './navSections';
import { NavIcon, SHELL_ICONS, groupIconPath, pageIconPath } from './ProjectLayoutIcons';
import { GlobalSearch } from './GlobalSearch';
import { NotificationBell } from './NotificationBell';
import { OrgSwitcher } from './OrgSwitcher';
import { UserMenu } from './UserMenu';
import { ProjectSwitcher } from './ProjectLayoutSwitcher';

// The compact project layout on a wide screen (the compact-layout feature
// key): a 64 px icon rail on the left and one top bar, where the sidebar
// layout spends 200 px on the menu and stacks the workspace, the project, the
// search and the account into it.
//
// The rail holds one button per menu group (Define, Verify, Plan, Agents)
// and the headerless tail's pages as direct links. A group button opens a
// flyout with that group's pages beside the rail; the flyout closes on
// arrival, on Escape and on a click anywhere else. Group buttons are real
// buttons with aria-expanded and the pages real links named by their menu
// label, so a group is opened and a page reached the same way as in the
// sidebar layout (e2e/tests/helpers.ts openModule).
//
// The top bar carries the workspace, where you are (project / page), the
// workspace search, the help button (also ? and F1, hooks/useHelpShortcut.ts),
// the bell and the account. data-help-key marks the menu and the search for
// the help panel (start.navigation in Getting started, search in Requirements & artifacts).

interface RailProps {
  sections: NavSection[];
  activePath: string;
}

const Rail: React.FC<RailProps> = ({ sections, activePath }) => {
  const location = useLocation();
  const [openGroup, setOpenGroup] = useState<NavGroup | null>(null);
  const railRef = useRef<HTMLElement | null>(null);

  // Arriving somewhere is the commonest reason to be done with the flyout.
  useEffect(() => setOpenGroup(null), [location.pathname]);

  useEffect(() => {
    if (!openGroup) return undefined;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpenGroup(null);
    };
    const onDown = (e: MouseEvent) => {
      if (railRef.current && !railRef.current.contains(e.target as Node)) setOpenGroup(null);
    };
    window.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onDown);
    return () => {
      window.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onDown);
    };
  }, [openGroup]);

  const grouped = sections.filter((s) => s.label && s.items.length > 0);
  const tail = sections.find((s) => !s.label)?.items ?? [];
  const flyout = grouped.find((s) => s.label === openGroup);

  return (
    <aside ref={railRef} className="ov-rail" aria-label="Project navigation">
      <NavLink to="/projects" end className="ov-rail-item" title="All projects in this workspace">
        <NavIcon d={SHELL_ICONS.projects} />
        <span>Projects</span>
      </NavLink>
      <nav className="ov-rail-nav" aria-label="Project pages" data-help-key="start.navigation">
        {grouped.map((section) => {
          const label = section.label as NavGroup;
          const expanded = openGroup === label;
          const current = sectionHasActive(section.items, activePath);
          return (
            <button
              key={label}
              type="button"
              className={`ov-rail-item${current ? ' is-current' : ''}`}
              aria-expanded={expanded}
              aria-controls={expanded ? 'ov-rail-flyout' : undefined}
              onClick={() => setOpenGroup(expanded ? null : label)}
            >
              <NavIcon d={groupIconPath(label)} />
              <span>{label}</span>
            </button>
          );
        })}
        <span className="ov-rail-rule" aria-hidden="true" />
        {tail.map((item) => (
          <NavLink key={item.to} to={item.to} end={item.end} className="ov-rail-item">
            <NavIcon d={pageIconPath(item.to)} />
            <span>{item.label}</span>
          </NavLink>
        ))}
        {flyout && (
          <div
            id="ov-rail-flyout"
            className="ov-rail-flyout"
            role="group"
            aria-label={`${flyout.label} pages`}
          >
            <div className="ov-rail-flyout-title">{flyout.label}</div>
            {flyout.items.map((item) => (
              <NavLink key={item.to} to={item.to} end={item.end} className="ov-rail-flyout-link">
                <NavIcon d={pageIconPath(item.to)} size={16} />
                {item.label}
              </NavLink>
            ))}
          </div>
        )}
      </nav>
    </aside>
  );
};

interface TopBarProps {
  /** The project in view, or null on a workspace page. */
  project: Project | null;
  pageLabel: string;
  helpOpen: boolean;
  onHelpToggle: () => void;
  /** A phone or tablet: the search stays in the project menu, not the bar. */
  compact?: boolean;
}

/**
 * The one top bar of the compact layout, on project pages and workspace pages
 * alike: the workspace switcher, the project switcher, the page, the
 * workspace search, help, the bell and the account.
 */
export const TopBar: React.FC<TopBarProps> = ({ project, pageLabel, helpOpen, onHelpToggle, compact = false }) => (
  <header className="ov-topbar safe-area-top">
    <div className="ov-topbar-org">
      <OrgSwitcher variant="light" />
    </div>
    <span className="ov-topbar-sep" aria-hidden="true">
      /
    </span>
    <nav className="ov-topbar-crumbs" aria-label="You are here">
      <ProjectSwitcher current={project} />
      {pageLabel && (
        <>
          <span className="ov-topbar-sep" aria-hidden="true">
            /
          </span>
          <span className="ov-topbar-page" aria-current="page">
            {pageLabel}
          </span>
        </>
      )}
    </nav>
    {!compact && (
      <div className="ov-topbar-search" data-help-key="search">
        <GlobalSearch />
      </div>
    )}
    <button
      type="button"
      className="ov-topbar-help"
      aria-label="Help"
      aria-expanded={helpOpen}
      aria-keyshortcuts="? F1"
      data-help-ui=""
      onClick={onHelpToggle}
      title="Help for this page (? or F1)"
    >
      Help
      <kbd aria-hidden="true">?</kbd>
    </button>
    <NotificationBell variant="light" />
    <UserMenu variant="light" />
  </header>
);

interface RailShellProps {
  project: Project | null;
  projectId: string;
  sections: NavSection[];
  helpOpen: boolean;
  onHelpToggle: () => void;
  /** The help panel, which the caller mounts once for every layout. */
  help: React.ReactNode;
}

export const RailShell: React.FC<RailShellProps> = ({
  project,
  projectId,
  sections,
  helpOpen,
  onHelpToggle,
  help,
}) => {
  const location = useLocation();
  const activePath = activeNavPath(location.pathname, projectId);
  const pageLabel =
    sections.flatMap((s) => s.items).find((item) => item.to === activePath)?.label ?? '';
  return (
    <div className="app-shell ov-layout">
      <Rail sections={sections} activePath={activePath} />
      <div className="ov-layout-column">
        <TopBar
          project={project}
          pageLabel={activePath === '' ? '' : pageLabel}
          helpOpen={helpOpen}
          onHelpToggle={onHelpToggle}
        />
        <main className="ov-layout-main">
          <Outlet />
        </main>
      </div>
      {help}
    </div>
  );
};

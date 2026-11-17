import React, { useEffect, useState } from 'react';
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom';
import type { Project } from '../api/client';
import type { NavGroup, NavSection } from '../pages';
import { GlobalSearch } from './GlobalSearch';
import { NotificationBell } from './NotificationBell';
import { OrgSwitcher } from './OrgSwitcher';
import { UserMenu } from './UserMenu';
import { ProjectSwitcher } from './ProjectLayoutSwitcher';
import { GroupTabs } from './ProjectLayoutGroupTabs';
import { NavIcon, SHELL_ICONS, groupIconPath, pageIconPath } from './ProjectLayoutIcons';
import { activeGroup, groupLanding, loadLastPages } from './ProjectLayoutNav';
import { activeNavPath, sectionHasActive } from './navSections';
import { TAB_BAR_HEIGHT } from './ProjectLayoutTabs';

// The project shell on a phone or a tablet in the refreshed layout (the
// ui-refresh feature key). Top to bottom: a header with the project's name
// and the bell; the row of the current group's pages; the page; and the bar,
// which reads Home, Plan, Define, Verify, More from left to right, the same
// groups in the same order as the rail on a computer reads top to bottom.
//
// A group's tab opens the page last read in that group. More is the whole
// map: every page as a tile under its group, with the workspace, the project
// switcher, the search, help and the account. There is no menu button in the
// header: More is the one way to the menu.

interface PhoneShellProps {
  project: Project | null;
  projectId: string;
  sections: NavSection[];
  onHelpOpen: () => void;
  /** The help panel, which the caller mounts once for every layout. */
  help: React.ReactNode;
}

interface MoreProps {
  sections: NavSection[];
  project: Project | null;
  onClose: () => void;
  onHelpOpen: () => void;
}

/** The More page: every page of the project as a tile, group by group. */
const MoreGrid: React.FC<MoreProps> = ({ sections, project, onClose, onHelpOpen }) => {
  const grouped = sections.filter((s) => s.label);
  const tail = sections.filter((s) => !s.label).flatMap((s) => s.items);
  return (
    <aside className="ov-more ov-more-refresh" aria-label="Project navigation">
      <div className="ov-more-body">
        <div className="ov-more-card ov-more-tools">
          <OrgSwitcher variant="light" />
          <ProjectSwitcher current={project} />
          <div className="ov-more-search">
            <GlobalSearch />
          </div>
        </div>
        <nav className="ov-more-nav" aria-label="Project pages" data-help-key="start.navigation">
          {grouped.map((section) => (
            <section key={section.label} className="ov-more-section" aria-label={section.label}>
              <span className="ov-more-label is-static">{section.label}</span>
              <div className="ov-more-grid">
                {section.items.map((item) => (
                  <NavLink key={item.to} to={item.to} end={item.end} className="ov-more-tile" onClick={onClose}>
                    <NavIcon d={pageIconPath(item.to)} size={20} />
                    <span>{item.label}</span>
                  </NavLink>
                ))}
              </div>
            </section>
          ))}
          <section className="ov-more-section" aria-label="Project">
            <span className="ov-more-label is-static">Project</span>
            <div className="ov-more-grid">
              {tail.map((item) => (
                <NavLink key={item.to} to={item.to} end={item.end} className="ov-more-tile" onClick={onClose}>
                  <NavIcon d={pageIconPath(item.to)} size={20} />
                  <span>{item.label}</span>
                </NavLink>
              ))}
              <button
                type="button"
                className="ov-more-tile"
                data-help-ui=""
                onClick={() => {
                  onClose();
                  onHelpOpen();
                }}
              >
                <NavIcon d={SHELL_ICONS.help} size={20} />
                <span>Help</span>
              </button>
              {/* Plain anchor (not NavLink): the manual opens in a new tab so
                  it doesn't navigate the user away from their work (#162). */}
              <a href="/manual" target="_blank" rel="noopener" title="Open the user manual in a new tab" className="ov-more-tile">
                <NavIcon d={pageIconPath('requirements')} size={20} />
                <span>Manual</span>
              </a>
              <Link to="/projects" className="ov-more-tile" title="Back to all projects in this workspace">
                <NavIcon d={SHELL_ICONS.projects} size={20} />
                <span>All projects</span>
              </Link>
            </div>
          </section>
        </nav>
        <section className="ov-more-section" aria-label="You">
          <span className="ov-more-label is-static">You</span>
          {/* The account controls keep the dark band they were drawn for:
              the name beside the avatar, and a menu that opens upward. */}
          <div className="ov-more-you">
            <div style={{ flex: 1, minWidth: 0 }}>
              <UserMenu variant="dark" />
            </div>
          </div>
        </section>
      </div>
    </aside>
  );
};

interface BarProps {
  sections: NavSection[];
  activePath: string;
  moreOpen: boolean;
  onMore: () => void;
  onNavigate: () => void;
}

/** The bar along the bottom: Home, each group, More. */
const GroupBar: React.FC<BarProps> = ({ sections, activePath, moreOpen, onMore, onNavigate }) => {
  useEffect(() => {
    const root = document.documentElement;
    root.style.setProperty('--ov-tabbar-height', `${TAB_BAR_HEIGHT}px`);
    return () => {
      root.style.removeProperty('--ov-tabbar-height');
    };
  }, []);
  const last = loadLastPages();
  // The bar has room for Home, three groups and More; a group past the third
  // (Agents) is reached from More.
  const groups = sections.filter((s) => s.label).slice(0, 3);
  // A page whose group has no tab of its own (Agents, Activity, Settings) is
  // one of More's, so More is the tab that is lit.
  const inMore = moreOpen || !groups.some((s) => sectionHasActive(s.items, activePath));
  return (
    <nav className="ov-tabbar safe-area-bottom" aria-label="Project tabs" data-help-key="start.navigation">
      <Link to="/projects" className="ov-tabbar-item" title="Home: what needs you, the workspace's activity and its projects">
        <NavIcon d={pageIconPath('')} size={20} />
        <span>Home</span>
      </Link>
      {groups.map((section) => {
        const current = !moreOpen && sectionHasActive(section.items, activePath);
        return (
          <Link
            key={section.label}
            to={groupLanding(section, last)}
            className={`ov-tabbar-item${current ? ' active' : ''}`}
            aria-current={current ? 'true' : undefined}
            onClick={onNavigate}
          >
            <NavIcon d={groupIconPath(section.label as NavGroup)} size={20} />
            <span>{section.label}</span>
          </Link>
        );
      })}
      <button type="button" className={`ov-tabbar-item${inMore ? ' active' : ''}`} aria-expanded={moreOpen} onClick={onMore}>
        <NavIcon d={SHELL_ICONS.more} size={20} />
        <span>More</span>
      </button>
    </nav>
  );
};

export const PhoneShell: React.FC<PhoneShellProps> = ({ project, projectId, sections, onHelpOpen, help }) => {
  const location = useLocation();
  const [moreOpen, setMoreOpen] = useState(false);
  const activePath = activeNavPath(location.pathname, projectId);
  const group = activeGroup(sections, activePath);

  // Arriving somewhere is the commonest reason to be done with the menu.
  useEffect(() => setMoreOpen(false), [location.pathname]);

  useEffect(() => {
    if (!moreOpen) return undefined;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMoreOpen(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [moreOpen]);

  const name = project?.name || '…';
  return (
    <div className="app-shell ov-refresh ov-phone-shell">
      <header className="safe-area-top ov-phone-header ov-phone-header-refresh">
        <div className="ov-phone-title" title={name}>
          <span className="ov-phone-title-page">{name}</span>
        </div>
        <NotificationBell variant="dark" />
      </header>
      {moreOpen && (
        <MoreGrid sections={sections} project={project} onClose={() => setMoreOpen(false)} onHelpOpen={onHelpOpen} />
      )}
      {/* The page stays mounted under the menu, hidden, so opening More does
          not throw away a draft or a scroll position. */}
      {group && !moreOpen && <GroupTabs section={group} />}
      <main className="ov-phone-main" hidden={moreOpen}>
        <Outlet />
      </main>
      <GroupBar
        sections={sections}
        activePath={activePath}
        moreOpen={moreOpen}
        onMore={() => setMoreOpen((open) => !open)}
        onNavigate={() => setMoreOpen(false)}
      />
      {help}
    </div>
  );
};

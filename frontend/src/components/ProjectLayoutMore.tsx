import React from 'react';
import { NavLink, useNavigate } from 'react-router-dom';
import type { Project } from '../api/client';
import { ProjectSwitcher } from './ProjectLayoutSwitcher';
import type { NavSection } from '../pages';
import { GlobalSearch } from './GlobalSearch';
import { NotificationBell } from './NotificationBell';
import { OrgSwitcher } from './OrgSwitcher';
import { UserMenu } from './UserMenu';
import { NavIcon, SHELL_ICONS, pageIconPath } from './ProjectLayoutIcons';
import { NavSectionState, navSectionIsOpen, saveNavSections, sectionHasActive, toggleNavSection } from './navSections';

// The project menu on a phone in the compact layout (the More design): a
// page of its own rather than a dark drawer. The project at the top, with the
// way back to every project; the workspace and the search; then each group of
// pages as a card of rows with an icon tile, under its uppercase label; and
// "You" at the foot, the account menu, help and the bell.
//
// It is the same menu as the drawer it replaces, element for element: the
// groups still collapse (the label is the button, as before), every page is
// still a link by its menu name, and the account controls are the same ones.

interface PhoneMoreMenuProps {
  project: Project | null;
  sections: NavSection[];
  activePath: string;
  openSections: NavSectionState;
  setOpenSections: (next: NavSectionState) => void;
  onClose: () => void;
}

const initials = (name: string): string =>
  name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]?.toUpperCase() ?? '')
    .join('') || '·';

const Chevron: React.FC = () => (
  <svg width="16" height="16" viewBox="0 0 20 20" fill="none" stroke="var(--neutral-mid)" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M8 4l6 6-6 6" />
  </svg>
);

export const PhoneMoreMenu: React.FC<PhoneMoreMenuProps> = ({
  project,
  sections,
  activePath,
  openSections,
  setOpenSections,
  onClose,
}) => {
  const navigate = useNavigate();
  const name = project?.name || '…';
  return (
    <div className="ov-more">
      <div className="ov-more-head">
        <span className="ov-more-title">More</span>
        <button type="button" aria-label="Close menu" onClick={onClose} className="ov-more-close">
          ✕
        </button>
      </div>
      <div className="ov-more-body">
        <button
          type="button"
          className="ov-more-project"
          title="Back to all projects in this workspace"
          onClick={() => navigate('/projects')}
        >
          <span className="ov-more-project-tile" aria-hidden="true">
            {initials(project?.name || '')}
          </span>
          <span className="ov-more-row-text">
            <b>{name}</b>
            <span className="ov-more-meta">
              <span aria-hidden>←</span> All projects
            </span>
          </span>
          <Chevron />
        </button>
        <div className="ov-more-card ov-more-tools">
          <OrgSwitcher variant="light" />
          <ProjectSwitcher current={project} />
          <div className="ov-more-search">
            <GlobalSearch />
          </div>
        </div>
        <nav className="ov-more-nav" data-help-key="start.navigation">
          {sections.map((section, i) => {
            const label = section.label;
            const expanded = label
              ? navSectionIsOpen(openSections, label, sectionHasActive(section.items, activePath))
              : true;
            return (
              <section key={label || `section-${i}`} className="ov-more-section">
                {label && (
                  <button
                    type="button"
                    className="ov-more-label"
                    aria-expanded={expanded}
                    title={`${expanded ? 'Collapse' : 'Expand'} ${label}`}
                    onClick={() => {
                      const next = toggleNavSection(openSections, label, expanded);
                      setOpenSections(next);
                      saveNavSections(next);
                    }}
                  >
                    <span aria-hidden style={{ width: 12 }}>
                      {expanded ? '▾' : '▸'}
                    </span>
                    {label}
                  </button>
                )}
                {expanded && (
                  <div className="ov-more-card">
                    {section.items.map((item) => (
                      <NavLink key={item.to} to={item.to} end={item.end} className="ov-more-row">
                        <span className="ov-more-icon" aria-hidden="true">
                          <NavIcon d={pageIconPath(item.to)} size={18} />
                        </span>
                        <span className="ov-more-row-text">{item.label}</span>
                        <Chevron />
                      </NavLink>
                    ))}
                  </div>
                )}
              </section>
            );
          })}
          <section className="ov-more-section">
            <span className="ov-more-label is-static">Help</span>
            <div className="ov-more-card">
              {/* Plain anchor (not NavLink): the manual opens in a new tab so
                  it doesn't navigate the user away from their work (#162). */}
              <a href="/manual" target="_blank" rel="noopener" title="Open the user manual in a new tab" className="ov-more-row">
                <span className="ov-more-icon" aria-hidden="true">
                  <NavIcon d={SHELL_ICONS.help} size={18} />
                </span>
                <span className="ov-more-row-text">Help</span>
                <Chevron />
              </a>
            </div>
          </section>
        </nav>
        <section className="ov-more-section">
          <span className="ov-more-label is-static">You</span>
          {/* The account controls keep the dark band they were drawn for. */}
          <div className="ov-more-you">
            <div style={{ flex: 1, minWidth: 0 }}>
              <UserMenu variant="dark" />
            </div>
            <NotificationBell variant="dark" />
          </div>
        </section>
      </div>
    </div>
  );
};

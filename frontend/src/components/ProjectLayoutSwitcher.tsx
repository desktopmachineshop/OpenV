import React, { useEffect, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { projectAPI, type Project } from '../api/client';
import { useAppStore } from '../state/store';
import { useFeature } from '../hooks/useFeature';
import { REDESIGN_PAGES_FEATURE } from '../features';

// The project switcher in the top bar (the compact layout): the current
// project's name with a caret, or "All projects" outside one, opening a list
// of the workspace's projects to move to, then All projects and New project.
// It is on every page that has the top bar, inside a project and out, so a
// project is chosen the same way wherever you are, as the design's top bar
// draws it ("Brake-by-wire ECU ▾").
//
// A disclosure, not an ARIA menu: a button with aria-expanded controlling a
// list of ordinary links, so the links are reached with Tab and each one is
// an address. It closes on arrival, on Escape and on a click elsewhere.

interface ProjectSwitcherProps {
  /** The project in view, or null outside a project. */
  current: Project | null;
  /** On the dark band of a phone header. */
  dark?: boolean;
}

/** Where a project opens: its Home with the redesign, its overview without. */
export const projectStart = (id: string, home: boolean): string => `/projects/${id}${home ? '/home' : ''}`;

export const ProjectSwitcher: React.FC<ProjectSwitcherProps> = ({ current, dark = false }) => {
  const location = useLocation();
  const home = useFeature(REDESIGN_PAGES_FEATURE);
  const stored = useAppStore((s) => s.projects);
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const [open, setOpen] = useState(false);
  const [loaded, setLoaded] = useState<Project[] | null>(null);
  const ref = useRef<HTMLDivElement | null>(null);

  useEffect(() => setOpen(false), [location.pathname]);
  // The list a page loaded may be another workspace's; read it afresh when
  // the workspace changes.
  useEffect(() => setLoaded(null), [activeOrgId]);

  useEffect(() => {
    if (!open) return undefined;
    if (loaded === null) {
      projectAPI
        .list()
        .then((res) => setLoaded(res.data || []))
        .catch(() => setLoaded(stored || []));
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    window.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onDown);
    return () => {
      window.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onDown);
    };
  }, [open, loaded, stored]);

  const projects = loaded ?? stored ?? [];
  const label = current ? current.name || '…' : 'All projects';

  return (
    <div ref={ref} className={dark ? 'ov-project-switcher is-dark' : 'ov-project-switcher'} data-help-key="start.navigation">
      <button
        type="button"
        className="ov-topbar-project"
        aria-expanded={open}
        aria-controls={open ? 'ov-project-list' : undefined}
        aria-label={current ? `Project: ${label}. Switch project` : 'Choose a project'}
        title={current ? `${label}: switch project` : 'Choose a project'}
        onClick={() => setOpen((o) => !o)}
      >
        <span className="ov-project-switcher-name">{label}</span>
        <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M6 8l4 4 4-4" />
        </svg>
      </button>
      {open && (
        <div id="ov-project-list" className="ov-project-list" role="group" aria-label="Projects in this workspace">
          <div className="ov-project-list-items">
            {loaded === null && projects.length === 0 && <span className="ov-project-list-note">Loading projects…</span>}
            {loaded !== null && projects.length === 0 && <span className="ov-project-list-note">No projects yet</span>}
            {projects.map((p) => (
              <Link
                key={p.id}
                to={projectStart(p.id, home)}
                className="ov-project-list-link"
                aria-current={current?.id === p.id ? 'true' : undefined}
              >
                {p.name}
              </Link>
            ))}
          </div>
          <div className="ov-project-list-foot">
            <Link to="/projects" className="ov-project-list-link">
              All projects
            </Link>
            <Link to="/projects" state={{ create: true }} className="ov-project-list-link is-new">
              + New project
            </Link>
          </div>
        </div>
      )}
    </div>
  );
};

import { useEffect, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { rememberProject } from '../appShortcuts';
import { guidedAPI, projectAPI, Project } from '../api/client';
import { hasWizardProgress } from './wizard/assistantSession';
import { useAppStore } from '../state/store';
import type { FeatureKey } from '../generated/contract';
import type { NavSection } from '../pages';

// What every project shell needs to know about the project it wraps, kept
// apart from the shells so the sidebar layout and the compact layout read it
// the same way: the project itself, and whether a guided definition has been
// worked on (which takes Guided Definition out of the menu).

export interface ProjectShellState {
  project: Project | null;
  hasGuidedSession: boolean;
}

export const useProjectShell = (projectId: string | undefined): ProjectShellState => {
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
    // Remember where we are, so an installed-app shortcut to the review queue
    // or the board (which have no project-free url) can land in one tap.
    rememberProject(projectId);
    projectAPI
      .get(projectId)
      .then((res) => setProject(res.data))
      .catch(() => navigate('/projects'));
  }, [projectId, setProjectId, navigate]);

  // Deep link into a project from another workspace: silently switch the
  // active workspace when the project belongs to an org the user is in.
  // (If they aren't a member, the project fetch 404s and redirects above.)
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

  return { project, hasGuidedSession };
};

/**
 * The menu as this member sees it: Guided Definition leaves once a guided
 * definition has been worked on, and a gated page waits for its feature.
 * Both layouts draw from this, so they always list the same pages.
 */
export const visibleSections = (
  sections: NavSection[],
  hasGuidedSession: boolean,
  features: Partial<Record<FeatureKey, boolean>> | undefined
): NavSection[] =>
  sections.map((section) => ({
    ...section,
    items: section.items.filter(
      (item) =>
        (item.to !== 'guided' || !hasGuidedSession) &&
        (!item.feature || Boolean(features?.[item.feature]))
    ),
  }));

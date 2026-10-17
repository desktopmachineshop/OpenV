// The page registry (refactor plan X17).
//
// Every routed page of the app, one entry each: where it lives, whether its
// view is loaded eagerly or through lazy(), whether it needs a session, which
// help topic it shows and, for a project page the menu lists, where the menu
// puts it. It is a record of what App.tsx's <Route> table already does, held
// to it by arch/pagesParity.test.ts, so the table stays hand-written (K13);
// the registry does not render a route.
//
// What the registry drives today is the project menu: navSections, below, is
// derived from it and is what ProjectLayout draws. The lists that still repeat
// a page by hand are each held to the registry by a test and not derived from
// it (utils/publicPaths.ts, components/helpTopics.ts).
//
// Adding a page: add its entry here, in the position the menu should show it,
// beside its <Route> in App.tsx. projectPath() builds the URL of a project's
// page for new code; existing call sites keep their template strings.
import type { FeatureKey } from './generated/contract';
import { TODO_LIST_FEATURE } from './features';

/** The labelled groups of the project menu, in the order it draws them. */
export type NavGroup = 'Define' | 'Verify' | 'Plan' | 'Agents';

/**
 * Where a project page sits in the menu. `group` is null for the tail that
 * has no header and never collapses. `end` makes the link current only on
 * its own path; `feature` hides the entry until that gate is on for the
 * workspace.
 */
export interface PageNav {
  group: NavGroup | null;
  label: string;
  end?: true;
  feature?: FeatureKey;
}

/**
 * The help panel's topic for a page: the key helpTopics.ts keeps it under,
 * or an explicit statement that the page has none of its own and shows the
 * overview's, with the reason.
 */
export type PageHelp = { topic: string } | { fallback: string };

export interface Page {
  /**
   * `site`: a route of the app's own, from the root. `project`: a route
   * beneath /projects/:projectId, which ProjectLayout wraps.
   */
  scope: 'site' | 'project';
  /**
   * The route's path as App.tsx writes it, relative to the scope: the
   * segment ('requirements', and '' for the overview or the front page),
   * with the rest of the pattern for a deeper route ('vv/runs/:runId').
   */
  path: string;
  /** How App.tsx brings the view in; a page that only redirects has none. */
  load: 'eager' | 'lazy' | 'redirect';
  /** Whether it renders without a session (utils/publicPaths.ts). */
  public: boolean;
  help: PageHelp;
  /** Present on the project pages the menu lists. */
  nav?: PageNav;
}

const NO_PANEL: PageHelp = { fallback: 'the page has no help panel' };
const NOT_WRITTEN: PageHelp = { fallback: 'no topic is written for the page' };

// The project's pages first, in the order of the menu (a group's entries run
// in the order they appear here), then the project routes the menu does not
// list, then the app's own pages.
export const PAGES: readonly Page[] = [
  {
    scope: 'project',
    path: '',
    load: 'eager',
    public: false,
    help: { fallback: 'the overview is the default topic' },
    nav: { group: 'Define', label: 'Overview', end: true },
  },
  {
    scope: 'project',
    path: 'guided',
    load: 'eager',
    public: false,
    help: { topic: 'guided' },
    nav: { group: 'Define', label: 'Guided Definition' },
  },
  {
    scope: 'project',
    path: 'requirements',
    load: 'lazy',
    public: false,
    help: { topic: 'requirements' },
    nav: { group: 'Define', label: 'Requirements' },
  },
  {
    scope: 'project',
    path: 'interviews',
    load: 'eager',
    public: false,
    help: { topic: 'interviews' },
    nav: { group: 'Define', label: 'Interviews' },
  },
  {
    scope: 'project',
    path: 'vv',
    load: 'eager',
    public: false,
    help: { topic: 'vv' },
    nav: { group: 'Verify', label: 'V&V' },
  },
  {
    scope: 'project',
    path: 'evidence',
    load: 'eager',
    public: false,
    help: NOT_WRITTEN,
    nav: { group: 'Verify', label: 'Evidence' },
  },
  {
    scope: 'project',
    path: 'matrix',
    load: 'lazy',
    public: false,
    help: { topic: 'matrix' },
    nav: { group: 'Verify', label: 'Traceability' },
  },
  {
    scope: 'project',
    path: 'impact',
    load: 'lazy',
    public: false,
    help: NOT_WRITTEN,
    nav: { group: 'Verify', label: 'Impact' },
  },
  {
    scope: 'project',
    path: 'review',
    load: 'lazy',
    public: false,
    help: NOT_WRITTEN,
    nav: { group: 'Verify', label: 'Review Queue' },
  },
  {
    scope: 'project',
    path: 'board',
    load: 'eager',
    public: false,
    help: { topic: 'board' },
    nav: { group: 'Plan', label: 'Board' },
  },
  {
    scope: 'project',
    path: 'todos',
    load: 'eager',
    public: false,
    help: NOT_WRITTEN,
    nav: { group: 'Plan', label: 'To-dos', feature: TODO_LIST_FEATURE },
  },
  {
    scope: 'project',
    path: 'agents',
    load: 'eager',
    public: false,
    help: { topic: 'agents' },
    nav: { group: 'Agents', label: 'Agents' },
  },
  {
    scope: 'project',
    path: 'crew',
    load: 'lazy',
    public: false,
    help: { topic: 'crew' },
    nav: { group: 'Agents', label: 'Crew' },
  },
  {
    scope: 'project',
    path: 'automations',
    load: 'eager',
    public: false,
    help: { topic: 'automations' },
    nav: { group: 'Agents', label: 'Automations' },
  },
  {
    scope: 'project',
    path: 'agent-runs',
    load: 'eager',
    public: false,
    help: { topic: 'agent-runs' },
    nav: { group: 'Agents', label: 'Runs' },
  },
  {
    scope: 'project',
    path: 'activity',
    load: 'lazy',
    public: false,
    help: { topic: 'activity' },
    nav: { group: null, label: 'Activity' },
  },
  {
    scope: 'project',
    path: 'settings',
    load: 'eager',
    public: false,
    help: { topic: 'settings' },
    nav: { group: null, label: 'Settings' },
  },

  { scope: 'project', path: 'baselines/:baselineId/compare', load: 'lazy', public: false, help: { topic: 'baselines' } },
  { scope: 'project', path: 'vv/runs/:runId', load: 'lazy', public: false, help: { topic: 'vv' } },
  { scope: 'project', path: 'crew/network', load: 'lazy', public: false, help: { topic: 'crew' } },
  { scope: 'project', path: 'team', load: 'redirect', public: false, help: NOT_WRITTEN },
  { scope: 'project', path: 'team/network', load: 'redirect', public: false, help: NOT_WRITTEN },

  { scope: 'site', path: 'login', load: 'eager', public: true, help: NO_PANEL },
  { scope: 'site', path: 'verify-email', load: 'eager', public: true, help: NO_PANEL },
  { scope: 'site', path: 'reset-password', load: 'eager', public: true, help: NO_PANEL },
  { scope: 'site', path: 'interview/:token', load: 'eager', public: true, help: NO_PANEL },
  { scope: 'site', path: 'share/:token', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 's/:token', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'open-source/:id', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: '', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'pricing', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'how-it-works', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'demos', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'faq', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'security', load: 'redirect', public: true, help: NO_PANEL },
  { scope: 'site', path: 'privacy', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'open-source', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'customers', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'white-papers', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'changelog', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'compare', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'compare/doors', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'compare/jama', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'compare/polarion', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'compare/reqview', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'compare/jira', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'requirements-traceability', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'verification-evidence', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'baselines-and-audit-trail', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'reqif', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'self-hosted-requirements-management', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'safety-critical', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'ai-connector', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'glossary', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'projects', load: 'eager', public: false, help: { topic: 'projects' } },
  { scope: 'site', path: 'org/settings', load: 'eager', public: false, help: NO_PANEL },
  { scope: 'site', path: 'org/runs', load: 'lazy', public: false, help: NO_PANEL },
  { scope: 'site', path: 'manual', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'manual/:chapterSlug', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'whats-new', load: 'lazy', public: false, help: NO_PANEL },
  { scope: 'site', path: 'connect', load: 'lazy', public: false, help: NO_PANEL },
  { scope: 'site', path: 'admin', load: 'lazy', public: false, help: NO_PANEL },
];

/** The full route pattern of a page, as react-router matches it. */
export const pagePattern = (page: Page): string => {
  const base = page.scope === 'project' ? '/projects/:projectId' : '';
  return page.path ? `${base}/${page.path}` : base || '/';
};

/**
 * The URL of one of a project's pages: `projectPath(id)` is the overview,
 * `projectPath(id, 'vv')` the V&V page. For new code; existing call sites
 * build the same string themselves and are left as they are.
 */
export const projectPath = (projectId: string, path = ''): string =>
  path ? `/projects/${projectId}/${path}` : `/projects/${projectId}`;

export interface NavItem {
  to: string;
  label: string;
  end?: boolean;
  /** Hide the entry until this gated feature is on for the workspace. */
  feature?: FeatureKey;
}

export interface NavSection {
  label?: string;
  items: NavItem[];
}

// The sidebar is grouped by what the user is doing: defining the product,
// verifying it, planning the work, and running agents (issue #97). The
// headerless tail comes last.
const NAV_GROUPS: readonly (NavGroup | null)[] = ['Define', 'Verify', 'Plan', 'Agents', null];

const navItem = (page: Page, nav: PageNav): NavItem => ({
  to: page.path,
  label: nav.label,
  ...(nav.end ? { end: true } : {}),
  ...(nav.feature ? { feature: nav.feature } : {}),
});

/** The project menu: PAGES' menu entries by group, in PAGES' order. */
export const navSections: NavSection[] = NAV_GROUPS.map((group) => {
  const items = PAGES.flatMap((page) =>
    page.scope === 'project' && page.nav && page.nav.group === group ? [navItem(page, page.nav)] : []
  );
  return group === null ? { items } : { label: group, items };
});

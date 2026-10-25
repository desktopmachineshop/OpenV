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
import { matchPath } from 'react-router-dom';
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
 * overview's, with the reason. `key` is the manual's context key the panel
 * asks the docs for (manual/help-contexts.yaml, the topics' resourceid):
 * every page a signed-in member reaches has one (pagesHelp.test.ts), and
 * `docs.py sync --check` fails a key no topic answers, directly or through a
 * parent (`vv.runs` falls back to `vv`). A key is an interface: add keys,
 * never rename one (docs/docs-maintenance.md, rule 5).
 */
export type PageHelp = ({ topic: string } | { fallback: string }) & { key?: string };

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
// A page whose help comes from the manual alone: the panel on its bar opens
// only where the manual serves help (the manual-help feature, the help-docs
// capability), so the app's own help has no topic for it.
const MANUAL_ONLY: PageHelp = { fallback: 'the page has help from the manual only' };

// The project's pages first, in the order of the menu (a group's entries run
// in the order they appear here), then the project routes the menu does not
// list, then the app's own pages.
export const PAGES: readonly Page[] = [
  {
    scope: 'project',
    path: '',
    load: 'eager',
    public: false,
    help: { fallback: 'the overview is the default topic', key: 'overview' },
    nav: { group: 'Define', label: 'Overview', end: true },
  },
  {
    scope: 'project',
    path: 'guided',
    load: 'eager',
    public: false,
    help: { topic: 'guided', key: 'guided' },
    nav: { group: 'Define', label: 'Guided Definition' },
  },
  {
    scope: 'project',
    path: 'requirements',
    load: 'lazy',
    public: false,
    help: { topic: 'requirements', key: 'requirements' },
    nav: { group: 'Define', label: 'Requirements' },
  },
  {
    scope: 'project',
    path: 'interviews',
    load: 'eager',
    public: false,
    help: { topic: 'interviews', key: 'interviews' },
    nav: { group: 'Define', label: 'Interviews' },
  },
  {
    scope: 'project',
    path: 'vv',
    load: 'eager',
    public: false,
    help: { topic: 'vv', key: 'vv' },
    nav: { group: 'Verify', label: 'V&V' },
  },
  {
    scope: 'project',
    path: 'evidence',
    load: 'eager',
    public: false,
    help: { ...NOT_WRITTEN, key: 'evidence' },
    nav: { group: 'Verify', label: 'Evidence' },
  },
  {
    scope: 'project',
    path: 'matrix',
    load: 'lazy',
    public: false,
    help: { topic: 'matrix', key: 'matrix' },
    nav: { group: 'Verify', label: 'Traceability' },
  },
  {
    scope: 'project',
    path: 'impact',
    load: 'lazy',
    public: false,
    help: { ...NOT_WRITTEN, key: 'impact' },
    nav: { group: 'Verify', label: 'Impact' },
  },
  {
    scope: 'project',
    path: 'review',
    load: 'lazy',
    public: false,
    help: { ...NOT_WRITTEN, key: 'review' },
    nav: { group: 'Verify', label: 'Review Queue' },
  },
  {
    scope: 'project',
    path: 'board',
    load: 'eager',
    public: false,
    help: { topic: 'board', key: 'board' },
    nav: { group: 'Plan', label: 'Board' },
  },
  {
    scope: 'project',
    path: 'todos',
    load: 'eager',
    public: false,
    help: { ...NOT_WRITTEN, key: 'todos' },
    nav: { group: 'Plan', label: 'To-dos', feature: TODO_LIST_FEATURE },
  },
  {
    scope: 'project',
    path: 'agents',
    load: 'eager',
    public: false,
    help: { topic: 'agents', key: 'agents' },
    nav: { group: 'Agents', label: 'Agents' },
  },
  {
    scope: 'project',
    path: 'crew',
    load: 'lazy',
    public: false,
    help: { topic: 'crew', key: 'crew' },
    nav: { group: 'Agents', label: 'Crew' },
  },
  {
    scope: 'project',
    path: 'automations',
    load: 'eager',
    public: false,
    help: { topic: 'automations', key: 'automations' },
    nav: { group: 'Agents', label: 'Automations' },
  },
  {
    scope: 'project',
    path: 'agent-runs',
    load: 'eager',
    public: false,
    help: { topic: 'agent-runs', key: 'agent-runs' },
    nav: { group: 'Agents', label: 'Runs' },
  },
  {
    scope: 'project',
    path: 'activity',
    load: 'lazy',
    public: false,
    help: { topic: 'activity', key: 'activity' },
    nav: { group: null, label: 'Activity' },
  },
  {
    scope: 'project',
    path: 'settings',
    load: 'eager',
    public: false,
    help: { topic: 'settings', key: 'settings' },
    nav: { group: null, label: 'Settings' },
  },

  { scope: 'project', path: 'baselines/:baselineId/compare', load: 'lazy', public: false, help: { topic: 'baselines', key: 'baselines' } },
  { scope: 'project', path: 'vv/runs/:runId', load: 'lazy', public: false, help: { topic: 'vv', key: 'vv.runs' } },
  { scope: 'project', path: 'crew/network', load: 'lazy', public: false, help: { topic: 'crew', key: 'crew' } },
  { scope: 'project', path: 'team', load: 'redirect', public: false, help: { ...NOT_WRITTEN, key: 'crew' } },
  { scope: 'project', path: 'team/network', load: 'redirect', public: false, help: { ...NOT_WRITTEN, key: 'crew' } },

  { scope: 'site', path: 'login', load: 'eager', public: true, help: NO_PANEL },
  { scope: 'site', path: 'verify-email', load: 'eager', public: true, help: NO_PANEL },
  { scope: 'site', path: 'reset-password', load: 'eager', public: true, help: NO_PANEL },
  { scope: 'site', path: 'interview/:token', load: 'eager', public: true, help: NO_PANEL },
  { scope: 'site', path: 'share/:token', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 's/:token', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'open-source/:id', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: '', load: 'eager', public: true, help: NO_PANEL },
  { scope: 'site', path: 'projects', load: 'eager', public: false, help: { topic: 'projects', key: 'projects' } },
  { scope: 'site', path: 'org/settings', load: 'eager', public: false, help: { ...MANUAL_ONLY, key: 'org.settings' } },
  { scope: 'site', path: 'org/runs', load: 'lazy', public: false, help: { ...MANUAL_ONLY, key: 'org.runs' } },
  { scope: 'site', path: 'manual', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'manual/:chapterSlug', load: 'lazy', public: true, help: NO_PANEL },
  { scope: 'site', path: 'whats-new', load: 'lazy', public: false, help: { ...MANUAL_ONLY, key: 'whats-new' } },
  { scope: 'site', path: 'connect', load: 'lazy', public: false, help: { ...MANUAL_ONLY, key: 'connect' } },
  { scope: 'site', path: 'admin', load: 'lazy', public: false, help: { ...MANUAL_ONLY, key: 'admin' } },
];

/** The full route pattern of a page, as react-router matches it. */
export const pagePattern = (page: Page): string => {
  const base = page.scope === 'project' ? '/projects/:projectId' : '';
  return page.path ? `${base}/${page.path}` : base || '/';
};

/**
 * The manual's context key for the page at `pathname` (its entry's
 * `help.key`), or '' for a path no page with a key matches. The help panel
 * sends it; a dialog, tab or help link may send a more specific one
 * (hooks/useHelp.tsx).
 */
export const helpKeyForPath = (pathname: string): string => {
  for (const page of PAGES) {
    if (page.help.key && matchPath({ path: pagePattern(page), end: true }, pathname)) return page.help.key;
  }
  return '';
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

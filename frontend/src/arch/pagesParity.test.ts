// The page registry (src/pages.ts, refactor plan X17) against the hand-written
// route table of App.tsx and the lists that repeat a page by hand.
//
// The registry records what App.tsx declares; it does not generate it (K13).
// So nothing but these tests keeps the two in step: a route with no entry, an
// entry with no route, a view loaded eagerly in one and lazily in the other,
// a public flag that disagrees with utils/publicPaths.ts, a help topic that
// helpTopics.ts does not hold, or a menu that differs from the one this
// derivation replaced. Read the route table with the same helpers S12 uses.
import { describe, expect, it } from 'vitest';
import { matchRoutes } from 'react-router-dom';
import { helpTopicForPath } from '../components/helpTopics';
import { PAGES, navSections, pagePattern, projectPath, type NavSection, type Page } from '../pages';
import { isPublicPath } from '../utils/publicPaths';
import { componentsOf, flattenRoutes, parseAppRoutes, toRouteObjects, type FlatRoute } from './appRoutes';

const app = parseAppRoutes();
const routes = flattenRoutes(app.routes);

/** A concrete URL for a route pattern: each :param becomes "x". */
const example = (pattern: string) => pattern.replace(/:[^/]+/g, 'x');

// Every route that names a page: not the catch-alls, which answer a URL no
// page has. The project layout shares its path with the overview's index.
const pageRoutes = (): FlatRoute[] => routes.filter((r) => r.node.path !== '*');

// What a route loads: its components' way in, or a redirect when it renders none.
const loadOf = (r: FlatRoute): string => {
  const parts = new Set(componentsOf(r.node.element).map((c) => c.source?.load ?? 'unresolved'));
  if (parts.size === 0) return 'redirect';
  if (parts.size > 1) return 'mixed';
  return [...parts][0];
};

describe('the registry holds one entry per route', () => {
  it('has no two entries for one route', () => {
    const patterns = PAGES.map(pagePattern);
    expect(patterns.filter((p, i) => patterns.indexOf(p) !== i)).toEqual([]);
  });

  it('has an entry for every route of App.tsx', () => {
    const entries = new Set(PAGES.map(pagePattern));
    expect(
      [...new Set(pageRoutes().map((r) => r.fullPath))].filter((p) => !entries.has(p))
    ).toEqual([]);
  });

  it('has a route in App.tsx for every entry', () => {
    const declared = new Set(pageRoutes().map((r) => r.fullPath));
    expect(PAGES.map(pagePattern).filter((p) => !declared.has(p))).toEqual([]);
  });

  it('matches every entry’s URL to its own route', () => {
    const table = toRouteObjects(app.routes, {});
    for (const page of PAGES) {
      const pattern = pagePattern(page);
      const url = page.scope === 'project' ? projectPath('p1', example(page.path)) : example(pattern);
      const hit = matchRoutes(table, url);
      expect(hit, url).toBeTruthy();
      const id = hit![hit!.length - 1].route.id;
      expect(id, url).toBe(page.scope === 'project' && page.path === '' ? `${pattern} (index)` : pattern);
    }
  });
});

describe('the registry agrees with how App.tsx loads each view', () => {
  it('records eager, lazy or redirect as the route does', () => {
    const wrong: string[] = [];
    for (const page of PAGES) {
      const pattern = pagePattern(page);
      const loads = new Set(pageRoutes().filter((r) => r.fullPath === pattern).map(loadOf));
      // The project layout shares the overview's path and loads eagerly too.
      if (loads.size !== 1 || !loads.has(page.load)) wrong.push(`${pattern}: ${page.load} vs ${[...loads]}`);
    }
    expect(wrong).toEqual([]);
  });
});

describe('the registry agrees with utils/publicPaths.ts', () => {
  it('flags a page public exactly when isPublicPath says so', () => {
    const wrong = PAGES.filter((page) => {
      const url = page.scope === 'project' ? projectPath('p1', example(page.path)) : example(pagePattern(page));
      return isPublicPath(url) !== page.public;
    }).map(pagePattern);
    expect(wrong).toEqual([]);
  });

  it('makes no project page public', () => {
    expect(PAGES.filter((p) => p.scope === 'project' && p.public)).toEqual([]);
  });
});

describe('the registry agrees with components/helpTopics.ts', () => {
  const urlOf = (page: Page) =>
    page.scope === 'project' ? projectPath('p1', example(page.path)) : example(pagePattern(page));
  const fallback = helpTopicForPath('/projects/p1');

  it('resolves a page with a topic to that topic, and not to the default', () => {
    for (const page of PAGES) {
      if (!('topic' in page.help)) continue;
      const url = urlOf(page);
      const topic = helpTopicForPath(url);
      expect(topic, url).not.toBe(fallback);
      expect(topic, url).toBe(helpTopicForPath(page.scope === 'project' ? projectPath('p1', page.help.topic) : `/${page.help.topic}`));
    }
  });

  it('resolves a page with an explicit exemption to the default topic', () => {
    for (const page of PAGES) {
      if (!('fallback' in page.help)) continue;
      expect(page.help.fallback, pagePattern(page)).not.toBe('');
      expect(helpTopicForPath(urlOf(page)), urlOf(page)).toBe(fallback);
    }
  });

  it('exempts only the pages known to have no topic', () => {
    expect(
      PAGES.filter((p) => p.scope === 'project' && 'fallback' in p.help).map((p) => p.path)
    ).toEqual(['', 'evidence', 'impact', 'review', 'todos', 'team', 'team/network']);
  });
});

// The literal list ProjectLayout held before the menu was derived, kept here
// as the proof of X17: the derivation must equal it exactly, in order, key
// for key. Do not edit it to follow a change of the menu; change the menu in
// pages.ts and say so in the pull request that does.
const FORMER_NAV_SECTIONS: NavSection[] = [
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
      { to: 'evidence', label: 'Evidence' },
      { to: 'matrix', label: 'Traceability' },
      { to: 'impact', label: 'Impact' },
      { to: 'review', label: 'Review Queue' },
    ],
  },
  {
    label: 'Plan',
    items: [
      { to: 'board', label: 'Board' },
      { to: 'todos', label: 'To-dos', feature: 'todo-list' },
    ],
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

describe('navSections, derived from the registry', () => {
  it('equals the list it replaced, key for key', () => {
    expect(navSections).toStrictEqual(FORMER_NAV_SECTIONS);
  });

  it('draws every project page the menu lists from a route that exists', () => {
    const declared = new Set(pageRoutes().map((r) => r.fullPath));
    for (const section of navSections) {
      for (const item of section.items) {
        expect(declared.has(item.to ? `/projects/:projectId/${item.to}` : '/projects/:projectId'), item.to).toBe(true);
      }
    }
  });

  it('lists only project pages that render a view', () => {
    const listed = PAGES.filter((page) => page.nav);
    expect(listed.filter((page) => page.scope !== 'project' || page.load === 'redirect')).toEqual([]);
  });
});

describe('projectPath', () => {
  it('builds the URL of a project and of a page in it', () => {
    expect(projectPath('p1')).toBe('/projects/p1');
    expect(projectPath('p1', '')).toBe('/projects/p1');
    expect(projectPath('p1', 'vv')).toBe('/projects/p1/vv');
    expect(projectPath('p1', 'vv/runs/r1')).toBe('/projects/p1/vv/runs/r1');
  });
});

import { SITE_PUBLIC_PATHS, isSitePublic } from './paths';
import { SITE_ROUTES } from './routes';

// The site's route table (routes.tsx) and the public paths it registers
// (paths.ts) are two lists, the second kept free of the pages so the app
// knows it without loading the site: every route is public, and every
// public path is a route of the site's.

describe('the site routes', () => {
  it('are public, each once', () => {
    const paths = SITE_ROUTES.map((r) => r.path);
    expect(paths.filter((p) => !isSitePublic(p))).toEqual([]);
    expect(new Set(paths).size).toBe(paths.length);
  });

  it('register no public path the site does not route', () => {
    const paths = new Set(SITE_ROUTES.map((r) => r.path));
    expect(SITE_PUBLIC_PATHS.filter((p) => !paths.has(p))).toEqual([]);
  });
});

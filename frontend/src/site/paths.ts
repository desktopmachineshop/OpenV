// The path segments the public site serves without a session
// (ext/registry.ts, Site.publicPaths). Kept apart from routes.tsx, and free of
// the pages' copy, so the app knows them without loading the site;
// routes.test.ts holds every route of the site to one of them.
export const SITE_PUBLIC_PATHS = [
  '/pricing',
  '/how-it-works',
  '/demos',
  '/faq',
  '/security',
  '/privacy',
  '/open-source',
  '/customers',
  '/white-papers',
  '/changelog',
  '/compare',
  '/requirements-traceability',
  '/verification-evidence',
  '/baselines-and-audit-trail',
  '/reqif',
  '/self-hosted-requirements-management',
  '/safety-critical',
  '/ai-connector',
  '/glossary',
];

/** Whether the site serves path without a session, as utils/publicPaths.ts matches. */
export const isSitePublic = (path: string): boolean =>
  SITE_PUBLIC_PATHS.some((segment) => path === segment || path.startsWith(segment + '/'));

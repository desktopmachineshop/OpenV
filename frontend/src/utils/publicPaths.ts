// Paths that render without a session. The router and the API client's 401
// interceptor both consult this, so a failed unauthenticated call can never
// bounce a visitor off a public page. Matching is by path segment: "/manual"
// covers "/manual/getting-started" but not "/manual-not", and the root is an
// exact match, since every signed-in route starts with a segment of its own.
// /verify-email is public because the emailed link may be opened in a
// browser that holds no session. /share, /s and /open-source are project
// views that need no account (REQ-149, REQ-151); the rest are the site's
// own pages.
const PUBLIC_SEGMENTS = [
  '/login',
  '/pricing',
  '/manual',
  '/interview',
  '/verify-email',
  '/share',
  '/s',
  '/open-source',
  '/how-it-works',
  '/demos',
  '/faq',
  '/customers',
  '/white-papers',
  '/security',
];

export function isPublicPath(pathname: string): boolean {
  const path = pathname.split(/[?#]/)[0];
  if (path === '/' || path === '') return true;
  return PUBLIC_SEGMENTS.some((segment) => path === segment || path.startsWith(segment + '/'));
}

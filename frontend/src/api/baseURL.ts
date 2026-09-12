// Determine API base URL.
//
// 1. REACT_APP_API_URL, baked in at build time: a split deployment where the
//    browser talks to the API's own origin. Cookies are then third-party,
//    which Safari/iOS block — prefer 2.
// 2. Production builds default to the app's own origin: nginx proxies /api/
//    to the API (frontend/nginx.conf), so the session cookie is first-party.
// 3. The CRA dev server and tests fall back to port 8080 on the page's host,
//    which is where the dev compose stack serves the API.
export const getAPIBaseURL = (): string => {
  const configured = (process.env.REACT_APP_API_URL || '').trim().replace(/\/+$/, '');
  if (configured) {
    return configured;
  }
  if (process.env.NODE_ENV === 'production') {
    return '';
  }
  if (typeof window !== 'undefined' && window.location) {
    return `${window.location.protocol}//${window.location.hostname}:8080`;
  }
  return 'http://localhost:8080';
};

// resolveAvatarUrl turns a user's avatar_url into something an <img> can
// load: an uploaded picture is a path on the API, which is only the page's
// own origin when nginx proxies /api/ — behind a configured REACT_APP_API_URL
// it lives elsewhere. Provider URLs are absolute already and pass through.
// Lives here, apart from the API client, so a component can resolve a URL
// without pulling the client (and its axios instance) into a test.
export const resolveAvatarUrl = (url?: string | null): string => {
  if (!url) return '';
  return url.startsWith('/') ? `${getAPIBaseURL()}${url}` : url;
};

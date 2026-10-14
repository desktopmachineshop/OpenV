import { siteAPI } from '../api/client';

// The site visit beacon behind the platform admin's Visitors dashboard: one
// POST per page load, and one more when the browser signs in or signs up,
// which links this browser's visitor to the account. The server names the
// browser by an HttpOnly cookie of its own, so nothing is kept here; it
// ignores bots and browsers that send Global Privacy Control or Do Not
// Track. Only the referring site's origin is sent, never its path.

let sentFor: string | null = null;

const referrerOrigin = (): string => {
  try {
    if (!document.referrer) return '';
    const ref = new URL(document.referrer);
    return ref.host === window.location.host ? '' : ref.origin;
  } catch {
    return '';
  }
};

// sendVisitBeacon reports the page load once per signed-in state: null for
// a browser that is not signed in, else the account id.
export function sendVisitBeacon(userId: string | null): void {
  const key = userId ?? '';
  if (sentFor === key) return;
  sentFor = key;
  let utmSource = '';
  try {
    utmSource = new URLSearchParams(window.location.search).get('utm_source') ?? '';
  } catch {
    // A page with no usable query still counts.
  }
  siteAPI
    .visit({ path: window.location.pathname, referrer: referrerOrigin(), utm_source: utmSource })
    .catch(() => {
      // A lost beacon is a visit not counted; the page never hears of it.
    });
}

// resetVisitBeacon is for tests.
export function resetVisitBeacon(): void {
  sentFor = null;
}

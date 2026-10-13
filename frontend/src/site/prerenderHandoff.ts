// The hand-off from a pre-rendered snapshot to the live page (prerender.tsx).
//
// A pre-rendered document carries the page's markup in #prerender, beside an
// empty #root; index.css hides #root while #prerender is there. The live page
// calls releasePrerender as it mounts, which removes the snapshot and so
// reveals #root with the same page in it. Until then React renders into the
// hidden #root: the auth check's "Loading…" and the lazy route's fallback are
// never on screen. A document that was not pre-rendered has no #prerender,
// and this does nothing.
import { useEffect } from 'react';

export const PRERENDER_ID = 'prerender';

/** How long a snapshot may stay up if no page ever releases it. */
export const PRERENDER_RELEASE_TIMEOUT_MS = 10000;

export function releasePrerender(): void {
  if (typeof document === 'undefined') return;
  document.getElementById(PRERENDER_ID)?.remove();
}

/**
 * Release the snapshot when this page mounts. Call it before any effect of
 * the same component that scrolls or measures, so they run against the live
 * page: effects run in order, and a child's before its parent's.
 */
export function useReleasePrerender(): void {
  useEffect(releasePrerender, []);
}

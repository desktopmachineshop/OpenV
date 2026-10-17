import React from 'react';
import type { Site } from './registry';

// App.tsx's one slot for a registered public site (registry.ts, Site): the
// site's front page, or its pages for a path the app does not route, each
// falling back to what the app does without a site.

export function SiteHome({ site, fallback }: { site: Site | null; fallback: React.ReactElement }) {
  return site ? <site.Home /> : fallback;
}

export function SitePages({ site, fallback }: { site: Site | null; fallback: React.ReactElement }) {
  return site ? <site.Pages fallback={fallback} /> : fallback;
}

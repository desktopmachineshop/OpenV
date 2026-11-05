import React from 'react';
import type { NavGroup } from '../pages';

// Line icons for the compact layout's rail, flyouts and phone tab bar. One
// stroke path each on a 20 × 20 grid, drawn in currentColor so they follow the
// rail's text colour. Decorative: every place that draws one also writes the
// label beside it, so the icon is hidden from assistive technology.

/** Icon paths by project-relative page path ('' is the overview). */
const PAGE_ICONS: Record<string, string> = {
  '': 'M3 10l7-6 7 6v7h-5v-4H8v4H3z',
  guided: 'M4 16L13 7M11 5l4 4M15 3v2M17 5h-2M5 4v2M6 5H4M16 13v2M17 14h-2',
  requirements:
    'M5.5 3h9A1.5 1.5 0 0116 4.5v11a1.5 1.5 0 01-1.5 1.5h-9A1.5 1.5 0 014 15.5v-11A1.5 1.5 0 015.5 3zM7 7h6M7 10h6M7 13h4',
  interviews: 'M3 4h10v7H7l-3 3v-3H3zM15 8h2v6h-1v3l-3-3h-4',
  home: 'M3 3h6v8H3zM11 3h6v5h-6zM11 10h6v7h-6zM3 13h6v4H3z',
  design: 'M10 3l7 4-7 4-7-4zM3 11l7 4 7-4',
  tests: 'M4 4h12v12H4zM7 8l2 2 4-4M7 13h6',
  baselines: 'M5 17V3M5 4h10l-2 3 2 3H5',
  vv: 'M3 10a7 7 0 1014 0 7 7 0 10-14 0M7 10l2 2 4-4',
  evidence: 'M14 9l-5 5a3 3 0 01-4.2-4.2L10.5 4a2 2 0 012.8 2.8L8 12.1',
  matrix: 'M3 3h14v14H3zM3 8h14M3 13h14M8 3v14M13 3v14',
  impact: 'M10 10m-1.5 0a1.5 1.5 0 103 0 1.5 1.5 0 10-3 0M5 10a5 5 0 0110 0M2 10a8 8 0 0116 0',
  review: 'M2 10s3-6 8-6 8 6 8 6-3 6-8 6-8-6-8-6zM10 10m-2 0a2 2 0 104 0 2 2 0 10-4 0',
  board: 'M3 3h4v14H3zM8 3h4v9H8zM13 3h4v11h-4z',
  todos: 'M3 5l1.5 1.5L7 4M3 11l1.5 1.5L7 10M9 5h8M9 11h8M9 16h8M4 16h1',
  agents: 'M10 3l1.5 4.5L16 9l-4.5 1.5L10 15l-1.5-4.5L4 9l4.5-1.5z',
  automations: 'M11 2L4 11h5l-1 7 7-9h-5z',
  'agent-runs': 'M6 4l10 6-10 6z',
  activity: 'M2 10h4l2-5 4 10 2-5h4',
  settings:
    'M7.5 10a2.5 2.5 0 105 0 2.5 2.5 0 10-5 0M10 2v2M10 16v2M2 10h2M16 10h2M4.3 4.3l1.4 1.4M14.3 14.3l1.4 1.4M4.3 15.7l1.4-1.4M14.3 5.7l1.4-1.4',
};

/** Icon paths for the rail's group buttons. */
const GROUP_ICONS: Record<NavGroup, string> = {
  Define: 'M4 16l1-4 8-8 3 3-8 8-4 1zM11 6l3 3',
  Verify: 'M10 2l6 3v5c0 4-3 6.5-6 8-3-1.5-6-4-6-8V5zM7 10l2 2 4-4',
  Plan: 'M3 3h4v14H3zM8 3h4v9H8zM13 3h4v11h-4z',
  Agents: PAGE_ICONS.agents,
};

/** The icons of the shell itself, outside the page list. */
export const SHELL_ICONS = {
  projects: 'M3 3h6v6H3zM11 3h6v6h-6zM3 11h6v6H3zM11 11h6v6h-6z',
  more: 'M4 6h12M4 10h12M4 14h12',
  help: 'M3 10a7 7 0 1014 0 7 7 0 10-14 0M7.8 7.8a2.3 2.3 0 114 1.6c-.8.7-1.8 1.1-1.8 2.3M10 14.2v.1',
};

/** A generic page mark for a page this file does not know yet. */
const FALLBACK = 'M5 3h7l3 3v11H5zM12 3v3h3';

export const pageIconPath = (path: string): string => PAGE_ICONS[path] ?? FALLBACK;

export const groupIconPath = (group: NavGroup): string => GROUP_ICONS[group];

export const NavIcon: React.FC<{ d: string; size?: number }> = ({ d, size = 18 }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 20 20"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.6}
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path d={d} />
  </svg>
);

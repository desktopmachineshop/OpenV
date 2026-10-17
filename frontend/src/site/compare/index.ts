// The comparison pages, in the order the hub lists them (types.ts says
// what a comparison is and the rules it keeps). App.tsx routes each path by
// hand (K13); the sitemap, llms.txt and pre-rendering read the list here.
import { DOORS } from './doors';
import { JAMA } from './jama';
import { JIRA } from './jira';
import { POLARION } from './polarion';
import { REQVIEW } from './reqview';
import type { Comparison } from './types';

export type { Comparison, CompareRow } from './types';
export { OPENV, OPENV_LIMITS } from './openv';

export const COMPARISONS: readonly Comparison[] = [DOORS, JAMA, POLARION, REQVIEW, JIRA];

export const getComparison = (path: string): Comparison | undefined => COMPARISONS.find((c) => c.path === path);

export const HUB = {
  path: '/compare',
  title: 'Compare requirements management tools',
  summary:
    'Hosted OpenV beside DOORS Next, Jama Connect, Polarion, ReqView and Jira on price, hosting, tests, ReqIF and AI, from each vendor’s own pages.',
  heading: 'OpenV compared with other requirements management tools',
  answer:
    'This page sets hosted OpenV beside five tools buyers weigh it against: four hosted services (IBM DOORS Next, Jama Connect, Siemens Polarion X and Jira) and the ReqView desktop app. Every statement about another product comes from its vendor’s own pages, checked on the date below, and each comparison says when the other tool is the better choice. OpenV is free to start today; the enterprise tools are priced by quote.',
  updated: '2026-10-16',
};

/** One row of the hub's matrix: a short value per tool, OpenV first. */
export interface HubRow {
  dimension: string;
  values: Record<'openv' | 'doors' | 'jama' | 'polarion' | 'reqview' | 'jira', string>;
}

export const HUB_COLUMNS: { key: keyof HubRow['values']; label: string; path?: string }[] = [
  { key: 'openv', label: 'OpenV' },
  { key: 'doors', label: 'DOORS Next', path: DOORS.path },
  { key: 'jama', label: 'Jama Connect', path: JAMA.path },
  { key: 'polarion', label: 'Polarion X', path: POLARION.path },
  { key: 'reqview', label: 'ReqView', path: REQVIEW.path },
  { key: 'jira', label: 'Jira', path: JIRA.path },
];

// Short forms of the rows on each comparison page; the page carries the
// vendor link for every value here.
export const HUB_ROWS: HubRow[] = [
  {
    dimension: 'Published price',
    values: { openv: 'Free for 1 user', doors: 'Quote', jama: 'Quote', polarion: 'Quote', reqview: 'From €440/user/yr', jira: 'Free to 10 users' },
  },
  {
    dimension: 'Vendor-hosted',
    values: { openv: 'Yes', doors: 'Yes (ELM SaaS)', jama: 'Yes', polarion: 'Yes (Polarion X)', reqview: 'Desktop app', jira: 'Yes' },
  },
  {
    dimension: 'Test management',
    values: { openv: 'Built in', doors: 'Separate IBM product', jama: 'Built in', polarion: 'In ALM and QA', reqview: 'Linked test cases', jira: 'Marketplace apps' },
  },
  {
    dimension: 'ReqIF',
    values: { openv: 'Import and export', doors: 'Import and export', jama: 'Via Interchange', polarion: 'Built in', reqview: 'Import and export', jira: 'Third-party apps' },
  },
  {
    dimension: 'Word or Excel import',
    values: { openv: 'No', doors: 'Yes', jama: 'Yes', polarion: 'Yes', reqview: 'Yes', jira: 'Not stated' },
  },
  {
    dimension: 'Electronic signatures',
    values: { openv: 'No', doors: 'DOORS classic', jama: 'Signed reviews', polarion: 'Yes', reqview: 'Not stated', jira: 'Marketplace apps' },
  },
  {
    dimension: 'AI',
    values: { openv: 'Agents, your AI subscription', doors: 'Quality suggestions', jama: 'Advisor add-on', polarion: 'Copilot add-on', reqview: 'Not stated', jira: 'Rovo' },
  },
];

/** Tools named here with one sourced line each; no page of their own yet. */
export const OTHERS: { name: string; line: string; source: string }[] = [
  {
    name: 'PTC Codebeamer',
    line: 'An ALM platform with test management, also offered as SaaS (Codebeamer+); priced by quote.',
    source: 'https://www.ptc.com/en/products/codebeamer',
  },
  {
    name: 'Visure Requirements ALM',
    line: 'A requirements platform with test management, also offered as Visure Cloud; priced by quote.',
    source: 'https://visuresolutions.com/pricing',
  },
  {
    name: 'Perforce ALM',
    line: 'Formerly Helix ALM, with test case management; priced by quote.',
    source: 'https://www.perforce.com/blog/alm/helix-alm-now-perforce-alm',
  },
  {
    name: 'Modern Requirements4DevOps',
    line: 'Requirements management that runs inside Azure DevOps; priced by quote.',
    source: 'https://www.modernrequirements.com/pricing/',
  },
  {
    name: 'Matrix Req',
    line: 'Priced by quote, with cloud hosting included in the subscription.',
    source: 'https://matrixone.health/get-pricing',
  },
  {
    name: 'Inflectra SpiraTeam',
    line: 'Test management built in, with a published price list and a cloud hosted by Inflectra.',
    source: 'https://inflectra.com/Purchase/Price-List.aspx',
  },
];

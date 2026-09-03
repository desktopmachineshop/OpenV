// The in-app user manual: an ordered registry of chapters. Content is plain
// markdown shipped as TS modules (CRA cannot raw-import .md files), rendered
// with react-markdown in the Manual view. Chapter slugs are stable — they are
// deep-linkable as /manual/<slug> so features can link to specific chapters.
import gettingStarted from './chapters/gettingStarted';
import projects from './chapters/projects';
import productOverview from './chapters/productOverview';
import artifacts from './chapters/artifacts';
import links from './chapters/links';
import guidedWizard from './chapters/guidedWizard';
import board from './chapters/board';
import agents from './chapters/agents';
import crews from './chapters/crews';
import runsRunners from './chapters/runsRunners';
import automations from './chapters/automations';
import interviews from './chapters/interviews';
import vv from './chapters/vv';
import orgSettings from './chapters/orgSettings';
import troubleshooting from './chapters/troubleshooting';

export interface ManualChapter {
  /** URL segment: /manual/<slug>. Stable — used for deep links. */
  slug: string;
  /** Short title shown in the sidebar TOC. */
  title: string;
  /** The chapter body, markdown (GFM). */
  content: string;
}

export const MANUAL_CHAPTERS: ManualChapter[] = [
  { slug: 'getting-started', title: 'Getting started', content: gettingStarted },
  { slug: 'projects', title: 'Projects & members', content: projects },
  { slug: 'product-overview', title: 'Product Overview', content: productOverview },
  { slug: 'artifacts', title: 'Requirements & artifacts', content: artifacts },
  { slug: 'links', title: 'Traceability links', content: links },
  { slug: 'guided-wizard', title: 'Guided definition & the V&V Assistant', content: guidedWizard },
  { slug: 'board', title: 'Kanban board', content: board },
  { slug: 'agents', title: 'Agents', content: agents },
  { slug: 'crews', title: 'Crews', content: crews },
  { slug: 'runs-runners', title: 'Runs & runners', content: runsRunners },
  { slug: 'automations', title: 'Automations', content: automations },
  { slug: 'interviews', title: 'Interviews', content: interviews },
  { slug: 'vv', title: 'V&V & test runs', content: vv },
  { slug: 'org-settings', title: 'Workspace settings & teams', content: orgSettings },
  { slug: 'troubleshooting', title: 'Troubleshooting & FAQ', content: troubleshooting },
];

export const getChapter = (slug: string): ManualChapter | undefined =>
  MANUAL_CHAPTERS.find((c) => c.slug === slug);

/** Slugify a heading for in-page anchors (mirrors the Manual view's ids). */
export const headingId = (text: string): string =>
  text
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, '')
    .trim()
    .replace(/\s+/g, '-');

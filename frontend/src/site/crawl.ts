// What search engines and AI crawlers are told about the site: robots.txt,
// sitemap.xml and llms.txt (docs/plans/search-and-ai-visibility.md, 4.1).
//
// The build writes the three files from this module (vite.config.ts) with
// SITE_URL_PLACEHOLDER where the site's address goes, and the container fills
// it in at start (docker-entrypoint.d/46-openv-crawl.sh) from OPENV_SITE_URL.
// The address is a run-time setting because one image serves openv.app,
// staging and every self-hosted copy, and only the first should advertise a
// sitemap of openv.app.
//
// The page list is derived from the page registry and the manual, so a new
// public page reaches the sitemap without a second list to keep in step;
// crawl.test.ts fails when one has no summary below.
import { MANUAL_CHAPTERS } from '../manual';
import { PAGES, pagePattern } from '../pages';
import { COMPARISONS, HUB } from './compare';
import { TOPICS } from './topics';

export const SITE_URL_PLACEHOLDER = '__OPENV_SITE_URL__';

/** One sentence, the same everywhere OpenV describes itself (plan, section 3). */
export const DESCRIPTOR =
  'OpenV is a requirements management and V&V platform: traceability from need to test evidence, baselines, ReqIF exchange and AI agents, hosted or self-hosted.';

/**
 * Crawlers named in robots.txt. They share the one group with `*`, so naming
 * them changes nothing about what they may fetch; it states that search and
 * AI crawlers are welcome rather than leaving it to a default.
 */
export const CRAWLERS = [
  'Googlebot',
  'Bingbot',
  'OAI-SearchBot',
  'ChatGPT-User',
  'GPTBot',
  'Claude-SearchBot',
  'Claude-User',
  'ClaudeBot',
  'PerplexityBot',
  'Perplexity-User',
  'Google-Extended',
];

/**
 * Paths no crawler needs: the API, everything behind a sign-in, and the
 * links that carry a one-off token. Share links are deliberately absent: the
 * API's page for one says noindex itself, and a crawler blocked from
 * fetching it would never read that.
 */
export const DISALLOWED_PREFIXES = [
  '/api/',
  '/projects',
  '/org/',
  '/admin',
  '/whats-new',
  '/interview/',
  '/verify-email',
  '/reset-password',
  // The consent an AI app asks for (REQ-44), and the OAuth and MCP paths
  // of the API this origin also serves.
  '/connect',
  '/oauth/',
  '/mcp',
];

/** Public pages that are not content: a form, a redirect, a token link. */
export const UNLISTED_PAGES = new Set(['/login', '/verify-email', '/reset-password', '/security']);

export interface CrawlPage {
  path: string;
  /**
   * The address search engines should index this page's content under: the
   * page's own path, or another page that shows the same content.
   */
  canonical: string;
  title: string;
  summary?: string;
}

/**
 * Public pages whose content is another page's: /pricing is the front page
 * scrolled to its plans, /manual is the first chapter. They are pre-rendered
 * and linked like any page, but name the other as canonical, and only the
 * other is in the sitemap.
 */
export const CANONICAL_PATHS: Record<string, string> = {
  '/pricing': '/',
  '/manual': `/manual/${MANUAL_CHAPTERS[0].slug}`,
};

/** The site's own pages, by path: what each is called and what it holds. */
export const SITE_PAGE_TEXT: Record<string, { title: string; summary: string }> = {
  '/': {
    title: 'OpenV',
    summary: 'What OpenV is: requirements, traceability and V&V evidence, with AI agents that work inside the audit trail.',
  },
  '/pricing': {
    title: 'Pricing',
    summary: 'The hosted plans, free self-hosting under the MIT License, and the free alpha.',
  },
  '/how-it-works': {
    title: 'How it works',
    summary: 'One graph from need to evidence: the artifact graph, the agent loop, where things run and who can see what.',
  },
  '/demos': {
    title: 'Demo videos',
    summary: 'Short narrated recordings of OpenV working on its own requirements project.',
  },
  '/faq': {
    title: 'FAQ',
    summary: 'Security, data separation, AI data handling, self-hosting, plans, export and ReqIF exchange with DOORS and Polarion.',
  },
  '/privacy': {
    title: 'Privacy policy',
    summary: 'What the hosted OpenV service collects, why, who it is shared with, how long it is kept, and how to reach us.',
  },
  '/open-source': {
    title: 'Open-source projects',
    summary: 'Open-source projects hosted free on OpenV, each published as of its latest baseline.',
  },
  '/customers': {
    title: 'Customer stories',
    summary: 'Teams using OpenV and what changed for them.',
  },
  '/white-papers': {
    title: 'White papers',
    summary: 'Longer reads on requirements, traceability, verification and AI agents in a regulated process.',
  },
  '/changelog': {
    title: 'Changelog',
    summary: 'Every OpenV release, newest first: new features, security fixes, maintenance updates and bug fixes.',
  },
  '/manual': {
    title: 'User manual',
    summary: 'The OpenV user manual, chapter by chapter.',
  },
  // The topic pages carry their own title and summary (site/topics).
  [HUB.path]: { title: HUB.title, summary: HUB.summary },
  ...Object.fromEntries(COMPARISONS.map((c) => [c.path, { title: c.title, summary: c.summary }])),
  ...Object.fromEntries(TOPICS.map((t) => [t.path, { title: t.title, summary: t.summary }])),
};

/** Every public page with content, site pages first, then the manual. */
export function crawlPages(): CrawlPage[] {
  const site = PAGES.filter((p) => p.scope === 'site' && p.public && !p.path.includes(':'))
    .map(pagePattern)
    .filter((path) => !UNLISTED_PAGES.has(path))
    .map((path) => {
      const text = SITE_PAGE_TEXT[path];
      if (!text) throw new Error(`crawl: public page ${path} has no entry in SITE_PAGE_TEXT`);
      return { path, canonical: CANONICAL_PATHS[path] ?? path, ...text };
    });
  const manual = MANUAL_CHAPTERS.map((c) => {
    const path = `/manual/${c.slug}`;
    return { path, canonical: path, title: c.title };
  });
  return [...site, ...manual];
}

const url = (path: string) => `${SITE_URL_PLACEHOLDER}${path}`;

/**
 * robots.txt. The Sitemap line is the one the entrypoint drops when no site
 * address is configured, so it stays the last line on its own.
 */
export function robotsTxt(): string {
  return [
    '# Search and AI crawlers are welcome on the public site.',
    ...CRAWLERS.map((c) => `User-agent: ${c}`),
    'User-agent: *',
    'Allow: /',
    ...DISALLOWED_PREFIXES.map((p) => `Disallow: ${p}`),
    '',
    `Sitemap: ${SITE_URL_PLACEHOLDER}/sitemap.xml`,
    '',
  ].join('\n');
}

/** The sitemap lists canonical addresses only (CANONICAL_PATHS). */
export function sitemapXml(): string {
  const entries = crawlPages()
    .filter((p) => p.canonical === p.path)
    .map((p) => `  <url><loc>${url(p.path)}</loc></url>`);
  return [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">',
    ...entries,
    '</urlset>',
    '',
  ].join('\n');
}

/** llms.txt (llmstxt.org): a markdown map of the site for language models. */
export function llmsTxt(): string {
  const link = (p: CrawlPage) => `- [${p.title}](${url(p.path)})${p.summary ? `: ${p.summary}` : ''}`;
  const pages = crawlPages();
  return [
    '# OpenV',
    '',
    `> ${DESCRIPTOR}`,
    '',
    'OpenV is open source under the MIT License: self-hosting is free at any scale with every feature, and the source is on GitHub at https://github.com/desktopmachineshop/OpenV. The hosted service is in a free alpha. Projects export as PDF, Word, JSON, CSV, Excel and ReqIF on every plan.',
    '',
    '## Site',
    '',
    ...pages.filter((p) => !p.path.startsWith('/manual/')).map(link),
    '',
    '## Manual',
    '',
    ...pages.filter((p) => p.path.startsWith('/manual/')).map(link),
    '',
  ].join('\n');
}

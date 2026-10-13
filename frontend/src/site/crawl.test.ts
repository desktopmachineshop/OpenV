import { MANUAL_CHAPTERS } from '../manual';
import { PAGES, pagePattern } from '../pages';
import {
  CANONICAL_PATHS,
  CRAWLERS,
  DISALLOWED_PREFIXES,
  SITE_PAGE_TEXT,
  SITE_URL_PLACEHOLDER,
  UNLISTED_PAGES,
  crawlPages,
  llmsTxt,
  robotsTxt,
  sitemapXml,
} from './crawl';

const disallowed = (path: string) => DISALLOWED_PREFIXES.some((p) => path === p || path.startsWith(p));

describe('crawlPages', () => {
  it('lists every public site page without a parameter, unless it is unlisted on purpose', () => {
    const listed = new Set(crawlPages().map((p) => p.path));
    const publicPages = PAGES.filter((p) => p.scope === 'site' && p.public && !p.path.includes(':')).map(pagePattern);
    for (const path of publicPages) {
      expect(listed.has(path) || UNLISTED_PAGES.has(path)).toBe(true);
    }
  });

  it('lists every manual chapter', () => {
    const listed = crawlPages().map((p) => p.path);
    for (const c of MANUAL_CHAPTERS) expect(listed).toContain(`/manual/${c.slug}`);
  });

  it('keeps no summary for a page the registry no longer has', () => {
    const patterns = new Set(PAGES.map(pagePattern));
    for (const path of Object.keys(SITE_PAGE_TEXT)) expect(patterns.has(path)).toBe(true);
  });

  it('lists nothing robots.txt disallows', () => {
    for (const p of crawlPages()) expect(disallowed(p.path)).toBe(false);
  });
});

describe('canonical addresses', () => {
  it('points each alias at a page that is its own canonical, and lists only canonical pages in the sitemap', () => {
    const pages = crawlPages();
    const own = new Set(pages.filter((p) => p.canonical === p.path).map((p) => p.path));
    for (const [alias, target] of Object.entries(CANONICAL_PATHS)) {
      expect(pages.map((p) => p.path)).toContain(alias);
      expect(own.has(target)).toBe(true);
      expect(sitemapXml()).not.toContain(`<loc>${SITE_URL_PLACEHOLDER}${alias}</loc>`);
    }
  });
});

describe('robotsTxt', () => {
  const robots = robotsTxt();

  it('disallows every page that needs a sign-in', () => {
    for (const page of PAGES.filter((p) => !p.public)) {
      expect(disallowed(pagePattern(page))).toBe(true);
    }
  });

  it('names each crawler in the one group, so all of them get the same rules', () => {
    const lines = robots.split('\n');
    const firstRule = lines.findIndex((l) => l.startsWith('Allow:'));
    const agents = lines.slice(0, firstRule).filter((l) => l.startsWith('User-agent:'));
    expect(agents).toEqual([...CRAWLERS, '*'].map((c) => `User-agent: ${c}`));
    expect(lines.slice(firstRule).some((l) => l.startsWith('User-agent:'))).toBe(false);
  });

  it('ends with the Sitemap line, which the entrypoint drops when no site address is set', () => {
    expect(robots.trimEnd().split('\n').pop()).toBe(`Sitemap: ${SITE_URL_PLACEHOLDER}/sitemap.xml`);
  });
});

describe('sitemapXml', () => {
  it('is a well-formed sitemap of absolute URLs under the site address', () => {
    const doc = new DOMParser().parseFromString(sitemapXml(), 'application/xml');
    expect(doc.getElementsByTagName('parsererror')).toHaveLength(0);
    const locs = Array.from(doc.getElementsByTagName('loc')).map((n) => n.textContent ?? '');
    expect(locs).toHaveLength(crawlPages().filter((p) => p.canonical === p.path).length);
    expect(locs).toContain(`${SITE_URL_PLACEHOLDER}/`);
    expect(locs).toContain(`${SITE_URL_PLACEHOLDER}/faq`);
    for (const loc of locs) expect(loc.startsWith(`${SITE_URL_PLACEHOLDER}/`)).toBe(true);
  });
});

describe('llmsTxt', () => {
  const llms = llmsTxt();

  it('opens with the name and the descriptor, as llmstxt.org lays it out', () => {
    const [h1, , quote] = llms.split('\n');
    expect(h1).toBe('# OpenV');
    expect(quote.startsWith('> OpenV is a requirements management and V&V platform')).toBe(true);
  });

  it('says what the licence is, and never that OpenV is open source', () => {
    expect(llms).toContain('source-available under the Elastic License 2.0');
    expect(llms).not.toMatch(/OpenV is open[- ]source/i);
  });

  it('links every page it lists', () => {
    for (const p of crawlPages()) expect(llms).toContain(`(${SITE_URL_PLACEHOLDER}${p.path})`);
  });
});

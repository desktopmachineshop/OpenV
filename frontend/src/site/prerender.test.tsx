import fs from 'node:fs';
import path from 'node:path';
import { LICENSE_URL } from '../landing/content';
import { FAQ_GROUPS } from './content';
import { CANONICAL_PATHS, SITE_URL_PLACEHOLDER, crawlPages } from './crawl';
import { HOME_TITLE, PrerenderedPage, pageDocument, prerenderedPages, renderBody, summarise } from './prerender';
import { TOPICS } from './topics';

const words = (html: string) =>
  html
    .replace(/<[^>]+>/g, ' ')
    .split(/\s+/)
    .filter(Boolean).length;

// react-router's Link reports useLayoutEffect once per server render; the
// build silences exactly that message, and so does this file.
const consoleError = console.error;
beforeAll(() => {
  console.error = (...args: unknown[]) => {
    if (typeof args[0] === 'string' && args[0].includes('useLayoutEffect does nothing on the server')) return;
    consoleError(...args);
  };
});
afterAll(() => {
  console.error = consoleError;
});

describe('renderBody', () => {
  let bodies = new Map<string, string>();
  beforeAll(() => {
    bodies = new Map(crawlPages().map((p) => [p.path, renderBody(p.path)]));
  });

  it('renders every public page with content, so none ships an empty snapshot', () => {
    for (const [path, body] of bodies) {
      expect({ path, words: words(body) >= 150 }).toEqual({ path, words: true });
    }
  });

  it('renders a different page for every canonical address', () => {
    const canonical = crawlPages().filter((p) => p.canonical === p.path);
    const seen = new Map<string, string>();
    for (const p of canonical) {
      const body = bodies.get(p.path) ?? '';
      expect({ path: p.path, same_as: seen.get(body) }).toEqual({ path: p.path, same_as: undefined });
      seen.set(body, p.path);
    }
  });

  it('renders nothing for a path no public page has', () => {
    expect(renderBody('/projects/123')).toBe('');
  });
});

describe('prerenderedPages', () => {
  const pages = prerenderedPages();
  const byPath = new Map(pages.map((p) => [p.path, p]));

  it('covers every public page, one file each', () => {
    expect(pages.map((p) => p.path)).toEqual(crawlPages().map((p) => p.path));
    expect(byPath.get('/')?.file).toBe('index.html');
    expect(byPath.get('/faq')?.file).toBe('faq.html');
    expect(byPath.get('/manual/vv')?.file).toBe('manual/vv.html');
    expect(new Set(pages.map((p) => p.file)).size).toBe(pages.length);
  });

  it('gives every page its own title and a description', () => {
    expect(byPath.get('/')?.title).toBe(HOME_TITLE);
    expect(byPath.get('/faq')?.title).toBe('FAQ · OpenV');
    expect(byPath.get('/manual/vv')?.title).toBe('V&V & test runs · OpenV manual');
    expect(new Set(pages.map((p) => p.title)).size).toBe(pages.length);
    for (const p of pages) expect(p.description.length).toBeGreaterThan(20);
  });

  it('names the canonical address of a page that shows another page', () => {
    for (const [alias, target] of Object.entries(CANONICAL_PATHS)) {
      expect(byPath.get(alias)?.canonical).toBe(target);
    }
    expect(byPath.get('/faq')?.canonical).toBe('/faq');
  });

  it('describes OpenV on the front page, every FAQ answer on the FAQ and each topic page as an article, as structured data', () => {
    const home = byPath.get('/')?.jsonLd[0] as { '@graph': Array<Record<string, unknown>> };
    const app = home['@graph'].find((n) => n['@type'] === 'SoftwareApplication');
    expect(app).toMatchObject({ name: 'OpenV', license: LICENSE_URL, url: `${SITE_URL_PLACEHOLDER}/` });
    const faq = byPath.get('/faq')?.jsonLd[0] as { mainEntity: unknown[] };
    expect(faq.mainEntity).toHaveLength(FAQ_GROUPS.flatMap((g) => g.entries).length);
    const topics = new Set(TOPICS.map((t) => t.path));
    for (const p of pages.filter((p) => p.path !== '/' && p.path !== '/faq' && !topics.has(p.path))) expect(p.jsonLd).toEqual([]);
    for (const t of TOPICS) {
      const data = byPath.get(t.path)?.jsonLd[0] as Record<string, unknown>;
      expect(data).toMatchObject({ '@type': t.kind === 'glossary' ? 'DefinedTermSet' : 'TechArticle', dateModified: t.updated, url: `${SITE_URL_PLACEHOLDER}${t.path}` });
    }
  });
});

describe('summarise', () => {
  it('takes the first paragraph of prose, without markdown, at most about 160 characters', () => {
    expect(summarise('# Title\n\nThe **V&V** view rolls up [coverage](/x).\n\nMore.')).toBe('The V&V view rolls up coverage.');
    expect(summarise('## Only\n\n- a list\n\n| a table |')).toBe('');
    const long = summarise(`${'word '.repeat(60)}end`);
    expect(long.length).toBeLessThanOrEqual(160);
    expect(long.endsWith('…')).toBe(true);
  });
});

describe('pageDocument', () => {
  const template = fs.readFileSync(path.join(__dirname, '../../index.html'), 'utf8');
  const page: PrerenderedPage = {
    path: '/faq',
    canonical: '/faq',
    file: 'faq.html',
    title: 'FAQ & "more" · OpenV',
    description: 'Answers <about> things & stuff',
    jsonLd: [{ '@type': 'Thing', name: '</script><script>alert(1)</script>' }],
  };
  const html = pageDocument(template, page, '<main>Body</main>');

  it('works on the app shell as it stands, so the build fails if the shell drifts', () => {
    expect(html).toContain('<title>FAQ &amp; "more" · OpenV</title>');
    expect(html).toContain('<meta name="description" content="Answers &lt;about&gt; things &amp; stuff"');
    expect(html).toContain('<meta property="og:title" content="FAQ &amp; &quot;more&quot; · OpenV"');
    expect(html).toContain('<meta name="twitter:description" content="Answers &lt;about&gt; things &amp; stuff"');
    expect(() => pageDocument('<html><head></head><body></body></html>', page, '')).toThrow(/index.html has nothing matching/);
  });

  it('puts the snapshot before an empty #root, in place of the noscript notice', () => {
    expect(html).toMatch(/<div id="prerender"><main>Body<\/main><\/div>\s*<div id="root"><\/div>/);
    expect(html).not.toContain('<noscript>');
  });

  it('keeps each line that carries the site address to one tag, so the entrypoint can drop it whole', () => {
    const lines = html.split('\n').filter((l) => l.includes(SITE_URL_PLACEHOLDER));
    expect(lines.map((l) => l.trim().slice(0, 30))).toEqual([
      `<link rel="canonical" href="${SITE_URL_PLACEHOLDER}`.slice(0, 30),
      `<meta property="og:url" conten`,
    ]);
    expect(html).toContain(`<link rel="canonical" href="${SITE_URL_PLACEHOLDER}/faq" />`);
  });

  it('writes structured data on one line that cannot close its own script element', () => {
    const line = html.split('\n').find((l) => l.includes('application/ld+json')) ?? '';
    expect(line).toContain('\\u003c/script>');
    expect(line.match(/<\/script>/g)).toHaveLength(1);
  });
});

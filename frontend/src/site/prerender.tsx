// The public site rendered ahead of time (docs/plans/search-and-ai-visibility.md,
// 4.2). Crawlers that do not run JavaScript (most AI crawlers among them)
// otherwise see an empty <div id="root">.
//
// The build renders every page the sitemap lists with react-dom/server
// (vite.config.ts) into a copy of the built index.html: the page's markup in
// a #prerender element beside #root, and its own title, description,
// canonical address and structured data in the head. The container fills in
// the site's address at start (docker-entrypoint.d/46-openv-crawl.sh) and
// nginx serves the copy for that path. In the browser the snapshot stays on
// screen, and #root hidden (index.css), until the live page has mounted and
// calls releasePrerender (prerenderHandoff.ts), so a visitor never sees the
// app's "Loading…" between the two.
//
// Only pages that need no session and render from bundled content belong
// here; what a page fetches after mounting is absent from its snapshot.
import { renderToString } from 'react-dom/server';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { LICENSE_URL, REPO_URL } from '../landing/content';
import { MANUAL_CHAPTERS } from '../manual';
import { Landing } from '../views/Landing';
import { ManualView } from '../views/ManualView';
import { FAQ_GROUPS } from './content';
import { Customers } from './Customers';
import { Changelog } from './Changelog';
import { DESCRIPTOR, SITE_PAGE_TEXT, SITE_URL_PLACEHOLDER, crawlPages } from './crawl';
import { Demos } from './Demos';
import { Faq } from './Faq';
import { HowItWorks } from './HowItWorks';
import { OpenSourceProjects } from './OpenSourceProjects';
import { TopicPage } from './TopicPage';
import { TOPICS, getTopic } from './topics';
import { WhitePapers } from './WhitePapers';

/**
 * The element App.tsx routes each public path to. App's route table stays
 * hand-written (K13); prerender.test.ts holds this one to the sitemap, so a
 * listed page without an element here fails there rather than shipping an
 * empty snapshot.
 */
export const PRERENDER_ROUTES: { path: string; element: JSX.Element }[] = [
  { path: '/', element: <Landing /> },
  { path: '/pricing', element: <Landing section="pricing" /> },
  { path: '/how-it-works', element: <HowItWorks /> },
  { path: '/demos', element: <Demos /> },
  { path: '/faq', element: <Faq /> },
  { path: '/open-source', element: <OpenSourceProjects /> },
  { path: '/customers', element: <Customers /> },
  { path: '/white-papers', element: <WhitePapers /> },
  { path: '/changelog', element: <Changelog /> },
  { path: '/manual', element: <ManualView /> },
  { path: '/manual/:chapterSlug', element: <ManualView /> },
  ...TOPICS.map((t) => ({ path: t.path, element: <TopicPage path={t.path} /> })),
];

export const HOME_TITLE = 'OpenV — requirements, traceability and V&V with AI agents';

export interface PrerenderedPage {
  path: string;
  /** The address the canonical link and og:url name (CANONICAL_PATHS). */
  canonical: string;
  /** Where the snapshot is written, relative to the snapshot directory. */
  file: string;
  title: string;
  description: string;
  /** Structured data for the head, each object one JSON-LD script. */
  jsonLd: object[];
}

export function renderBody(path: string): string {
  return renderToString(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        {PRERENDER_ROUTES.map((r) => (
          <Route key={r.path} path={r.path} element={r.element} />
        ))}
      </Routes>
    </MemoryRouter>,
  );
}

const absolute = (path: string) => `${SITE_URL_PLACEHOLDER}${path}`;

/** The first paragraph of prose in a chapter, as plain text of at most about 160 characters. */
export function summarise(markdown: string): string {
  const paragraph =
    markdown
      .split(/\n\s*\n/)
      .map((p) => p.trim())
      .find((p) => p && !/^(#|```|\||[-*] |\d+\. |>|!\[)/.test(p)) ?? '';
  const text = paragraph
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/[`*_]/g, '')
    .replace(/\s+/g, ' ')
    .trim();
  if (text.length <= 160) return text;
  const cut = text.slice(0, 157);
  return `${cut.slice(0, cut.lastIndexOf(' '))}…`;
}

function homeJsonLd(): object {
  const org = `${absolute('/')}#organization`;
  return {
    '@context': 'https://schema.org',
    '@graph': [
      {
        '@type': 'Organization',
        '@id': org,
        name: 'Desktop Machine Shop',
        url: absolute('/'),
        logo: absolute('/icon-512.png'),
        sameAs: [REPO_URL],
      },
      {
        '@type': 'SoftwareApplication',
        name: 'OpenV',
        url: absolute('/'),
        description: DESCRIPTOR,
        applicationCategory: 'BusinessApplication',
        applicationSubCategory: 'Requirements management',
        operatingSystem: 'Web browser; self-hosted on Linux with Docker',
        isAccessibleForFree: true,
        license: LICENSE_URL,
        image: absolute('/Images/og.png'),
        publisher: { '@id': org },
        sameAs: [REPO_URL],
      },
    ],
  };
}

/** A topic page as a dated article by the organisation, for search engines and assistants. */
function topicJsonLd(path: string): object[] {
  const topic = getTopic(path);
  if (!topic) return [];
  return [
    {
      '@context': 'https://schema.org',
      '@type': topic.kind === 'glossary' ? 'DefinedTermSet' : 'TechArticle',
      name: topic.title,
      headline: topic.heading,
      description: topic.summary,
      url: absolute(path),
      dateModified: topic.updated,
      about: 'OpenV',
      publisher: { '@type': 'Organization', name: 'Desktop Machine Shop', url: absolute('/') },
      ...(topic.kind === 'glossary'
        ? {
            hasDefinedTerm: topic.sections.map((s) => ({
              '@type': 'DefinedTerm',
              name: s.heading,
              description: (s.paragraphs ?? []).join(' '),
            })),
          }
        : {}),
    },
  ];
}

function faqJsonLd(): object {
  return {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: FAQ_GROUPS.flatMap((g) => g.entries).map((e) => ({
      '@type': 'Question',
      name: e.q,
      acceptedAnswer: { '@type': 'Answer', text: e.a },
    })),
  };
}

/** Every public page with content (crawlPages), with what its head says about it. */
export function prerenderedPages(): PrerenderedPage[] {
  return crawlPages().map((page) => {
    const file = page.path === '/' ? 'index.html' : `${page.path.slice(1)}.html`;
    const { path, canonical } = page;
    if (path === '/') {
      return { path, canonical, file, title: HOME_TITLE, description: DESCRIPTOR, jsonLd: [homeJsonLd()] };
    }
    const chapter = MANUAL_CHAPTERS.find((c) => `/manual/${c.slug}` === page.path);
    if (chapter) {
      return {
        path,
        canonical,
        file,
        title: `${chapter.title} · OpenV manual`,
        description: summarise(chapter.content) || SITE_PAGE_TEXT['/manual'].summary,
        jsonLd: [],
      };
    }
    return {
      path,
      canonical,
      file,
      title: `${page.title} · OpenV`,
      description: page.summary ?? DESCRIPTOR,
      jsonLd: path === '/faq' ? [faqJsonLd()] : topicJsonLd(path),
    };
  });
}

const attr = (s: string) => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
const text = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

/** Replace one match of `pattern` in `html`, or fail: a template that has drifted must stop the build. */
function replaceOnce(html: string, pattern: RegExp, replacement: string): string {
  if (!pattern.test(html)) throw new Error(`prerender: index.html has nothing matching ${pattern}`);
  return html.replace(pattern, () => replacement);
}

const setContent = (html: string, selector: string, value: string) =>
  replaceOnce(html, new RegExp(`<meta ${selector} content="[^"]*"`), `<meta ${selector} content="${attr(value)}"`);

/**
 * The built index.html made into one page's document. The lines that carry
 * the site's address hold SITE_URL_PLACEHOLDER and nothing else that matters,
 * one tag per line, so the entrypoint can drop them whole when no address is
 * configured.
 */
export function pageDocument(template: string, page: PrerenderedPage, body: string): string {
  let html = template;
  html = replaceOnce(html, /<title>[^<]*<\/title>/, `<title>${text(page.title)}</title>`);
  html = setContent(html, 'name="description"', page.description);
  html = setContent(html, 'property="og:title"', page.title);
  html = setContent(html, 'property="og:description"', page.description);
  html = setContent(html, 'name="twitter:title"', page.title);
  html = setContent(html, 'name="twitter:description"', page.description);
  const url = absolute(page.canonical);
  const head = [
    `    <link rel="canonical" href="${attr(url)}" />`,
    `    <meta property="og:url" content="${attr(url)}" />`,
    ...page.jsonLd.map(
      (data) => `    <script type="application/ld+json">${JSON.stringify(data).replace(/</g, '\\u003c')}</script>`,
    ),
  ];
  html = replaceOnce(html, /<\/head>/, `${head.join('\n')}\n</head>`);
  html = replaceOnce(
    html,
    /<noscript>[\s\S]*?<\/noscript>\s*<div id="root"><\/div>/,
    `<div id="prerender">${body}</div>\n    <div id="root"></div>`,
  );
  return html;
}

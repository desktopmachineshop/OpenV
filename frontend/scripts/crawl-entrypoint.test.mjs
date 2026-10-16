// Tests for docker-entrypoint.d/46-openv-crawl.sh, the start-up step that
// turns the build's crawl templates into robots.txt, sitemap.xml and llms.txt,
// finishes the pre-rendered public pages, and writes the host-name map behind
// the noindex header.
// Run: cd frontend && node --test 'scripts/*.test.mjs'
//
// The script runs here as the image runs it, under sh, against a web root and
// an nginx config directory made for each test; the templates are small
// stand-ins carrying the placeholder, since src/site/crawl.test.ts covers what
// the real ones say.
import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const SCRIPT = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'docker-entrypoint.d', '46-openv-crawl.sh');
const PLACEHOLDER = '__OPENV_SITE_URL__';

const TEMPLATES = {
  'robots.txt': `User-agent: *\nAllow: /\nDisallow: /api/\n\nSitemap: ${PLACEHOLDER}/sitemap.xml\n`,
  'sitemap.xml': `<urlset><url><loc>${PLACEHOLDER}/</loc></url><url><loc>${PLACEHOLDER}/faq</loc></url></urlset>\n`,
  'llms.txt': `# OpenV\n\n- [FAQ](${PLACEHOLDER}/faq)\n`,
};

// A pre-rendered page as src/site/prerender.tsx writes it: the lines that
// carry the address hold the placeholder and one tag each.
const PAGE = [
  '<head>',
  `    <link rel="canonical" href="${PLACEHOLDER}/faq" />`,
  `    <meta property="og:url" content="${PLACEHOLDER}/faq" />`,
  `    <script type="application/ld+json">{"url":"${PLACEHOLDER}/"}</script>`,
  '</head>',
  '<div id="prerender"><h1>FAQ</h1></div>',
  '',
].join('\n');

function setup({ templates = true } = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'openv-crawl-'));
  const root = path.join(dir, 'html');
  const conf = path.join(dir, 'openv');
  fs.mkdirSync(path.join(root, '.openv-crawl'), { recursive: true });
  fs.mkdirSync(conf);
  if (templates) {
    for (const [name, body] of Object.entries(TEMPLATES)) fs.writeFileSync(path.join(root, '.openv-crawl', name), body);
    fs.mkdirSync(path.join(root, '.openv-prerender', 'manual'), { recursive: true });
    fs.writeFileSync(path.join(root, '.openv-prerender', 'faq.html'), PAGE);
    fs.writeFileSync(path.join(root, '.openv-prerender', 'manual', 'vv.html'), PAGE);
  }
  const run = (env = {}) =>
    execFileSync('sh', [SCRIPT], {
      env: { PATH: process.env.PATH, OPENV_WEB_ROOT: root, OPENV_NGINX_CONF_DIR: conf, ...env },
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    });
  const read = (name) => {
    const file = path.join(root, name);
    return fs.existsSync(file) ? fs.readFileSync(file, 'utf8') : null;
  };
  const map = () => fs.readFileSync(path.join(conf, 'robots-host.conf'), 'utf8');
  return { run, read, map };
}

test('with OPENV_SITE_URL, the three files carry the address and other host names are noindex', () => {
  const { run, read, map } = setup();
  run({ OPENV_SITE_URL: 'https://OpenV.app/' });
  assert.equal(read('robots.txt'), 'User-agent: *\nAllow: /\nDisallow: /api/\n\nSitemap: https://OpenV.app/sitemap.xml\n');
  assert.equal(read('sitemap.xml'), '<urlset><url><loc>https://OpenV.app/</loc></url><url><loc>https://OpenV.app/faq</loc></url></urlset>\n');
  assert.equal(read('llms.txt'), '# OpenV\n\n- [FAQ](https://OpenV.app/faq)\n');
  assert.match(map(), /default "noindex, nofollow";/);
  assert.match(map(), /^ {4}"openv\.app" "";$/m);
});

test('a port in the address stays in the files and out of the host-name map', () => {
  const { run, read, map } = setup();
  run({ OPENV_SITE_URL: 'http://reqs.example.com:8443' });
  assert.match(read('robots.txt'), /^Sitemap: http:\/\/reqs\.example\.com:8443\/sitemap\.xml$/m);
  assert.match(map(), /^ {4}"reqs\.example\.com" "";$/m);
});

test('without OPENV_SITE_URL, robots.txt has no Sitemap line and nothing else is published', () => {
  const { run, read, map } = setup();
  run();
  assert.equal(read('robots.txt'), 'User-agent: *\nAllow: /\nDisallow: /api/\n\n');
  assert.equal(read('sitemap.xml'), null);
  assert.equal(read('llms.txt'), null);
  assert.match(map(), /default "";/);
  assert.doesNotMatch(map(), /noindex/);
});

test('OPENV_NOINDEX wins: everything is fetchable, so the noindex header is seen, and nothing is advertised', () => {
  const { run, read, map } = setup();
  run({ OPENV_NOINDEX: '1', OPENV_SITE_URL: 'https://staging.openv.app' });
  assert.match(read('robots.txt'), /^User-agent: \*\nAllow: \/$/m);
  assert.doesNotMatch(read('robots.txt'), /Disallow|Sitemap/);
  assert.equal(read('sitemap.xml'), null);
  assert.equal(read('llms.txt'), null);
  // 45-openv-noindex.sh sends the header on every host; a second one here
  // would only repeat it.
  assert.match(map(), /default "";/);
});

test('OPENV_NOINDEX set to an off value is treated as unset, as 45-openv-noindex.sh treats it', () => {
  for (const off of ['0', 'false', 'off', 'no']) {
    const { run, read } = setup();
    run({ OPENV_NOINDEX: off, OPENV_SITE_URL: 'https://openv.app' });
    assert.notEqual(read('sitemap.xml'), null, `OPENV_NOINDEX=${off}`);
  }
});

test('an address that is not scheme://host is refused and treated as unset', () => {
  for (const bad of ['openv.app', 'https://openv.app/path', 'https://open|v.app', 'https://openv.app" ""; default "', 'ftp://openv.app']) {
    const { run, read, map } = setup();
    run({ OPENV_SITE_URL: bad });
    assert.equal(read('sitemap.xml'), null, bad);
    assert.doesNotMatch(read('robots.txt'), /Sitemap/, bad);
    assert.match(map(), /default "";/, bad);
  }
});

test('a restart in another mode removes what the previous start published', () => {
  const { run, read } = setup();
  run({ OPENV_SITE_URL: 'https://openv.app' });
  assert.notEqual(read('llms.txt'), null);
  run();
  assert.equal(read('sitemap.xml'), null);
  assert.equal(read('llms.txt'), null);
});

test('a build without the templates publishes nothing and leaves every host indexable', () => {
  const { run, read, map } = setup({ templates: false });
  run({ OPENV_SITE_URL: 'https://openv.app' });
  assert.equal(read('robots.txt'), null);
  assert.equal(read('sitemap.xml'), null);
  assert.equal(read('prerendered/faq.html'), null);
  assert.match(map(), /default "";/);
});

test('pre-rendered pages are finished with the address when OPENV_SITE_URL is set', () => {
  const { run, read } = setup();
  run({ OPENV_SITE_URL: 'https://openv.app' });
  const faq = read('prerendered/faq.html');
  assert.match(faq, /<link rel="canonical" href="https:\/\/openv\.app\/faq" \/>/);
  assert.match(faq, /"url":"https:\/\/openv\.app\/"/);
  assert.doesNotMatch(faq, new RegExp(PLACEHOLDER));
  assert.notEqual(read('prerendered/manual/vv.html'), null);
});

test('on a noindex deployment, pre-rendered pages lose only the lines that carry the address', () => {
  for (const env of [{ OPENV_NOINDEX: '1', OPENV_SITE_URL: 'https://staging.openv.app' }, { OPENV_NOINDEX: '1' }]) {
    const { run, read } = setup();
    run(env);
    assert.equal(read('prerendered/faq.html'), '<head>\n</head>\n<div id="prerender"><h1>FAQ</h1></div>\n', JSON.stringify(env));
  }
});

test('a deployment with no address that is not staging (self-hosted) publishes no pre-rendered pages', () => {
  const { run, read } = setup();
  run({ OPENV_SITE_URL: 'https://openv.app' });
  run();
  assert.equal(read('prerendered/faq.html'), null);
  assert.equal(read('prerendered/manual/vv.html'), null);
});

test('each start rebuilds the pre-rendered pages from the build, so a changed address takes', () => {
  const { run, read } = setup();
  run({ OPENV_SITE_URL: 'https://old.example.com' });
  run({ OPENV_SITE_URL: 'https://openv.app' });
  assert.match(read('prerendered/faq.html'), /href="https:\/\/openv\.app\/faq"/);
  assert.doesNotMatch(read('prerendered/faq.html'), /old\.example\.com/);
});

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { runnerImport, type Plugin } from 'vite';

// The build steps the modules this build links in add (vite.config.ts): the
// counterpart of src/ext/modules.ts for the build, and like it the one file
// a build that links other modules replaces. This build links the public
// site (src/site), which writes its crawl files and pre-rendered pages.

// robots.txt, sitemap.xml and llms.txt, written into the build with the
// site's address left as a placeholder (src/site/crawl.ts). They go under a
// dot-directory, which nginx refuses to serve, and the container copies them
// to the web root with the address filled in at start
// (docker-entrypoint.d/46-openv-crawl.sh). The module is loaded through
// Vite's own runner rather than imported here, so the app's source keeps its
// bundler-style imports and this file stays loadable by Node unaided.
const crawlFiles = (): Plugin => ({
  name: 'openv-crawl-files',
  apply: 'build',
  async generateBundle() {
    const root = fileURLToPath(new URL('.', import.meta.url));
    const { module: crawl } = await runnerImport<typeof import('./src/site/crawl')>('/src/site/crawl.ts', {
      root,
      configFile: false,
      logLevel: 'error',
    });
    const files = { 'robots.txt': crawl.robotsTxt(), 'sitemap.xml': crawl.sitemapXml(), 'llms.txt': crawl.llmsTxt() };
    for (const [name, source] of Object.entries(files)) {
      this.emitFile({ type: 'asset', fileName: `.openv-crawl/${name}`, source });
    }
  },
});

// The public pages rendered ahead of time (src/site/prerender.tsx): one copy
// of the built index.html per sitemap page, with the page's markup and head,
// written under .openv-prerender/ for the container to finish at start like
// the crawl files. Runs once the bundle is on disk, since each copy is made
// from the emitted index.html and its asset tags.
const prerenderPages = (): Plugin => {
  let outDir = '';
  return {
    name: 'openv-prerender-pages',
    apply: 'build',
    configResolved(config) {
      outDir = path.resolve(config.root, config.build.outDir);
    },
    async closeBundle() {
      const root = fileURLToPath(new URL('.', import.meta.url));
      const { module: pre } = await runnerImport<typeof import('./src/site/prerender')>('/src/site/prerender.tsx', {
        root,
        configFile: false,
        mode: 'production',
        logLevel: 'error',
      });
      const template = fs.readFileSync(path.join(outDir, 'index.html'), 'utf8');
      const target = path.join(outDir, '.openv-prerender');
      fs.rmSync(target, { recursive: true, force: true });
      // react-router's Link uses useLayoutEffect, which React reports once per
      // server render; it is expected here, and nothing else is silenced.
      const error = console.error;
      console.error = (...args: unknown[]) => {
        if (typeof args[0] === 'string' && args[0].includes('useLayoutEffect does nothing on the server')) return;
        error(...args);
      };
      try {
        const pages = pre.prerenderedPages();
        for (const page of pages) {
          const file = path.join(target, page.file);
          fs.mkdirSync(path.dirname(file), { recursive: true });
          fs.writeFileSync(file, pre.pageDocument(template, page, pre.renderBody(page.path)));
        }
        this.info(`pre-rendered ${pages.length} public pages into .openv-prerender/`);
      } finally {
        console.error = error;
      }
    },
  };
};

export const extPlugins = (): Plugin[] => [crawlFiles(), prerenderPages()];

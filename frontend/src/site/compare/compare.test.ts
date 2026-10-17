// @vitest-environment node
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { PAGES, pagePattern } from '../../pages';
import { isPublicPath } from '../../utils/publicPaths';
import { COMPARISONS, HUB, HUB_COLUMNS, HUB_ROWS, OTHERS } from '.';

// The rules every comparison page keeps (types.ts). Comparative claims are
// what a reader, a competitor or a regulator checks first, so their shape is
// held here rather than left to review.

// Every vendor link a page cites must be in the dated fact sheet, which is
// where each one was read and what it was read for.
const FACT_SHEET = readFileSync(
  fileURLToPath(new URL('../../../../docs/research/competitor-facts-2026-10-16.md', import.meta.url)),
  'utf8',
);

const allText = (c: (typeof COMPARISONS)[number]) =>
  [
    c.title,
    c.summary,
    c.heading,
    c.answer,
    ...c.chooseThem,
    ...c.chooseOpenV,
    ...c.rows.flatMap((r) => [r.dimension, r.openv, r.them]),
    ...c.moving,
    ...c.limits,
  ].join('\n');

describe('comparison pages', () => {
  it('are public site pages the registry routes, each once', () => {
    const sitePaths = new Set(PAGES.filter((p) => p.scope === 'site' && p.public).map(pagePattern));
    for (const path of [HUB.path, ...COMPARISONS.map((c) => c.path)]) {
      expect({ path, routed: sitePaths.has(path), public: isPublicPath(path) }).toEqual({ path, routed: true, public: true });
    }
    expect(new Set(COMPARISONS.map((c) => c.path)).size).toBe(COMPARISONS.length);
  });

  it('open with an answer of two to four sentences, and a summary that fits a meta description', () => {
    for (const c of [HUB, ...COMPARISONS]) {
      const sentences = c.answer.split(/(?<=[.!?])\s+(?=[A-Z])/).length;
      expect({ path: c.path, ok: sentences >= 2 && sentences <= 4 && c.answer.length <= 600 }).toEqual({ path: c.path, ok: true });
      expect({ path: c.path, ok: c.summary.length <= 200 }).toEqual({ path: c.path, ok: true });
    }
  });

  it('say when the other tool is the better choice, and what OpenV does not do', () => {
    for (const c of COMPARISONS) {
      expect({ path: c.path, them: c.chooseThem.length > 0, limits: c.limits.length > 0 }).toEqual({
        path: c.path,
        them: true,
        limits: true,
      });
    }
  });

  it('cite a vendor page in the fact sheet for every statement about another tool', () => {
    for (const c of COMPARISONS) {
      for (const r of c.rows) {
        expect({ path: c.path, row: r.dimension, https: r.source.startsWith('https://') }).toEqual({ path: c.path, row: r.dimension, https: true });
        expect({ path: c.path, row: r.dimension, inFactSheet: FACT_SHEET.includes(r.source) }).toEqual({
          path: c.path,
          row: r.dimension,
          inFactSheet: true,
        });
      }
    }
    for (const o of OTHERS) expect({ name: o.name, inFactSheet: FACT_SHEET.includes(o.source) }).toEqual({ name: o.name, inFactSheet: true });
  });

  it('carry a real date for when they were last checked', () => {
    for (const c of [HUB, ...COMPARISONS]) {
      expect(c.updated).toMatch(/^\d{4}-\d{2}-\d{2}$/);
      expect(Number.isNaN(Date.parse(c.updated))).toBe(false);
    }
  });

  it('make no superlative claims and are plain text', () => {
    for (const c of COMPARISONS) {
      const text = allText(c);
      expect({ path: c.path, superlative: /\b(best|#1|number one|leading|world-class)\b/i.test(text) }).toEqual({
        path: c.path,
        superlative: false,
      });
      expect({ path: c.path, markdown: /`|\*\*|\]\(/.test(text) }).toEqual({ path: c.path, markdown: false });
    }
  });

  it('give the hub matrix a column for OpenV and each head-to-head page, and a value in every cell', () => {
    expect(HUB_COLUMNS.filter((col) => col.path).map((col) => col.path)).toEqual(COMPARISONS.map((c) => c.path));
    for (const r of HUB_ROWS) for (const col of HUB_COLUMNS) expect(r.values[col.key]).toBeTruthy();
  });
});

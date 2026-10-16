import { MANUAL_CHAPTERS } from '../../manual';
import { PAGES, pagePattern } from '../../pages';
import { isPublicPath } from '../../utils/publicPaths';
import { TOPICS, getTopic } from '.';

// The rules every topic page keeps (types.ts): the copy is what search
// engines and AI assistants quote, so its shape is held here rather than left
// to review.

const allText = (t: (typeof TOPICS)[number]) =>
  [
    t.title,
    t.summary,
    t.heading,
    t.answer,
    ...t.sections.flatMap((s) => [s.heading, ...(s.paragraphs ?? []), ...(s.bullets ?? [])]),
    ...t.limits,
  ].join('\n');

describe('topic pages', () => {
  it('are public site pages the registry routes, each once', () => {
    const sitePaths = new Set(PAGES.filter((p) => p.scope === 'site' && p.public).map(pagePattern));
    for (const t of TOPICS) {
      expect({ path: t.path, routed: sitePaths.has(t.path), public: isPublicPath(t.path) }).toEqual({
        path: t.path,
        routed: true,
        public: true,
      });
    }
    expect(new Set(TOPICS.map((t) => t.path)).size).toBe(TOPICS.length);
    expect(new Set(TOPICS.map((t) => t.footerLabel)).size).toBe(TOPICS.length);
  });

  it('open with an answer of two to four sentences that stands on its own', () => {
    for (const t of TOPICS) {
      const sentences = t.answer.split(/(?<=[.!?])\s+(?=[A-Z])/).length;
      expect({ path: t.path, sentences: sentences >= 2 && sentences <= 4 }).toEqual({ path: t.path, sentences: true });
      expect(t.answer.length).toBeLessThanOrEqual(600);
    }
  });

  it('keep the summary to one meta description', () => {
    for (const t of TOPICS) expect({ path: t.path, length: t.summary.length <= 200 }).toEqual({ path: t.path, length: true });
  });

  it('say what OpenV does not do, except the glossary', () => {
    for (const t of TOPICS) {
      if (t.kind === 'glossary') expect(t.limits).toEqual([]);
      else expect({ path: t.path, limits: t.limits.length > 0 }).toEqual({ path: t.path, limits: true });
    }
  });

  it('link only to manual chapters and topic pages that exist', () => {
    const slugs = new Set(MANUAL_CHAPTERS.map((c) => c.slug));
    for (const t of TOPICS) {
      expect(t.manual.length).toBeGreaterThan(0);
      for (const slug of t.manual) expect({ path: t.path, slug, exists: slugs.has(slug) }).toEqual({ path: t.path, slug, exists: true });
      for (const r of t.related) {
        expect(r).not.toBe(t.path);
        expect({ path: t.path, related: r, exists: !!getTopic(r) }).toEqual({ path: t.path, related: r, exists: true });
      }
    }
  });

  it('carry a real date for when the copy was last checked', () => {
    for (const t of TOPICS) {
      expect(t.updated).toMatch(/^\d{4}-\d{2}-\d{2}$/);
      expect(Number.isNaN(Date.parse(t.updated))).toBe(false);
    }
  });

  it('are plain text, since the layout renders no markdown', () => {
    for (const t of TOPICS) expect({ path: t.path, markdown: /`|\*\*|\]\(|^#/m.test(allText(t)) }).toEqual({ path: t.path, markdown: false });
  });

  it('name the licence as the MIT License, never source-available or the Elastic License', () => {
    for (const t of TOPICS) expect(allText(t)).not.toMatch(/source[- ]available|Elastic License/i);
    expect(TOPICS.map(allText).join('\n')).toContain('MIT License');
  });
});

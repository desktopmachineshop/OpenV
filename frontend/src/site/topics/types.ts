// A topic page: one buyer question answered in full
// (docs/plans/search-and-ai-visibility.md, Phase 2). The copy is data so the
// pages share one layout (site/TopicPage.tsx), the sitemap and pre-render
// take their titles and summaries from here, and a test can hold every page
// to the same rules: an answer first, a limits section, links into the
// manual.
//
// Every capability claim in a topic must be one the product can show; what
// it cannot do goes in `limits`, stated as plainly as what it can.

export interface TopicSection {
  heading: string;
  paragraphs?: string[];
  bullets?: string[];
}

export interface Topic {
  /** The page's path, e.g. '/requirements-traceability'. */
  path: string;
  /**
   * 'glossary' renders the sections as a definition list (heading = term,
   * paragraphs = definition) and has no limits; every other topic must
   * state its limits.
   */
  kind?: 'glossary';
  /** The page title (document title, sitemap, llms.txt). */
  title: string;
  /** One sentence for the meta description and llms.txt. */
  summary: string;
  /** The label of its link in the site footer. */
  footerLabel: string;
  eyebrow: string;
  heading: string;
  /**
   * The answer, first: two or three sentences that stand on their own,
   * since they are what a search engine or an AI assistant quotes.
   */
  answer: string;
  /** When the copy was last checked against the product, YYYY-MM-DD. */
  updated: string;
  sections: TopicSection[];
  /** What OpenV does not do here. Never empty, except on the glossary. */
  limits: string[];
  /** Manual chapters that go deeper, by slug. */
  manual: string[];
  /** Other topic pages, by path. */
  related: string[];
}

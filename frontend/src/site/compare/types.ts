// A comparison page: hosted OpenV beside one other hosted tool, for the
// buyer who asks "what is the alternative to X" (docs/plans/
// search-and-ai-visibility.md, 5.2). The copy is data so the pages share one
// layout (site/ComparePage.tsx) and a test holds every page to the rules.
//
// The rules (docs/research/competitor-facts-2026-10-16.md):
// - every statement about the other tool is what its vendor says, with a
//   link to the vendor page in `source`, and that link is in the fact sheet;
// - a fact we could not find is left out, never written as "they lack it";
// - no price for another tool unless the vendor publishes one;
// - OpenV's own limits are stated as plainly as its strengths;
// - hosted is compared with hosted.

export interface CompareRow {
  /** What is compared, e.g. "Test management". */
  dimension: string;
  /** Hosted OpenV, today. */
  openv: string;
  /** The other tool, in its vendor's terms, paraphrased. */
  them: string;
  /** The vendor page `them` comes from. */
  source: string;
}

export interface Comparison {
  /** e.g. '/compare/doors'. */
  path: string;
  /** The other tool's name as its vendor writes it. */
  competitor: string;
  /** Who makes it. */
  vendor: string;
  /** Document title, sitemap and llms.txt. */
  title: string;
  /** One sentence for the meta description and llms.txt, at most 200 characters. */
  summary: string;
  heading: string;
  /** Two to four sentences that stand on their own. */
  answer: string;
  /** When every row was last checked against both products, YYYY-MM-DD. */
  updated: string;
  /** When the other tool is the better choice. Never empty. */
  chooseThem: string[];
  /** When OpenV is. */
  chooseOpenV: string[];
  rows: CompareRow[];
  /** How a team moves across, with ReqIF or otherwise. */
  moving: string[];
  /** What OpenV does not do that matters in this comparison. Never empty. */
  limits: string[];
}

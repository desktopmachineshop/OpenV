// In-app help from the user manual on OpenD (docs/plans/opend-help.md):
// the topics GET /orgs/{id}/help/* answers for a workspace's docs line.

/** A topic near another in the manual's navigation. */
export interface HelpRelated {
  topic_id: string;
  title: string;
}

/** One topic of the manual. `html` is sanitized by the server; links to
 *  other topics carry data-help-topic (and data-help-fragment). */
export interface HelpTopic {
  /** How a context resolved: the key, a parent of it, or the default. */
  key?: string;
  via?: 'key' | 'parent' | 'default';
  fragment?: string;
  topic_id: string;
  title: string;
  shortdesc: string;
  html: string;
  related: HelpRelated[];
  /** The page on the public docs site, or "" when there is none. */
  full_url: string;
  /** The docs line served, such as "0.23". */
  line: string;
}

export interface HelpHit {
  topic_id: string;
  title: string;
  snippet: string;
}

export interface HelpResults {
  query: string;
  hits: HelpHit[];
  line: string;
}

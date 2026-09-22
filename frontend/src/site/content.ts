// Copy for the storefront pages beyond the landing page (site/*.tsx). One
// place for the words, like landing/content.ts, so tests can assert they
// reach the DOM and so a fact changes once.

import { FREE_HOSTING_ISSUE_URL, REPO_URL } from '../landing/content';

/** The five demo videos, in viewing order. Files live in public/videos. */
export interface Demo {
  id: string;
  title: string;
  /** What the viewer sees, one sentence. */
  summary: string;
  /** Vertical (phone) recordings are laid out portrait. */
  vertical?: boolean;
  minutes: number;
}

export const DEMOS: Demo[] = [
  {
    id: 'tour',
    title: 'The requirements module in five minutes',
    summary:
      'A workspace, a project, the tree, a requirement with its stable reference, status, attributes and traceability, filters, and the baseline selector.',
    minutes: 3,
  },
  {
    id: 'verify',
    title: 'Verification: coverage, gaps and evidence',
    summary:
      'The V&V summary, per-requirement coverage, verification rolled up from child projects, the gap list, test runs, the traceability matrix, impact analysis and the review queue.',
    minutes: 3,
  },
  {
    id: 'documents',
    title: 'Documents, baselines and what changed',
    summary:
      'The download wizard and its formats, choosing the content of a PDF, capturing a baseline, reading the project as it was, comparing two baselines, and release notes.',
    minutes: 3,
  },
  {
    id: 'phone-review',
    title: 'Review and approve from your phone',
    summary:
      'The project menu, the requirements module stacked into panes, a requirement on a phone, approvals, notes, the review queue, V&V and notifications.',
    vertical: true,
    minutes: 2,
  },
  {
    id: 'phone-agents',
    title: 'Plan, run agents and keep in touch from your phone',
    summary:
      'The board, agent definitions, recorded runs, stakeholder interviews, project and workspace settings, and where notifications reach you.',
    vertical: true,
    minutes: 2,
  },
];

export const DEMOS_INTRO =
  'Five short recordings of OpenV working on a real project: the platform’s own requirements, kept in OpenV. Narrated, no slides, nothing staged.';

/** One FAQ entry. */
export interface FaqEntry {
  q: string;
  a: string;
}

export interface FaqGroup {
  id: string;
  title: string;
  entries: FaqEntry[];
}

export const FAQ_GROUPS: FaqGroup[] = [
  {
    id: 'security',
    title: 'Security, on every tier',
    entries: [
      {
        q: 'Who can see my project?',
        a: 'Members you grant access to, by name or by team, at the role you choose: owner, editor, reviewer or viewer. Workspace admins see every project in their workspace. Nobody else, unless you mint a share link, and a link stops working the moment you revoke it.',
      },
      {
        q: 'How is my data separated from other customers’ on the hosted service?',
        a: 'Every request is checked against the caller’s workspace and project role before anything is read or written; there is no path to another workspace’s rows. Files are stored under the uploading workspace and served only through that same check.',
      },
      {
        q: 'Is data encrypted?',
        a: 'In transit, always: the hosted service is HTTPS only, and the API refuses plain HTTP behind the proxy. At rest, the hosted database and file volume are encrypted by the hosting provider. Session tokens, runner keys, invitation tokens and share-link tokens are stored as SHA-256 hashes, never in the clear.',
      },
      {
        q: 'What does an AI agent see, and where does it go?',
        a: 'An agent reads your project through typed tools scoped to the project it runs in, and writes back as proposals a person approves. It runs on your own AI subscription or your workspace’s own API keys, so your requirements go to the AI provider you chose and to no one else. OpenV does not resell or train on your data.',
      },
      {
        q: 'Can I keep it entirely on my own hardware?',
        a: 'Yes. Self-hosting is free forever under the Elastic License 2.0, with every feature, and the Agent Connector runs agents on a machine you control. A JSON export from the hosted service restores into a self-hosted OpenV.',
      },
    ],
  },
  {
    id: 'tiers',
    title: 'What each tier gets',
    entries: [
      {
        q: 'Single User',
        a: 'The full product for one person in a personal workspace: requirements, traceability, V&V, documents, interviews and agents on your own AI subscription. Sign-in by password or Google. Exports on every format. Free.',
      },
      {
        q: 'Business Lite',
        a: 'Everything in Single User, plus agents on your own API keys, an always-on hosted runner for cron and event automations, and a larger cloud runner. Still one person, still your own keys.',
      },
      {
        q: 'Business',
        a: 'Shared company workspaces with members and teams, per-project access grants, a workspace AI budget with usage reporting, and the monthly stable release channel with an upgrade window you choose. Workspace admins are notified of every membership change.',
      },
      {
        q: 'Enterprise',
        a: 'Business, on your terms: a dedicated instance on your servers or ours with our support, SSO/OIDC and directory setup, custom integrations with your PLM, ALM, ticketing or CI, a support SLA and priority fixes.',
      },
      {
        q: 'Self-hosted',
        a: 'Every feature of every tier, on your hardware, no limits from us. Security is yours to run: your TLS, your backups, your identity provider, your network.',
      },
      {
        q: 'Charities and open-source projects',
        a: 'The hosted service, free for as long as it exists. For an open-source project the deal is public by design: the latest baseline of every project in the workspace is published on the open-source page, while live work stays private until the next baseline.',
      },
      {
        q: 'How billing works',
        a: 'A workspace admin subscribes from the Billing tab in workspace settings: Business Lite is a flat price, Business is per member, both monthly or yearly, in GBP, USD or EUR. Payment goes through Stripe on a page of theirs, so no card detail ever reaches OpenV; VAT is worked out at checkout and a business VAT number is accepted. Invoices, the payment card and cancellation are managed in the same place. A first subscription starts with a 14-day trial. If a subscription lapses, everything in the workspace stays readable and exportable.',
      },
    ],
  },
  {
    id: 'sharing',
    title: 'Sharing and review',
    entries: [
      {
        q: 'How do I give a customer or an assessor a copy?',
        a: 'Download a PDF or Word document with exactly the sections and fields you choose, or mint a public share link that opens the live project, read only, with no account. The link unfurls with a preview when pasted into Slack, Discord, LinkedIn or Teams.',
      },
      {
        q: 'How do reviewers comment without editing?',
        a: 'Mint a reviewer link, or grant the reviewer role by name. A reviewer reads everything and adds notes, comments and mentions, and cannot change a word of the specification.',
      },
      {
        q: 'What is a baseline?',
        a: 'An immutable snapshot of every artifact, link and attachment at a moment: what was agreed at a review, a release or a contract. Any document can be produced from a baseline instead of the live project, and two baselines can be compared for the list of what changed.',
      },
    ],
  },
  {
    id: 'practical',
    title: 'Practical',
    entries: [
      {
        q: 'Can I get my data out?',
        a: 'Always, on every plan: PDF, Word, JSON, CSV, Excel and ReqIF, from the live project or any baseline. A JSON export restores into another OpenV, hosted or self-hosted.',
      },
      {
        q: 'Does it work with DOORS or Polarion?',
        a: 'ReqIF, the OMG interchange format both read and write, is an import and an export format in OpenV.',
      },
      {
        q: 'Does it work on a phone?',
        a: 'Yes, as an installable web app: the requirements module stacks into panes, approvals and comments work with a thumb, and notifications arrive as pushes.',
      },
      {
        q: 'What happens when the alpha ends?',
        a: 'You keep what you have until it is announced otherwise, and export never depends on a plan.',
      },
    ],
  },
];

export const OPEN_SOURCE_INTRO =
  'Open-source projects use hosted OpenV free, for as long as it exists. The one condition is openness: the latest baseline of every project in the workspace is published here, for anyone to read. Live work stays private until the next baseline is captured.';

export const OPEN_SOURCE_CLAIM_URL = FREE_HOSTING_ISSUE_URL;

export const OPENV_OWN_PROJECT_NOTE =
  'OpenV is itself built this way: the platform’s requirements, design and tests live in an OpenV project, and every change to the product starts there.';

/** Customer stories are collected, not invented: the page holds the room. */
export const CUSTOMERS_INTRO =
  'OpenV is in alpha and the teams using it are still writing their stories. This page is where they will go: who they are, what they build, and what changed when their requirements, tests and agents started sharing one audit trail.';

export const CUSTOMER_STORY_PLACEHOLDERS: { sector: string; prompt: string }[] = [
  { sector: 'Medical devices', prompt: 'A regulated product team on design controls and V&V evidence.' },
  { sector: 'Aerospace and defence', prompt: 'A programme with subsystems and suppliers on flow-down.' },
  { sector: 'Industrial machinery', prompt: 'A small engineering firm replacing spreadsheets and Word.' },
];

export const CUSTOMER_STORY_INVITE_URL = `${REPO_URL}/issues/new?template=alpha-feedback.md&title=${encodeURIComponent('Our story with OpenV')}`;

export const WHITE_PAPERS_INTRO =
  'Longer reads on the thinking behind OpenV: how requirements, traceability and verification fit together, and where AI agents belong in a regulated process. Each paper is published here when it is ready.';

export interface WhitePaper {
  title: string;
  summary: string;
  status: 'In preparation' | 'Published';
  href?: string;
}

export const WHITE_PAPERS: WhitePaper[] = [
  {
    title: 'Agents inside the audit trail',
    summary:
      'Why an AI agent should propose and never write, how proposals, approvals and versions make its work as traceable as a person’s, and what that means for a design history file.',
    status: 'In preparation',
  },
  {
    title: 'Flow-down without the pain',
    summary:
      'Modelling a programme as a tree of projects, refining requirements across the boundary, and rolling verification back up, with suppliers working in their own project.',
    status: 'In preparation',
  },
  {
    title: 'Leaving DOORS: a ReqIF migration guide',
    summary: 'What survives a ReqIF round trip, what does not, and how to check the result against the source.',
    status: 'In preparation',
  },
  {
    title: 'Requirements quality you can measure',
    summary:
      'The wording rules OpenV lints against (ISO/IEC/IEEE 29148, EARS), how the score is computed, and what it is worth as a review gate.',
    status: 'In preparation',
  },
];

export const WHITE_PAPER_NOTIFY_URL = `${REPO_URL}/issues/new?template=alpha-feedback.md&title=${encodeURIComponent('Tell me when the white papers are out')}`;

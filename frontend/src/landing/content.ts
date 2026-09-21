// Copy for the public landing page (views/Landing.tsx). It lives apart from
// the view so the unit test and the manual FAQ read the same words, and so the
// facts below have one place to change.

export const REPO_URL = 'https://github.com/desktopmachineshop/OpenV';
export const ISSUES_URL = `${REPO_URL}/issues`;
export const FREE_HOSTING_ISSUE_URL = `${REPO_URL}/issues/new?template=free-hosting.md`;
export const FEEDBACK_ISSUE_URL = `${REPO_URL}/issues/new?template=alpha-feedback.md`;
export const QUICKSTART_URL = `${REPO_URL}/blob/master/docs/QUICKSTART.md`;
export const LICENSE_URL = `${REPO_URL}/blob/master/LICENSE`;

export const TAGLINE = 'Requirements, traceability and V&V evidence, with AI agents that work inside the audit trail.';

export const SUBLINE =
  'DOORS-grade rigour without the DOORS-grade licence: typed artifacts, stable references, baselines, verification runs, documents, and AI agents that propose changes for your team to approve. Open source. Bring your own AI.';

/** What the platform does, one card each. */
export const FEATURES: { title: string; body: string }[] = [
  {
    title: 'Requirements and traceability',
    body: 'Requirements, user needs, design items, hazards and test cases with stable refs, typed links, version history, suspect-link review and immutable baselines.',
  },
  {
    title: 'V&V evidence',
    body: 'Test runs record results against test cases. Coverage, gaps and the traceability matrix update as links change, and the V&V status ships as a PDF.',
  },
  {
    title: 'Agents with human review',
    body: 'Agents read your project through typed tools and write back as proposals you approve. Kanban cards launch runs; automations run on a schedule or an event.',
  },
  {
    title: 'Stakeholder interviews',
    body: 'Share a link. An interviewer agent talks to the stakeholder and records candidate needs as draft artifacts for your team to review.',
  },
  {
    title: 'Documents and hand-over',
    body: 'PDF and Word specifications with the sections, fields and evidence you choose, from the live project or any baseline; ReqIF for the tool on the other side; a share link for anyone who only needs to read.',
  },
  {
    title: 'Programmes, not just projects',
    body: 'Subsystems and suppliers work in their own projects, refine the requirements above them, and verification rolls back up the tree. Owners and reference parties say who is responsible for what.',
  },
];

/** Export formats, in the words the download wizard uses. */
export const EXPORT_FORMATS: { label: string; body: string }[] = [
  { label: 'PDF specification', body: 'The document as a reader sees it: sections, artifacts, figures and traceability.' },
  { label: 'Word document', body: 'The same specification as a .docx, for editing or review outside OpenV.' },
  { label: 'JSON data', body: 'The complete project, including everything an OpenV import can restore.' },
  { label: 'CSV table', body: 'One row per artifact for a spreadsheet.' },
  { label: 'Excel workbook (.xlsx)', body: 'A sheet per artifact type, one for the links, and a cover naming the snapshot.' },
  { label: 'ReqIF interchange', body: 'The OMG format read by DOORS and Polarion.' },
];

export const IMPORT_FORMATS = 'JSON and ReqIF';

/** The commitment, verbatim on the page and in the manual. */
export const DATA_PROMISE =
  'If hosted OpenV ever charges, your data will not be behind the paywall. Export and import stay available on every plan, and a JSON export restores into a self-hosted OpenV.';

/** Single User (free plan) limits in force. Mirrors the free plan in
 *  internal/domain/orgs/limits.go; change both together. */
export const HOSTED_LIMITS: string[] = [
  'Hosted runner: 2 GB memory, 1 CPU.',
  'Cloud runner lease: 60 minutes, reclaimed after 15 idle minutes.',
  'Largest figure you can attach: 128 MB.',
  'Cloud runners come from a shared pool, so at busy times you may wait for one.',
  'Hosted runners cannot reach code repositories. Run the Agent Connector on your own machine for that.',
  'Agent runs use your own AI subscription (Claude Code, Codex or Gemini) or your workspace’s own API keys. OpenV does not resell AI.',
];

export interface PricingTier {
  id: string;
  name: string;
  /** "Free", "Free forever" or "Coming soon". */
  price: string;
  /** False for tiers that exist on the page but cannot be had yet. */
  available: boolean;
  /** The plan key the platform sells this tier under (business_lite,
   *  business), for reading a live price from GET /api/v1/public/plans.
   *  Page ids are hyphenated; plan keys are not. Absent on tiers that are
   *  never bought. */
  planKey?: string;
  summary: string;
  points: string[];
  cta: { label: string; href: string; external?: boolean };
}

const interest = (tier: string) =>
  `${REPO_URL}/issues/new?template=alpha-feedback.md&title=${encodeURIComponent(`Interested in ${tier}`)}`;

/** The hosted service, from the free offering to the enterprise tier. */
export const HOSTED_TIERS: PricingTier[] = [
  {
    id: 'single',
    name: 'Single User',
    price: 'Free',
    available: true,
    summary: 'The whole product for one person, free. Bring your own AI.',
    points: [
      'Requirements, V&V, traceability and interviews: the full product for one person.',
      'Agents run on your own AI subscription (Claude Code, Codex or Gemini) via the Agent Connector on your machine, or a leased cloud runner within the free limits.',
      'Personal workspace.',
      'Export everything, always.',
    ],
    cta: { label: 'Create free account', href: '/login?mode=register' },
  },
  {
    id: 'business-lite',
    name: 'Business Lite',
    price: 'Coming soon',
    available: false,
    planKey: 'business_lite',
    summary: 'For one person who wants agents working while the laptop is closed.',
    points: [
      'Everything in Single User.',
      'Higher cloud runner limits: 4 GB memory, 2 CPUs, 120-minute lease reclaimed after 20 idle minutes.',
      'Attach figures up to 512 MB, so a CAD assembly goes on the requirement it belongs to.',
      'Agents on your own API keys instead of a signed-in CLI.',
      'Always-on agents: a hosted runner that keeps cron and event automations running unattended.',
    ],
    cta: { label: 'Tell us you want this', href: interest('Business Lite'), external: true },
  },
  {
    id: 'business',
    name: 'Business',
    price: 'Coming soon',
    available: false,
    planKey: 'business',
    summary: 'For companies working on the same products together.',
    points: [
      'Everything in Business Lite.',
      'Cloud runners again: 8 GB memory, 4 CPUs, a 4-hour lease reclaimed after 30 idle minutes.',
      'Attach figures up to 1 GB.',
      'Shared company workspaces.',
      'Teams, per-project access and the reviewer role.',
      'Workspace AI budget and usage reporting.',
      'Monthly stable release channel, turned on at a time you choose.',
    ],
    cta: { label: 'Tell us you want this', href: interest('Business'), external: true },
  },
  {
    id: 'enterprise',
    name: 'Enterprise',
    price: 'Coming soon',
    available: false,
    summary: 'For organisations that need it on their terms.',
    points: [
      'Everything in Business.',
      'Run on your own servers with our support.',
      'SSO/OIDC and directory setup.',
      'Custom integrations with your PLM, ALM, ticketing or CI.',
      'Support SLA and priority fixes.',
    ],
    cta: { label: 'Tell us you want this', href: interest('Enterprise'), external: true },
  },
];

/** The two ways that are free for good. */
export const OTHER_TIERS: PricingTier[] = [
  {
    id: 'self-host',
    name: 'Self-hosted',
    price: 'Free forever',
    available: true,
    summary: 'Elastic License 2.0. All features, your hardware, no limits from us.',
    points: [
      'One command: git clone, docker compose up.',
      'Your data never leaves your machines.',
      'Same agents, runners and export as the hosted service.',
      'Hosted-runner limits do not apply.',
    ],
    cta: { label: 'Self-host guide', href: QUICKSTART_URL, external: true },
  },
  {
    id: 'charity',
    name: 'Charities and open source',
    price: 'Free forever',
    available: true,
    summary: 'Registered charities and open-source projects use the hosted service free, for as long as it exists.',
    points: [
      'Everything in the hosted service.',
      'Stays free when tiers launch.',
      'Claim it with a GitHub issue: tell us who you are and the workspace name.',
    ],
    cta: { label: 'Claim free hosting', href: FREE_HOSTING_ISSUE_URL, external: true },
  },
];

/** Every tier on the page, for tests and the manual. */
export const PRICING_TIERS: PricingTier[] = [...HOSTED_TIERS, ...OTHER_TIERS];

export const ALPHA_NOTE =
  'While OpenV is in alpha, every workspace has every tier’s features, free. When tiers launch you keep what you have until we announce otherwise, and export never depends on a plan.';

/** What each paid tier raises the free limits to. Quotes the plans in
 *  internal/domain/orgs/limits.go; change both together.
 *
 *  Business is stated rather than left as "everything in Business Lite":
 *  the two tiers used to have byte-identical runner limits, so a reader
 *  comparing them found nothing to compare (issue #361). A tier that raises
 *  a number says which number. */
export const BUSINESS_LITE_LIMITS: string[] = [
  'Cloud runner: 4 GB memory, 2 CPUs.',
  'Cloud runner lease: 120 minutes, reclaimed after 20 idle minutes.',
  'Largest figure: 512 MB.',
];

export const BUSINESS_LIMITS: string[] = [
  'Cloud runner: 8 GB memory, 4 CPUs.',
  'Cloud runner lease: 240 minutes, reclaimed after 30 idle minutes.',
  'Largest figure: 1 GB.',
];

export const PRICING_FOOTNOTE =
  'There is no billing in the product today and nothing to buy. What is free stays free until announced otherwise, and export never depends on a plan.';

export const SELF_HOST_COMMANDS = ['git clone https://github.com/desktopmachineshop/OpenV.git', 'cd OpenV', 'docker compose up -d'];

export const LICENSE_GLOSS =
  'OpenV is licensed under the Elastic License 2.0. Self-hosting is free for anyone, at any scale: read the source, fork it, change it, and run it on your own servers for your own teams with every feature. The one thing you may not do is sell OpenV to others as a hosted or managed service.';

/** The licence in plain English, one line each; the README carries the same list. */
export const LICENSE_INTENT: string[] = [
  'Self-host free, at any scale, with every feature. No licence key, no phone-home, no seat count.',
  'Read, fork and modify the source; keep your changes private or publish them.',
  'Paid help is fine: a consultant or integrator can charge to install, customise or support a customer’s own OpenV.',
  'Not allowed: offering OpenV, or a substantial part of it, to third parties as a hosted or managed service.',
];

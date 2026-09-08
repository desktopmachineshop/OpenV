// Copy for the public landing page (views/Landing.tsx). It lives apart from
// the view so the Jest test and the manual FAQ read the same words, and so the
// facts below have one place to change.

export const REPO_URL = 'https://github.com/desktopmachineshop/OpenV';
export const ISSUES_URL = `${REPO_URL}/issues`;
export const FREE_HOSTING_ISSUE_URL = `${REPO_URL}/issues/new?template=free-hosting.md`;
export const FEEDBACK_ISSUE_URL = `${REPO_URL}/issues/new?template=alpha-feedback.md`;
export const QUICKSTART_URL = `${REPO_URL}/blob/master/docs/QUICKSTART.md`;
export const LICENSE_URL = `${REPO_URL}/blob/master/LICENSE`;

export const TAGLINE = 'Requirements, traceability and V&V evidence, with AI agents that work inside the audit trail.';

export const SUBLINE =
  'DOORS-style rigour for teams that ship real products: typed artifacts, baselines, verification runs, and agents that propose changes for you to approve. Open source. Bring your own AI.';

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
    body: 'Share a link. An interviewer agent talks to the stakeholder and records candidate needs as draft artifacts for you to review.',
  },
];

/** Export formats, in the words the download wizard uses. */
export const EXPORT_FORMATS: { label: string; body: string }[] = [
  { label: 'PDF specification', body: 'The document as a reader sees it: sections, artifacts, figures and traceability.' },
  { label: 'Word document', body: 'The same specification as a .docx, for editing or review outside OpenV.' },
  { label: 'JSON data', body: 'The complete project, including everything an OpenV import can restore.' },
  { label: 'CSV table', body: 'One row per artifact for a spreadsheet.' },
  { label: 'ReqIF interchange', body: 'The OMG format read by DOORS and Polarion.' },
];

export const IMPORT_FORMATS = 'JSON and ReqIF';

/** The commitment, verbatim on the page and in the manual. */
export const DATA_PROMISE =
  'If hosted OpenV ever charges, your data will not be behind the paywall. Export and import stay available on every plan, and a JSON export restores into a self-hosted OpenV.';

/** Hosted-plan limits in force. Mirrors the free plan in
 *  internal/domain/orgs/limits.go; change both together. */
export const HOSTED_LIMITS: string[] = [
  'Hosted runner: 2 GB memory, 1 CPU.',
  'Cloud runner lease: 60 minutes, reclaimed after 15 idle minutes.',
  'Cloud runners come from a shared pool, so at busy times you may wait for one.',
  'Hosted runners cannot reach code repositories. Run the Agent Connector on your own machine for that.',
  'Agent runs use your own AI subscription (Claude Code, Codex or Gemini) or your workspace’s own API keys. OpenV does not resell AI.',
];

export interface PricingTier {
  id: string;
  name: string;
  price: string;
  summary: string;
  points: string[];
  cta: { label: string; href: string; external?: boolean };
}

export const PRICING_TIERS: PricingTier[] = [
  {
    id: 'hosted',
    name: 'Hosted alpha',
    price: 'Free',
    summary: 'Free while OpenV is in alpha. Every feature. No card, no trial clock.',
    points: [
      'The full product: requirements, V&V, agents, interviews, automations.',
      'Workspaces, teams and per-project access included.',
      'Runs on our servers; the limits below apply.',
      'When hosted plans arrive they will be announced ahead of time.',
    ],
    cta: { label: 'Create free account', href: '/login?mode=register' },
  },
  {
    id: 'self-host',
    name: 'Self-hosted',
    price: 'Free forever',
    summary: 'AGPL-3.0. All features, your hardware, no limits from us.',
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
    summary: 'Registered charities and open-source projects use the hosted service free, for as long as it exists.',
    points: [
      'Everything in the hosted plan.',
      'Stays free when hosted plans arrive.',
      'Claim it with a GitHub issue: tell us who you are and the workspace name.',
    ],
    cta: { label: 'Claim free hosting', href: FREE_HOSTING_ISSUE_URL, external: true },
  },
];

export const PRICING_FOOTNOTE =
  'There is no billing in the product today and nothing to buy. What is free stays free until announced otherwise, and export never depends on a plan.';

export const SELF_HOST_COMMANDS = ['git clone https://github.com/desktopmachineshop/OpenV.git', 'cd OpenV', 'docker compose up -d'];

export const LICENSE_GLOSS =
  'OpenV is licensed under the GNU AGPL-3.0. You can use, self-host, modify and redistribute it freely, solo, as a team or commercially. The AGPL asks only that if you run a modified version for others over a network, you offer those users the modified source. Just using OpenV obligates you to nothing.';

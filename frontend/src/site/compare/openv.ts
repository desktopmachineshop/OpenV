// Hosted OpenV's side of every comparison, written once so the pages and
// the hub matrix cannot disagree. Each line restates a claim the site
// already makes and that was checked against the code (site/topics,
// landing/content.ts); change it there first.

export const OPENV = {
  price:
    'Single User plan free; team plans are coming soon, and workspaces created during the alpha keep every feature free.',
  hosted: 'Hosted at openv.app by Desktop Machine Shop, the makers of OpenV. Also free to self-host under the MIT License.',
  traceability:
    'Typed, rule-checked links from needs to requirements, design, hazards and tests; suspect links; a traceability matrix and gap reports; immutable baselines with comparison.',
  tests:
    'Built in: test runs recorded against test-case versions, evidence with SHA-256 digests, and a V&V status report.',
  reqif:
    'ReqIF 1.x import and export. Round trips are tested OpenV to OpenV; exchange with other tools has not been tested yet.',
  formats: 'Export to PDF, Word, Excel, CSV, JSON and ReqIF on every plan. Import reads JSON and ReqIF only.',
  ai: 'AI agents run on your own Claude Code, Codex or Gemini subscription and, by default, propose changes for a person to approve.',
  reviews:
    'Review rounds and a review queue; public read-only links, and reviewer links that let an outside reviewer sign in and comment without editing. No electronic signatures and no separate approver role.',
  programmes: 'Programmes as linked projects: subsystems and suppliers refine the requirements above them and verification rolls up.',
  start: 'A free account with no card, today.',
} as const;

/** What OpenV does not do, in the words the comparison pages reuse. */
export const OPENV_LIMITS = {
  signatures: 'No electronic signatures, so 21 CFR Part 11 and similar rules are not met by OpenV alone.',
  approver: 'No separate approver role: any project editor can approve, including their own change.',
  certification: 'No certification and no tool qualification kit for ISO 26262, DO-178C, IEC 62304 or any other standard.',
  reqif: 'ReqIF exchange with DOORS, DOORS Next, Polarion or Jama has not been tested yet; treat a first move as a pilot.',
  alpha: 'OpenV is in alpha: features and screens still change between releases.',
  identity: 'No multi-factor authentication or SAML of its own; use an OpenID Connect provider that enforces MFA.',
  variants: 'No variant or product-line configuration management.',
  imports: 'No Word, Excel or CSV import: requirements come in by ReqIF or OpenV’s own JSON.',
  jira: 'No live links into Jira: work crosses between the two by export, ReqIF or the API.',
} as const;

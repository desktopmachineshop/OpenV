import type { Topic } from './types';

export const SAFETY_CRITICAL: Topic = {
  path: '/safety-critical',
  title: 'OpenV for safety-critical and regulated work',
  summary:
    'What a requirements and V&V tool must do for safety-critical products, what OpenV covers today, what it does not claim, and how to evaluate it for a regulated programme.',
  footerLabel: 'Safety-critical work',
  eyebrow: 'Regulated engineering',
  heading: 'Requirements and V&V for safety-critical products: what OpenV covers and what it does not claim',
  answer:
    'OpenV gives a safety-critical team the record an assessor asks for: needs, requirements, design, hazards and tests in one traced project, verification results and evidence against each requirement, immutable baselines and a full change history. It is in alpha, holds no certification, ships no tool qualification kit and has no electronic signatures, so a regulated programme should treat it as a tool to evaluate and qualify for its own process, not as a compliant system out of the box.',
  updated: '2026-10-13',
  sections: [
    {
      heading: 'What to look for in any tool',
      bullets: [
        'Bidirectional traceability from stakeholder needs through requirements and design to verification, with the gaps reported, not left to a search.',
        'Hazards linked to the design items that mitigate them, and unmitigated hazards reported.',
        'A verification method on every requirement, results tied to the version of the test that was run, and evidence kept with the result.',
        'Baselines that cannot change after the fact, comparisons between them, and every artifact’s history.',
        'A review and approval workflow, and a log of who changed what.',
        'Change impact analysis, and links flagged for review when either end changes.',
        'An exit: open export formats, so the record outlives the tool.',
        'For the strictest regimes: electronic signatures, separation of duties, and a qualification package for the tool itself.',
      ],
    },
    {
      heading: 'What OpenV does today',
      bullets: [
        'Typed, rule-checked links from needs to requirements, design items, hazards and test cases, a traceability matrix, and seven gap reports.',
        'Hazards as their own artifact type, with design items that mitigate them and a gap report for hazards nothing mitigates.',
        'Test runs with results tied to test-case versions, evidence bundles with SHA-256 digests, and a V&V status report.',
        'Immutable baselines, baseline comparison, per-artifact version history and an activity log with a named actor on every event.',
        'Draft, in-review, approved and superseded statuses, review rounds, a review queue, and suspect links when an artifact changes.',
        'Requirement wording checked against ISO/IEC/IEEE 29148 “shall” rules as advice, never as a gate.',
        'PDF, Word, Excel, CSV, JSON and ReqIF downloads on every plan, from the live project or any baseline, and free self-hosting so the data can stay on your network.',
        'AI agents that, by default, can only propose changes for a person to approve, with every proposal and approval recorded.',
      ],
    },
    {
      heading: 'How to evaluate it for your programme',
      paragraphs: [
        'Run a pilot on a real slice of your product: a subsystem’s requirements, its hazards and its tests. Check that the traceability matrix, the gap list and a baseline document give your assessor what they need, and decide which of the limits below your process covers another way. If your standard asks for tool qualification, OpenV’s own requirements, design and test cases are kept in an OpenV project, and exports of it are published in the source repository: a starting point for your own qualification evidence, not a substitute for it.',
      ],
    },
  ],
  limits: [
    'No certification and no claim of compliance with ISO 26262, DO-178C, IEC 62304, ISO 13485, IEC 61508 or any other standard, and no tool qualification kit.',
    'No electronic signatures, so 21 CFR Part 11 and similar rules are not met by OpenV alone.',
    'No separate approver role: any project editor can approve, including their own change.',
    'No multi-factor authentication of its own, and no SAML; use an OpenID Connect provider that enforces MFA.',
    'Audit-log export and retention settings are not built yet.',
    'OpenV is in alpha: features and screens still change between releases, though data, exports and the API stay compatible across stable releases.',
  ],
  manual: ['vv', 'links', 'artifacts'],
  related: ['/requirements-traceability', '/verification-evidence', '/baselines-and-audit-trail', '/self-hosted-requirements-management'],
};

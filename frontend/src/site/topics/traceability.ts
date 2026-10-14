import type { Topic } from './types';

export const TRACEABILITY: Topic = {
  path: '/requirements-traceability',
  title: 'Requirements traceability',
  summary:
    'End-to-end traceability from user needs to requirements, design items, hazards and test cases, with a trace matrix, gap analysis, suspect links and change impact.',
  footerLabel: 'Requirements traceability',
  eyebrow: 'Traceability',
  heading: 'End-to-end requirements traceability, from need to test evidence',
  answer:
    'OpenV keeps user needs, requirements, design items, hazards and test cases in one project, joined by typed links whose rules the server enforces. From those links it builds a traceability matrix, finds the gaps (requirements nothing verifies, needs nothing derives from, hazards nothing mitigates) and marks every link suspect when either end changes, so the trace stays true as the requirements move.',
  updated: '2026-10-13',
  sections: [
    {
      heading: 'What is traced to what',
      paragraphs: [
        'Each item is an artifact with a type and a stable reference that is never reused: NEED-3, REQ-12, DES-4, HAZ-2, TC-9. Links between them are typed, and the server refuses a link its rules do not allow, so a matrix built from them means what it says.',
      ],
      bullets: [
        'derives-from: a requirement derives from a user need',
        'decomposes-to and refines: a requirement breaks down into, or refines, another requirement',
        'satisfies: a design item satisfies a requirement',
        'mitigates: a design item mitigates a hazard',
        'verifies: a test case verifies a requirement',
        'validates: a test case validates a user need',
        'impacts and relates-to: looser links between any two artifacts',
      ],
    },
    {
      heading: 'The traceability matrix',
      paragraphs: [
        'One row per requirement: the user needs it derives from, the design items that satisfy it, the hazards those design items mitigate, the test cases that verify it, and the latest result of each. Every row carries a coverage rollup. The matrix exports as CSV, and the same coverage goes into the PDF and Word downloads and the V&V status report.',
      ],
    },
    {
      heading: 'Finding the gaps',
      paragraphs: [
        'The V&V view lists seven kinds of gap, each with the artifacts in it, so a review starts from what is missing rather than from a search:',
      ],
      bullets: [
        'requirements with no verification method chosen',
        'requirements to be verified by test that no test case verifies',
        'requirements verified by inspection, analysis or demonstration that nobody has yet attested to',
        'requirements whose latest verification failed',
        'test cases that verify nothing',
        'user needs that no requirement derives from',
        'hazards that no design item mitigates',
      ],
    },
    {
      heading: 'Suspect links and change impact',
      paragraphs: [
        'When an artifact’s content changes, every link touching it is marked suspect until someone re-reads both ends and confirms it; approving the changed artifact confirms them too. Suspect links collect in the review queue. Before a change is made, the impact view walks the link graph upstream, downstream or both, and shows everything the change could reach and the path it takes.',
      ],
    },
    {
      heading: 'Across projects and suppliers',
      paragraphs: [
        'A project can name a parent project. Its requirements refine the parent’s, and the parent’s coverage rolls up the worst result from the child, so a system requirement shows red when a subsystem’s verification fails. Each artifact can carry an owner, a person, team or supplier, and a download can be narrowed to one owner’s share.',
      ],
    },
  ],
  limits: [
    'Validation links (a test case validating a user need) are recorded and shown, but they do not count towards coverage or gap analysis.',
    'Traceability lives inside OpenV. There are no live links into Jira, a PLM system or a code repository; work crosses tool boundaries by export, ReqIF or the API.',
    'Cross-project flow-down reaches a workspace on the stable release channel only with the stable release that includes it.',
  ],
  manual: ['links', 'vv', 'artifacts'],
  related: ['/verification-evidence', '/baselines-and-audit-trail', '/safety-critical'],
};

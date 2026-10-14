import type { Topic } from './types';

const term = (heading: string, definition: string) => ({ heading, paragraphs: [definition] });

export const GLOSSARY: Topic = {
  path: '/glossary',
  kind: 'glossary',
  title: 'Requirements and V&V glossary',
  summary:
    'Plain definitions of the terms of requirements management and verification and validation: traceability, trace matrix, baseline, verification method, suspect link, ReqIF and more.',
  footerLabel: 'Glossary',
  eyebrow: 'Glossary',
  heading: 'Requirements management and V&V terms, defined',
  answer:
    'The words used across requirements management, systems engineering and verification and validation, each defined in a sentence or two, with how OpenV handles it. The definitions follow common usage, including ISO/IEC/IEEE 29148 for requirements and the usual V&V vocabulary of safety-critical engineering.',
  updated: '2026-10-13',
  sections: [
    term('User need (stakeholder need)', 'What a person or organisation needs from the product, in their terms, before it becomes a requirement. In OpenV a user need is its own artifact type, and requirements derive from it.'),
    term('Requirement', 'A statement of what the system shall do or be, written to be unambiguous and verifiable. ISO/IEC/IEEE 29148 asks for “shall” statements; OpenV checks wording against that convention, or RFC 2119, as advice.'),
    term('Design item', 'A part of the design that satisfies one or more requirements, or mitigates a hazard.'),
    term('Hazard', 'A potential source of harm. A hazard is controlled by design items that mitigate it, and a hazard with no mitigation is a gap.'),
    term('Traceability', 'The recorded relationships between needs, requirements, design, hazards and tests, followed in both directions: from a need down to the tests that prove it is met, and from a failed test up to the needs it puts at risk.'),
    term('Traceability matrix (trace matrix)', 'A table with one row per requirement showing what it derives from, what satisfies it, what verifies it and the latest result. OpenV builds it from the links and exports it as CSV.'),
    term('Verification', 'Confirming that the product was built as the requirements say (“did we build it right?”), by test, inspection, analysis or demonstration.'),
    term('Validation', 'Confirming that the product meets the users’ real needs (“did we build the right thing?”). In OpenV a test case validates a user need.'),
    term('V&V', 'Verification and validation, taken together; also the name of the OpenV view that shows coverage, gaps and test runs.'),
    term('Verification method', 'How a requirement will be verified: test, inspection, analysis or demonstration. A requirement with no method is a gap.'),
    term('Test case', 'A defined procedure that checks one or more requirements, with the expected result. In OpenV it can be automated, manual or physical.'),
    term('Test run', 'One execution of a set of test cases, recording a result for each: pass, fail, blocked or not run.'),
    term('Verification evidence', 'What shows a result is real: logs, photographs, measurements, reports. In OpenV evidence is kept in bundles that results cite, with a digest of each file.'),
    term('Coverage', 'How far the requirements are verified. OpenV rolls each requirement up to the worst of its latest results, or uncovered when nothing verifies it.'),
    term('Gap analysis', 'Listing what is missing from the trace: requirements nothing verifies, needs nothing derives from, hazards nothing mitigates, tests that verify nothing.'),
    term('Baseline', 'A fixed, named snapshot of the requirements set at a moment, such as a review, a release or a contract, kept unchanged so later work can be compared against it.'),
    term('Suspect link', 'A trace link flagged for review because an artifact at one end changed after the link was made. Someone re-reads both ends and confirms it.'),
    term('Change impact analysis', 'Following the trace from an artifact to everything a change to it could affect, before making the change.'),
    term('Flow-down', 'Passing requirements from a system to its subsystems or suppliers, each refining the parent’s, with verification results rolling back up.'),
    term('Review status', 'Where an artifact stands in its review: draft, in review, approved, or superseded by a later approved version.'),
    term('ReqIF', 'The Requirements Interchange Format, an OMG standard XML format for moving requirements between tools. OpenV imports and exports ReqIF 1.x.'),
    term('Audit trail', 'The record of who changed what and when. In OpenV it is the activity log and every artifact’s version history.'),
  ],
  limits: [],
  manual: ['links', 'vv', 'artifacts'],
  related: ['/requirements-traceability', '/verification-evidence', '/baselines-and-audit-trail', '/reqif'],
};

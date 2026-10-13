import type { Topic } from './types';

export const VERIFICATION: Topic = {
  path: '/verification-evidence',
  title: 'Test runs and verification evidence',
  summary:
    'Record test runs and results against requirements, attach and cite verification evidence, track verification status, and produce a V&V status report.',
  footerLabel: 'Verification evidence',
  eyebrow: 'Verification & validation',
  heading: 'Record test runs, keep the evidence, and report verification status',
  answer:
    'In OpenV a test run records a pass, fail, blocked or not-run result for each test case, against the version of the test case that was run and the person or agent who ran it. Evidence files are kept in bundles that results cite, with a SHA-256 digest taken at upload, and every requirement carries a verification rollup that feeds the gap list and a V&V status report you can download as a PDF.',
  updated: '2026-10-13',
  sections: [
    {
      heading: 'Verification methods',
      paragraphs: [
        'Each requirement names how it will be verified: test, inspection, analysis or demonstration. A requirement verified by test is covered by the test cases that verify it; one verified another way is marked verified by a person once the inspection, analysis or demonstration is done. A requirement with no method chosen is listed as a gap.',
      ],
    },
    {
      heading: 'Test runs and results',
      bullets: [
        'A run is in progress, completed or aborted, and can name the baseline it was run against.',
        'Each result records pass, fail, blocked or not-run, the test-case version it applies to, and who executed it.',
        'Recording a case again adds a new result and keeps the old one in the run’s history.',
        'A run that holds results cannot be deleted, only closed, so the record of what was run stays.',
        'Test cases are marked automated, manual or physical. An AI agent can execute and record automated cases; manual and physical ones are recorded by people.',
      ],
    },
    {
      heading: 'Evidence bundles',
      paragraphs: [
        'A bundle (EVD-1, EVD-2, …) is one capture session: a title, a summary, when and by whom it was captured, the conditions, and any files, of any type. Results cite bundles, and one bundle can back results across several runs. Each file’s SHA-256 digest is recorded when it is uploaded and returned when it is downloaded, so a file can be shown to be the one that was captured. Files are always served as downloads, never opened in the browser.',
      ],
    },
    {
      heading: 'Verification status and reports',
      paragraphs: [
        'Each requirement’s rollup is the worst of its latest results (fail, then blocked, then not run, then pass), or uncovered, verified by attestation, or missing a method. The rollups drive the traceability matrix and the gap list. The V&V status report is a PDF of coverage, gaps and test runs, for the live project or for any baseline, and the PDF and Word specifications can include the V&V rollup and the test results.',
      ],
    },
  ],
  limits: [
    'OpenV records results; it does not run tests itself. Results come from people, from an AI agent you run, or from your own tooling through the API.',
    'There are no electronic signatures on results or approvals: who recorded a result and when is logged, but nothing asks the signer to re-authenticate.',
    'Test results and evidence are live, not part of a baseline: a document produced from a baseline shows the results as they stand when it is exported, and says so on its cover. Evidence files are not carried by a JSON or ReqIF export.',
    'One evidence file is capped at 200 MB by default, and evidence storage per hosted workspace depends on its plan.',
  ],
  manual: ['vv'],
  related: ['/requirements-traceability', '/baselines-and-audit-trail', '/safety-critical'],
};

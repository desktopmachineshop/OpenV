import type { Topic } from './types';

export const BASELINES: Topic = {
  path: '/baselines-and-audit-trail',
  title: 'Baselines, version history and audit trail',
  summary:
    'Immutable project baselines, baseline comparison, per-artifact version history, review status, and an activity log of who changed what and when.',
  footerLabel: 'Baselines and audit trail',
  eyebrow: 'Configuration control',
  heading: 'Baselines, version history and an audit trail for regulated work',
  answer:
    'OpenV captures named, immutable baselines of a project, compares any two of them (or a baseline with the live project), and keeps every version of every artifact with who changed it and when. Review status moves each artifact from draft through review to approved, and an activity log records each change, link, status move and baseline under the name of the person or agent who made it.',
  updated: '2026-10-13',
  sections: [
    {
      heading: 'Baselines',
      paragraphs: [
        'A baseline is a named snapshot of the project: every artifact and link, the list of attachments, the product profile and the attribute definitions in force, with who captured it and when. It cannot be edited, only read or deleted, and deleting one is limited to the project owner and recorded. Every download, PDF, Word, Excel, CSV, JSON or ReqIF, can be produced from a baseline instead of the live project, and its cover says which.',
      ],
    },
    {
      heading: 'Comparing baselines',
      paragraphs: [
        'Compare two baselines, or a baseline with the live project, for the artifacts added, removed and modified, which fields changed on each (status included), and the links added and removed. It is the list a change review or a release note starts from.',
      ],
    },
    {
      heading: 'Version history',
      paragraphs: [
        'Every edit and every status change makes a new version of the artifact; earlier versions, and the links they had, stay readable, including those of an artifact that has since been deleted. Figures are versioned too, and restoring an old figure makes it a new version rather than rewriting history.',
      ],
    },
    {
      heading: 'Review status and review rounds',
      paragraphs: [
        'Each artifact is draft, in review, approved or superseded. Editing an approved artifact puts the new version back in draft and keeps the approved one in its history. A review round moves every draft into review in one step, and the review queue lets a reviewer approve an artifact or send it back with the reason recorded as a note. Reviewers from outside the team can be invited with a reviewer link: they can read and comment, never edit.',
      ],
    },
    {
      heading: 'The activity log',
      paragraphs: [
        'Every change is an event with an actor: artifacts created, changed, deleted, restored or moved between statuses; links made, changed and removed; baselines captured and deleted; review rounds started; test results recorded; agent runs and their proposals; and changes to who may see a project. Each artifact also keeps its own feed of notes and changes.',
      ],
    },
  ],
  limits: [
    'There are no electronic signatures: an approval is a recorded status change by a named user, without re-authentication or a signature manifest, so OpenV does not by itself meet 21 CFR Part 11.',
    'Any project editor can approve; there is no separate approver role, and nothing stops an editor approving their own change. Enforce separation of duties by who you make an editor.',
    'The activity log cannot yet be exported as an audit report, and no retention period can be set for it.',
    'A baseline keeps the list of attachments, not the files themselves, and test results and evidence are live rather than part of the snapshot.',
  ],
  manual: ['artifacts', 'vv', 'projects'],
  related: ['/requirements-traceability', '/verification-evidence', '/safety-critical'],
};

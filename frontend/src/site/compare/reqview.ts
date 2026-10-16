import { OPENV, OPENV_LIMITS } from './openv';
import type { Comparison } from './types';

const PRICING = 'https://www.reqview.com/pricing/';

export const REQVIEW: Comparison = {
  path: '/compare/reqview',
  competitor: 'ReqView',
  vendor: 'Eccam',
  title: 'OpenV and ReqView compared',
  summary:
    'Hosted OpenV beside ReqView on price, where the data lives, collaboration, traceability, tests, ReqIF and imports, with when to choose each.',
  heading: 'OpenV as a ReqView alternative: a hosted service beside a desktop tool',
  answer:
    'ReqView is a desktop requirements tool with published per-user prices that keeps projects in local files, on a shared drive or in Git, and imports Word, Excel and ReqIF. OpenV is a hosted service where the whole team works in one live project, with test runs, verification evidence and AI agents, and you can start free today. It imports only ReqIF and its own JSON.',
  updated: '2026-10-16',
  chooseThem: [
    'You want projects kept in local files, on a shared drive or in Git beside your code.',
    'You start from Word or Excel documents and want to import them directly.',
    'You want a published per-user price for a small team.',
    'You want a desktop app for Windows, macOS or Linux.',
  ],
  chooseOpenV: [
    'You want one hosted project the whole team works in at the same time.',
    'You want test runs with recorded results and evidence with SHA-256 digests.',
    'You want reviews from a phone, and public or reviewer links for people outside the team.',
    'You want AI agents that propose changes for review, on an AI subscription you already have.',
  ],
  rows: [
    {
      dimension: 'Price',
      openv: OPENV.price,
      them: 'PRO €440 and TEAM €600 per user per year; ENTERPRISE €1,800 per floating client per year, at least three. Subscriptions only.',
      source: PRICING,
    },
    {
      dimension: 'Where the data lives',
      openv: OPENV.hosted,
      them: 'A desktop app storing projects locally, on a shared or cloud drive, or in Git or SVN; a browser version also runs at desktop.reqview.com.',
      source: 'https://www.reqview.com/doc/faqs/',
    },
    {
      dimension: 'Working together',
      openv: 'Every member works in the same live project at openv.app, with roles, notifications and an activity log.',
      them: 'Teams share projects on a shared drive or through Git or SVN; starting to edit a document locks it for others.',
      source: 'https://www.reqview.com/doc/git-collaboration/',
    },
    {
      dimension: 'Traceability and baselines',
      openv: OPENV.traceability,
      them: 'Custom link types, suspect links, change history with version comparison, and tagged Git commits as baselines.',
      source: 'https://www.reqview.com/doc/requirements-traceability-links/',
    },
    {
      dimension: 'Verification',
      openv: OPENV.tests,
      them: 'Test cases kept as linked documents, a traceability matrix, and the share of requirements verified and tests passed.',
      source: 'https://www.reqview.com/doc/example-project/',
    },
    {
      dimension: 'ReqIF and other formats',
      openv: `${OPENV.reqif} ${OPENV.formats}`,
      them: 'ReqIF import and export; Word and Excel or CSV import; export to Word, Excel, PDF, HTML and CSV.',
      source: 'https://www.reqview.com/doc/import-reqif/',
    },
    {
      dimension: 'Reviews',
      openv: OPENV.reviews,
      them: 'Comments in a discussion column and a review table view; approval recorded with a status attribute.',
      source: 'https://www.reqview.com/doc/review-document/',
    },
    {
      dimension: 'Getting started',
      openv: OPENV.start,
      them: 'A FREE edition limited to one document of up to 150 objects, and a 14-day trial with TEAM features.',
      source: PRICING,
    },
  ],
  moving: [
    'Export ReqIF from ReqView and import it into a new OpenV project, then check counts by type, a sample of bodies, links on artifacts you know well and every enumeration.',
    'OpenV has not been tested against ReqView’s ReqIF yet, so move one document first and open an issue with anything that does not come across.',
  ],
  limits: [
    OPENV_LIMITS.imports,
    'No desktop app: hosted OpenV runs in the browser and needs a connection.',
    'No requirements stored as files in your own Git repository.',
    OPENV_LIMITS.alpha,
  ],
};

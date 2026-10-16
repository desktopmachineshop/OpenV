import { OPENV, OPENV_LIMITS } from './openv';
import type { Comparison } from './types';

const ELM_PRICING = 'https://www.ibm.com/products/engineering-lifecycle-management/pricing';
const RM = 'https://www.ibm.com/products/requirements-management';

export const DOORS: Comparison = {
  path: '/compare/doors',
  competitor: 'IBM DOORS Next',
  vendor: 'IBM',
  title: 'OpenV and IBM DOORS Next compared',
  summary:
    'Hosted OpenV beside IBM DOORS Next on price, hosting, traceability, test management, ReqIF and AI, with when to choose each and how to move.',
  heading: 'OpenV as a DOORS alternative: how it compares with IBM DOORS Next',
  answer:
    'IBM DOORS Next is IBM’s web requirements tool, with variants and product lines managed through global configurations: it is bought by quote, offered as a single-tenant SaaS, and pairs with a separate IBM test product. OpenV is a hosted requirements and V&V tool you can start free today, with tests, evidence and AI agents in the same project. It has no variant management, and no electronic signatures, which IBM lists for DOORS classic.',
  updated: '2026-10-16',
  chooseThem: [
    'Your programme already runs on IBM Engineering Lifecycle Management or DOORS classic.',
    'You manage variants and product lines through global configurations.',
    'Your process needs electronic signatures, which IBM lists for DOORS classic.',
    'You want a single-tenant SaaS with a 99.9% availability SLA.',
  ],
  chooseOpenV: [
    'You want to start without a sales call: a free account and the whole product today.',
    'You want test runs and verification evidence in the same project as the requirements.',
    'You want AI agents that propose changes for review, running on an AI subscription you already have.',
    'You want every export on every plan, and the option to self-host the same product.',
  ],
  rows: [
    { dimension: 'Price', openv: OPENV.price, them: 'Quote only: the pricing page asks you to contact IBM.', source: ELM_PRICING },
    {
      dimension: 'Hosted service',
      openv: OPENV.hosted,
      them: 'IBM ELM is offered as SaaS on a single-tenant site with a 99.9% availability SLA, in two tiers by number of users.',
      source: ELM_PRICING,
    },
    {
      dimension: 'Traceability and baselines',
      openv: OPENV.traceability,
      them: 'Links carry a suspect, valid or invalid status; streams, baselines and global configurations manage change.',
      source: 'https://www.ibm.com/docs/SSYMRC_7.2/com.ibm.jazz.vvc.doc/topics/c_linkval.html',
    },
    {
      dimension: 'Test management',
      openv: OPENV.tests,
      them: 'A separate product, IBM Engineering Test Management, sold within the ELM suite.',
      source: 'https://www.ibm.com/products/ibm-engineering-test-management',
    },
    {
      dimension: 'ReqIF and other formats',
      openv: `${OPENV.reqif} ${OPENV.formats}`,
      them: 'ReqIF import and export; CSV and Excel in and out; Word import.',
      source: 'https://www.ibm.com/docs/en/SSYMRC_7.0.2/com.ibm.rational.rrm.help.doc/topics/t_import_export.html',
    },
    {
      dimension: 'AI',
      openv: OPENV.ai,
      them: 'Scores requirement quality and suggests better wording, which IBM says domain experts approve; also natural-language queries and summaries.',
      source: RM,
    },
    {
      dimension: 'Variants and programmes',
      openv: OPENV.programmes,
      them: 'Variants and product lines through global configuration management, with reuse across parallel streams.',
      source: 'https://www.ibm.com/downloads/documents/us-en/16951f139ce170df',
    },
    {
      dimension: 'Standards',
      openv: OPENV_LIMITS.certification,
      them: 'IBM states support for ASPICE, ISO 26262 and DO-178C.',
      source: RM,
    },
    { dimension: 'Getting started', openv: OPENV.start, them: 'A free trial is offered through IBM.', source: 'https://www.ibm.com/products/engineering-lifecycle-management' },
  ],
  moving: [
    'Export a ReqIF file from DOORS Next, one component or stream at a time (IBM’s documentation says a ReqIF package cannot migrate several at once), and import it into a new OpenV project. Then compare the two before you rely on the copy: artifact counts by type, a sample of bodies, the links on artifacts you know well, and every enumerated attribute.',
    'OpenV’s ReqIF exchange with DOORS Next has not been tested yet, so run the first move as a pilot on one module. If something does not come across, open an issue with the file or a cut-down copy; that is how another tool’s dialect gets supported.',
  ],
  limits: [OPENV_LIMITS.variants, OPENV_LIMITS.signatures, OPENV_LIMITS.approver, OPENV_LIMITS.reqif, OPENV_LIMITS.alpha],
};

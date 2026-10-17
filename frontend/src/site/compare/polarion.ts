import { OPENV, OPENV_LIMITS } from './openv';
import type { Comparison } from './types';

const REQS = 'https://www.siemens.com/en-us/products/polarion/requirements/';

export const POLARION: Comparison = {
  path: '/compare/polarion',
  competitor: 'Siemens Polarion',
  vendor: 'Siemens',
  title: 'OpenV and Siemens Polarion compared',
  summary:
    'Hosted OpenV beside Siemens Polarion X on price, cloud hosting, traceability, tests, e-signatures, ReqIF and AI, with when to choose each.',
  heading: 'OpenV as a Polarion alternative: hosted OpenV beside Polarion X',
  answer:
    'Siemens Polarion is a full application lifecycle management suite, offered in the cloud as Polarion X, with requirements, tests, Word round trips, e-signatures and variant configuration. OpenV is a narrower hosted tool for requirements, traceability and verification evidence that you can start free today, with AI agents on your own AI subscription. It has no electronic signatures, Word import or variant configuration.',
  updated: '2026-10-16',
  chooseThem: [
    'You want requirements, tests and the wider ALM in one Siemens suite.',
    'Stakeholders must e-sign specifications as reviewed or approved.',
    'Your authors work in Word and need a round trip back into the tool.',
    'You configure product variants, which Polarion Variant Configurator handles.',
  ],
  chooseOpenV: [
    'You want requirements and V&V without a full ALM suite, starting free today.',
    'You want AI agents on an AI subscription you already have, rather than an AI add-on.',
    'You want every export on every plan, never behind a paywall.',
    'You want the option to self-host the same product free.',
  ],
  rows: [
    {
      dimension: 'Price',
      openv: OPENV.price,
      them: 'Priced through Siemens sales: the Polarion X plans page offers a call with sales and a trial.',
      source: 'https://www.siemens.com/en-us/products/polarion/polarion-x/plans-pricing/',
    },
    {
      dimension: 'Hosted service',
      openv: OPENV.hosted,
      them: 'Polarion X, hosted by Siemens, with everything in on-premises Polarion.',
      source: 'https://www.siemens.com/en-us/products/polarion/polarion-x/',
    },
    {
      dimension: 'Traceability and baselines',
      openv: OPENV.traceability,
      them: 'Custom link roles, suspect links, and baselines at document and project level.',
      source:
        'https://docs.plm.automation.siemens.com/content/polarion/20/help/en_US/user_and_administration_help/user_guide/work_with_documents/document_baselines.html',
    },
    {
      dimension: 'Test management',
      openv: OPENV.tests,
      them: 'Test management is part of Polarion ALM and Polarion QA.',
      source: 'https://www.siemens.com/en-us/products/polarion/',
    },
    {
      dimension: 'Reviews and approval',
      openv: OPENV.reviews,
      them: 'Stakeholders can e-sign specifications as reviewed or approved.',
      source: REQS,
    },
    {
      dimension: 'ReqIF and other formats',
      openv: `${OPENV.reqif} ${OPENV.formats}`,
      them: 'Built-in ReqIF exchange; Word and Excel import with round trip; Word and PDF export.',
      source: REQS,
    },
    {
      dimension: 'AI',
      openv: OPENV.ai,
      them: 'Polarion Copilot checks wording against INCOSE rules and finds similar and inconsistent items; an add-on on the Polarion X Standard, Advanced, Automotive and Premium plans.',
      source: 'https://blogs.sw.siemens.com/polarion/polarion-x-2512-whats-new-and-noteworthy/',
    },
    {
      dimension: 'Variants and programmes',
      openv: OPENV.programmes,
      them: 'Polarion Variant Configurator, which replaced Polarion VARIANTS in release 2606.',
      source: 'https://blogs.sw.siemens.com/polarion/polarion-alm-2606-whats-new-and-noteworthy/',
    },
    {
      dimension: 'Getting started',
      openv: OPENV.start,
      them: 'A free trial of Polarion X in the cloud, or a download of self-managed Polarion.',
      source: 'https://www.siemens.com/en-us/products/polarion/trials/',
    },
  ],
  moving: [
    'Export ReqIF from Polarion and import it into a new OpenV project, then check counts by type, a sample of bodies, links on artifacts you know well and every enumeration.',
    'OpenV’s ReqIF exchange with Polarion has not been tested yet, so run the first move as a pilot on one document and open an issue with anything that does not come across.',
  ],
  limits: [OPENV_LIMITS.signatures, OPENV_LIMITS.imports, OPENV_LIMITS.variants, OPENV_LIMITS.reqif, OPENV_LIMITS.alpha],
};

import { OPENV, OPENV_LIMITS } from './openv';
import type { Comparison } from './types';

const PRICING = 'https://www.jamasoftware.com/pricing';

export const JAMA: Comparison = {
  path: '/compare/jama',
  competitor: 'Jama Connect',
  vendor: 'Jama Software',
  title: 'OpenV and Jama Connect compared',
  summary:
    'Hosted OpenV beside Jama Connect on price, cloud hosting, traceability, test management, reviews, ReqIF and AI, with when to choose each.',
  heading: 'OpenV as a Jama Connect alternative: what each does',
  answer:
    'Jama Connect is a requirements platform offered as a Jama-run cloud, with built-in test management, a Review Center with approver roles, and published frameworks for medical and airborne work; it is priced by quote. OpenV is a hosted requirements and V&V tool you can start free today, with test evidence and AI agents on your own AI subscription. It has no electronic signatures or separate approver role yet.',
  updated: '2026-10-16',
  chooseThem: [
    'Your reviews need separate moderator, reviewer and approver roles and signing that asks for the password again.',
    'You want vendor frameworks for IEC 62304, ISO 14971, DO-178C or ARP4754 work.',
    'You need a US government cloud: Jama offers AWS GovCloud.',
  ],
  chooseOpenV: [
    'You want to see the price and start today: a free account, no sales call.',
    'You want AI agents on an AI subscription you already have, rather than an AI add-on.',
    'You want every export, ReqIF included, on every plan.',
    'You want the option to self-host the same product free.',
  ],
  rows: [
    {
      dimension: 'Price',
      openv: OPENV.price,
      them: 'Quote only. Jama says hosting and reviewer licences are free.',
      source: PRICING,
    },
    {
      dimension: 'Hosted service',
      openv: OPENV.hosted,
      them: 'Jama Connect Cloud, run by Jama, and AWS GovCloud (US).',
      source: 'https://www.jamasoftware.com/deployment-options/',
    },
    {
      dimension: 'Traceability and baselines',
      openv: OPENV.traceability,
      them: 'Custom relationship types and rules, suspect links, impact analysis and baseline comparison.',
      source: 'https://help.jamasoftware.com/ah/en/manage-content/baselines/compare-baselines.html',
    },
    {
      dimension: 'Test management',
      openv: OPENV.tests,
      them: 'Built in: test cases, test runs and test cycles, with a Test Runner licence type.',
      source: 'https://help.jamasoftware.com/ah/en/test/test-runs.html',
    },
    {
      dimension: 'Reviews and approval',
      openv: OPENV.reviews,
      them: 'Review Center with moderator, reviewer and approver roles; signing asks the user to log in again.',
      source: 'https://www.jamasoftware.com/media/2023/12/jama-connect-and-fda-21-cfr-part-11.pdf',
    },
    {
      dimension: 'ReqIF and other formats',
      openv: `${OPENV.reqif} ${OPENV.formats}`,
      them: 'ReqIF through Jama Connect Interchange, which names DOORS, DOORS Next, Codebeamer and Polarion as exchange partners.',
      source: 'https://www.jamasoftware.com/media/2024/06/jama-connect-interchange-for-ReqIF-datasheet.pdf',
    },
    {
      dimension: 'Word and Excel',
      openv: 'Export to Word and Excel; no Word or Excel import.',
      them: 'Word and Excel import and export.',
      source: 'https://support.jamasoftware.com/hc/en-us/sections/47011460363533',
    },
    {
      dimension: 'AI',
      openv: OPENV.ai,
      them: 'Jama Connect Advisor checks wording against INCOSE rules and EARS and drafts test cases; engineers accept its suggestions. A paid add-on for cloud customers.',
      source: 'https://help.jamasoftware.com/ah/en/jama-connect-advisor-.html',
    },
    {
      dimension: 'Regulated frameworks',
      openv: OPENV_LIMITS.certification,
      them: 'Frameworks for medical devices (IEC 62304, ISO 14971, ISO 13485) and airborne systems (DO-178C, DO-254, ARP4754).',
      source: 'https://www.jamasoftware.com/solution-overview/jama-connect-airborne-systems-solution-overview/',
    },
    {
      dimension: 'Getting started',
      openv: OPENV.start,
      them: 'A free trial on request.',
      source: 'https://www.jamasoftware.com/jama-connect-free-trial/',
    },
  ],
  moving: [
    'Export ReqIF from Jama through Jama Connect Interchange and import it into a new OpenV project, then check counts by type, a sample of bodies, links on artifacts you know well and every list attribute.',
    'OpenV’s ReqIF exchange with Jama has not been tested yet, so run the first move as a pilot on one project and open an issue with anything that does not come across.',
  ],
  limits: [OPENV_LIMITS.signatures, OPENV_LIMITS.approver, OPENV_LIMITS.certification, OPENV_LIMITS.reqif, OPENV_LIMITS.alpha],
};

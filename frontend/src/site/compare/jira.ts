import { OPENV, OPENV_LIMITS } from './openv';
import type { Comparison } from './types';

const R4J = 'https://marketplace.atlassian.com/apps/1213064/easerequirements-requirements-management-for-jira-r4j';

export const JIRA: Comparison = {
  path: '/compare/jira',
  competitor: 'Jira and Confluence',
  vendor: 'Atlassian',
  title: 'Requirements in Jira, and when a dedicated tool fits',
  summary:
    'What Jira and Confluence give you for requirements, what needs Marketplace apps, and when a dedicated requirements and V&V tool such as OpenV fits better.',
  heading: 'Requirements management in Jira, and when a dedicated requirements tool fits',
  answer:
    'Jira gives you custom work types and custom link types, which suits teams that keep requirements as work items. Test management, ReqIF exchange and electronic signatures are offered by Marketplace apps, and apps such as R4J sell requirement baselines and traceability. OpenV puts requirements, traceability, baselines, test runs and evidence in one hosted project, but it has no live link into Jira.',
  updated: '2026-10-16',
  chooseThem: [
    'Your requirements are user stories and your team already plans its work in Jira.',
    'Your requirements live as work items and you do not need a dedicated requirements tool.',
    'Up to ten people can work on the Free plan.',
    'You want Atlassian’s AI, Rovo, which comes with the Standard plan and above.',
  ],
  chooseOpenV: [
    'You need baselines, a traceability matrix and gap reports without assembling apps.',
    'You record test runs and verification evidence against the requirement version they verify.',
    'You exchange requirements with suppliers or customers by ReqIF.',
    'You want stable requirement references and reviews that live with the specification, not in tickets.',
  ],
  rows: [
    {
      dimension: 'Price',
      openv: OPENV.price,
      them: 'Free for up to 10 users; Standard, Premium and Enterprise plans are priced per user on Atlassian’s pricing page.',
      source: 'https://www.atlassian.com/software/jira/pricing',
    },
    {
      dimension: 'Requirements',
      openv: 'Requirements, user needs, design items, hazards and test cases as typed artifacts with stable references and version history.',
      them: 'Custom work types and custom link types.',
      source: 'https://support.atlassian.com/jira-cloud-administration/docs/what-are-issue-types/',
    },
    {
      dimension: 'Baselines and trace matrix',
      openv: OPENV.traceability,
      them: 'Offered by Marketplace apps such as R4J (easeRequirements), which sells baselining and traceability.',
      source: R4J,
    },
    {
      dimension: 'Test management',
      openv: OPENV.tests,
      them: 'Through Marketplace apps such as Xray and Zephyr.',
      source: 'https://marketplace.atlassian.com/apps/1211769/xray-test-management-for-jira',
    },
    {
      dimension: 'ReqIF',
      openv: OPENV.reqif,
      them: 'Through third-party apps.',
      source: 'https://marketplace.atlassian.com/apps/1220602/reqif4jira',
    },
    {
      dimension: 'Electronic signatures',
      openv: OPENV_LIMITS.signatures,
      them: 'Through Marketplace apps such as Vera Electronic Signatures for Jira.',
      source: 'https://marketplace.atlassian.com/apps/1220599/vera-electronic-signatures-for-jira',
    },
    {
      dimension: 'AI',
      openv: OPENV.ai,
      them: 'Rovo searches in plain language, drafts and summarises, suggests child work items and flags likely duplicates; on Standard, Premium and Enterprise.',
      source: 'https://support.atlassian.com/organization-administration/docs/understand-atlassian-intelligence-features-in-products',
    },
  ],
  moving: [
    'Keep Jira for delivery and put the specification in OpenV: requirements, hazards and tests with their links, baselines and evidence. Reference OpenV’s stable refs (REQ-12) in Jira work items, or move data across with OpenV’s API.',
    'There is no Jira import: bring requirements in by ReqIF if an app on your Jira exports it, or through the API.',
  ],
  limits: [OPENV_LIMITS.jira, OPENV_LIMITS.imports, OPENV_LIMITS.signatures, OPENV_LIMITS.alpha],
};

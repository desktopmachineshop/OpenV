import type { Topic } from './types';

export const REQIF: Topic = {
  path: '/reqif',
  title: 'ReqIF import and export',
  summary:
    'What OpenV reads and writes in ReqIF 1.x, what survives a round trip, what does not, and how to check an exchange with DOORS, Polarion or another tool.',
  footerLabel: 'ReqIF import and export',
  eyebrow: 'Interchange',
  heading: 'ReqIF import and export: what goes across and what does not',
  answer:
    'OpenV exports any project or baseline as ReqIF 1.x and imports a ReqIF file as a new project, keeping the hierarchy, the links, status and custom attributes, with list attributes as ReqIF enumerations. A project exported from OpenV imports back intact, and that round trip is what the tests prove; files from other tools are read on a best-effort basis, and an exchange with DOORS or Polarion has not been tested yet.',
  updated: '2026-10-13',
  sections: [
    {
      heading: 'What an export carries',
      bullets: [
        'every artifact as a SPEC-OBJECT with its identifier, title, text, status and version, and its own last-change time',
        'the body as XHTML, so line breaks and paragraphs survive',
        'custom attributes, with list-typed ones written as enumerations from the project’s attribute definitions',
        'the document hierarchy as a SPEC-HIERARCHY, and links as SPEC-RELATIONs',
        'the live project or any baseline, narrowed by section, type or owner like any other download',
      ],
    },
    {
      heading: 'What an import does',
      paragraphs: [
        'Importing a ReqIF file creates a new project: fresh identifiers, every artifact at version 1, with the hierarchy, links, statuses and attributes rebuilt from the file. A value outside an enumeration’s list is refused with a message naming it, rather than imported as something else. Today a ReqIF file is imported through the API (POST /api/v1/projects/import); the app’s import button takes OpenV’s own JSON export.',
      ],
    },
    {
      heading: 'Moving from another tool',
      paragraphs: [
        'Export a ReqIF file from the tool you are leaving, import it into a new OpenV project, then compare the two before you rely on the copy: artifact counts per type, a sample of bodies, the links on a few artifacts you know well, and every enumeration. If something is missing, open an issue with the file (or a cut-down one) and what you expected; that is how support for another tool’s dialect gets built.',
      ],
    },
  ],
  limits: [
    'Interoperability with DOORS, DOORS Next, Polarion or Jama has not been tested yet; the tests cover OpenV-to-OpenV round trips. Treat a first exchange with another tool as a pilot and check the result.',
    'Attachments and embedded objects are not carried in either direction.',
    'An import always makes a new project: there is no merge of a ReqIF file into an existing one.',
    'ReqIF import is through the API only for now; the import button in the app accepts OpenV’s JSON export.',
    'Besides ReqIF, OpenV imports only its own JSON export. There is no Excel or CSV import.',
  ],
  manual: ['projects', 'artifacts'],
  related: ['/self-hosted-requirements-management', '/baselines-and-audit-trail'],
};

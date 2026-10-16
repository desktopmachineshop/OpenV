import type { Topic } from './types';

export const AI_CONNECTOR: Topic = {
  path: '/ai-connector',
  title: 'Connect Claude and other AI apps to OpenV',
  summary:
    'Add OpenV to Claude, Cursor or any remote MCP client by its address, sign in, approve one workspace, and work on requirements, links and V&V evidence from the chat.',
  footerLabel: 'Connect Claude to OpenV',
  eyebrow: 'AI connector',
  heading: 'Connect Claude and other AI apps to an OpenV workspace',
  answer:
    'OpenV is a remote MCP server at https://openv.app/mcp. Add that address to Claude as a connector, or to any app that speaks MCP, then sign in to OpenV and approve one workspace with read-only or read-and-change access. The app then works on that workspace as you, with your permissions, until you disconnect it.',
  updated: '2026-10-16',
  sections: [
    {
      heading: 'Set it up in Claude',
      bullets: [
        'In Claude (claude.ai, the desktop app or the mobile app), open Settings → Connectors and choose Add custom connector.',
        'Name it OpenV and paste https://openv.app/mcp, without www.',
        'Claude opens OpenV in your browser: sign in if asked, choose the workspace, choose read and change or read only, and choose Connect.',
        'On a Claude Team or Enterprise plan, an owner of the Claude organization adds the connector first; each member then connects their own OpenV account.',
      ],
    },
    {
      heading: 'Set it up in another app',
      paragraphs: [
        'Add OpenV as a remote MCP server at https://openv.app/mcp. It uses OAuth 2.0 with dynamic client registration, so the app registers itself and sends you to OpenV to approve it; there is no key to copy. A self-hosted OpenV serves the same endpoint at its own address, shown under Settings → Connected apps.',
      ],
    },
    {
      heading: 'What you can ask it',
      bullets: [
        '"Map the Braking project and list its V&V gaps": the project outline, coverage and the requirements no test verifies.',
        '"Draft a requirement for the lockout after five failed logins": written in the project’s house style and checked against its quality rules before it is saved.',
        '"Link REQ-12 to the test that verifies it and record today’s run as passed": traceability links, test runs and results as verification evidence.',
        '"Capture a baseline called Release 2": an immutable snapshot of the project.',
      ],
    },
    {
      heading: 'The tools',
      paragraphs: [
        'Read-only tools list and search projects and artifacts, read an artifact with its context, map a project, read baselines, links, work items, quality rules and findings, and report V&V coverage and gaps. Write tools create and update artifacts, create, confirm and delete links, add comments, record test runs and results, capture baselines, record candidate needs and start a review round. Every tool tells the app whether it only reads, adds, or changes or deletes what is already there, so Claude asks before it edits an artifact or deletes a link.',
      ],
    },
    {
      heading: 'Permissions and disconnecting',
      paragraphs: [
        'The app acts as you in the one workspace you approved, and can do only what you can do there; a read-only connection is offered only the read tools. Settings → Connected apps lists each app you have connected, the workspace and access it has, and when it was last used. Disconnect stops it at once. To use another workspace, connect the app again and choose that one.',
      ],
    },
    {
      heading: 'Troubleshooting',
      bullets: [
        '"Couldn’t check the server" with a 301: the address has www in it. Use https://openv.app/mcp.',
        'The app says it is not authorised: disconnect it in Settings → Connected apps and connect it again.',
        'Writes are refused: the app was approved read-only, or your role in the workspace cannot make that change.',
      ],
    },
  ],
  limits: [
    'One connection reaches one workspace; to work in two, connect the app twice.',
    'The connector works with the content of a workspace, not its settings: it cannot invite members, change billing or manage runners.',
    'What the AI app does with the data it reads, such as how it keeps your conversations, is governed by that app’s own terms, not OpenV’s.',
  ],
  manual: ['runs-runners', 'agents', 'vv'],
  related: ['/requirements-traceability', '/verification-evidence', '/baselines-and-audit-trail'],
};

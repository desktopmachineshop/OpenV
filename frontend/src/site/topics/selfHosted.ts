import type { Topic } from './types';

export const SELF_HOSTED: Topic = {
  path: '/self-hosted-requirements-management',
  title: 'Self-hosted requirements management',
  summary:
    'Run OpenV on your own servers with Docker: what it needs, how upgrades and backups work, sign-in options, and what the source-available Elastic License 2.0 allows.',
  footerLabel: 'Self-hosting',
  eyebrow: 'Self-hosting',
  heading: 'Self-hosted requirements management, free under a source-available licence',
  answer:
    'OpenV runs on your own servers from its public source with Docker Compose: PostgreSQL, the API, the web app and an optional runner pool, every feature included, with no licence key, phone-home or seat count. It is source-available under the Elastic License 2.0, which lets you run, read, fork and modify it for your own organisation and forbids only selling it to others as a hosted service.',
  updated: '2026-10-13',
  sections: [
    {
      heading: 'What it takes',
      bullets: [
        'A Linux host with Docker and Docker Compose. Three commands start it: clone the repository, change into it, and run docker compose up -d.',
        'For production, a second compose file on top of the first adds health checks, stops publishing the database port and serves the app through nginx; the stack is sized at 1 GB of memory each for PostgreSQL and the API.',
        'Three settings at minimum: a database password, the address the app is served from, and a key for runners. The full list is documented.',
        'A reverse proxy for TLS in front of it: the stack serves plain HTTP inside your network.',
        'PostgreSQL 15. The pgvector extension is optional and only used for semantic search and duplicate detection.',
      ],
    },
    {
      heading: 'Upgrades and backups',
      paragraphs: [
        'The API applies its own database migrations when it starts, so an upgrade is a pull and a restart. Between stable releases the API, the agent tools, the export formats and the runner protocol stay compatible, and anything removed is announced two stable releases ahead. One command writes a backup archive of the database and both file volumes, one restores it, and an optional sidecar takes a backup every day and keeps a week of them.',
      ],
    },
    {
      heading: 'Sign-in and access',
      paragraphs: [
        'Accounts sign in with a password, with optional email verification if you give it an SMTP server, with Google, or with one OpenID Connect provider of your choice. Inside, access works as on the hosted service: workspaces, teams, and owner, editor, reviewer and viewer roles per project.',
      ],
    },
    {
      heading: 'Your data, either way',
      paragraphs: [
        'A JSON export from the hosted service restores into a self-hosted OpenV as a new project, and the other way round, so you can start hosted and move in-house later, or keep a copy of everything at home. Export never depends on a plan.',
      ],
    },
    {
      heading: 'AI agents on your own terms',
      paragraphs: [
        'Agents run on a machine you control, through the Agent Connector or the agentd runner, using your own Claude Code, Codex or Gemini subscription or your own API keys. The OpenV server never runs a model itself.',
      ],
    },
    {
      heading: 'The licence in plain words',
      bullets: [
        'Self-host free, at any scale, with every feature.',
        'Read, fork and modify the source; keep your changes private or publish them.',
        'A consultant or integrator may charge to install, customise or support a customer’s own OpenV.',
        'Not allowed: offering OpenV, or a substantial part of it, to third parties as a hosted or managed service.',
        'The Elastic License 2.0 is source-available, not an OSI open-source licence.',
      ],
    },
  ],
  limits: [
    'The API runs as a single instance; there is no clustered or high-availability setup, and no Helm chart for Kubernetes.',
    'TLS, SAML single sign-on and multi-factor authentication are not built in: put a reverse proxy in front for TLS, and use an OpenID Connect provider that enforces MFA if you need it.',
    'Backups are written unencrypted to the host; encrypt and move them off the machine yourself.',
    'A JSON or ReqIF export does not carry attachment files, so moving between instances leaves figures and evidence behind unless you copy the file volumes too.',
    'There is no paid support contract for self-hosting yet; help is through GitHub issues.',
  ],
  manual: ['getting-started', 'runs-runners', 'org-settings'],
  related: ['/reqif', '/safety-critical', '/baselines-and-audit-trail'],
};

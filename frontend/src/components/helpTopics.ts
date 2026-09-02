// Help content and the route→topic mapping for the floating help bubble.
//
// Kept apart from the component so the mapping is a pure function over a
// pathname: no router, no React, directly testable. HelpSidebar.tsx renders
// whatever this resolves.
//
// Chapter slugs must match MANUAL_CHAPTERS in src/manual/index.ts — they are
// deep-linked as /manual/<slug>.

export interface ChapterLink {
  slug: string;
  title: string;
}

export interface HelpTopic {
  /** Panel heading — the page the user is on, in their vocabulary. */
  title: string;
  /** One-or-two-sentence orientation for this page. */
  summary: string;
  /** 3-5 practical tips, sourced from the manual chapter content. */
  tips: string[];
  /** The most relevant manual chapter for this page. */
  chapter: ChapterLink;
  /** Optional extra chapters worth surfacing on this page. */
  also?: ChapterLink[];
}

const DEFAULT_TOPIC: HelpTopic = {
  title: 'Project overview',
  summary:
    'The Overview page shows your product framing and stakeholder interviews, and is the jumping-off point for defining the product.',
  tips: [
    'The guided definition shortcut here adapts to where you are — its label reads Start, Resume, or Modify.',
    'Stakeholder interviews live on this page, in the User interviews section.',
    'Switch workspaces from the top of the dark left sidebar; your choice is remembered between visits.',
    'For AI agents to work in this project, a runner must be online — see Runs & runners in the manual.',
  ],
  chapter: { slug: 'getting-started', title: 'Getting started' },
};

// Keyed by the first path segment after /projects/:projectId, plus the
// "projects" key for the project list itself (see helpTopicForPath).
const HELP_TOPICS: Record<string, HelpTopic> = {
  projects: {
    title: 'Projects',
    summary:
      'Every project in the workspace you are currently in. Start one from scratch, from a template, or from an example, or bring an existing project in from a JSON export.',
    tips: [
      'New Project offers four starting points: a blank project, one of your saved templates, a bundled example, or a random joke product for trying the tool out.',
      'Import Project takes a JSON file produced by a project export, so a project can move between workspaces or instances.',
      '⧉ on a card saves that project as a reusable template; ✎ renames it and edits its description.',
      'Export a project as JSON to re-import it whole, or as CSV to get its artifacts as flat spreadsheet rows.',
      'Projects belong to the workspace shown in the top bar — switch workspaces there to see a different set.',
    ],
    chapter: { slug: 'projects', title: 'Projects & members' },
    also: [
      { slug: 'getting-started', title: 'Getting started' },
      { slug: 'org-settings', title: 'Workspace settings & teams' },
    ],
  },
  requirements: {
    title: 'Requirements',
    summary:
      'The artifact tree — requirements, user needs, test cases, hazards, design items — with an editor, version history, attachments, comments, and baselines.',
    tips: [
      'Right-click an artifact in the tree to create before / after / child; new siblings inherit the type and parent of the artifact you clicked.',
      'Every save creates a new version — use History to preview and restore any earlier version; nothing is lost.',
      'The ⚙ button opens filter rows you can combine with AND/OR and save as named presets.',
      'Capture a baseline before big changes — baselines are read-only snapshots you can compare and report on.',
      'Use the Chatter panel on the right for review discussion instead of editing the requirement text itself.',
    ],
    chapter: { slug: 'artifacts', title: 'Requirements & artifacts' },
    also: [{ slug: 'links', title: 'Traceability links' }],
  },
  baselines: {
    title: 'Baseline comparison',
    summary:
      'A baseline is an immutable snapshot of the whole project — artifacts plus links — captured at a point in time.',
    tips: [
      'Baseline views are read-only — switch back to Live Project in the Requirements view to edit.',
      'Capture and delete baselines from the top bar of the Requirements view.',
      'Generate Report works for the live project or any baseline.',
      'The V&V dashboard and the Matrix have their own baseline selectors for historical views.',
    ],
    chapter: { slug: 'artifacts', title: 'Requirements & artifacts' },
  },
  guided: {
    title: 'Guided Definition',
    summary:
      'An eight-step wizard from a blank product to a committed, traceable requirement set — with an AI copilot suggesting entries you can add with one click.',
    tips: [
      'Moving Next materializes new entries as draft artifacts (green dot); edit them later in the Requirements view.',
      "Click a copilot suggestion card's add button to insert it into the current step; replacements update an entry in place.",
      'Abandon session keeps drafts already created — only unsaved entries are discarded.',
      "If the copilot doesn't answer, no runner is online; your messages are saved and answered once one connects.",
      'After committing, take the offered "Initial requirements" baseline as your starting snapshot.',
    ],
    chapter: { slug: 'guided-wizard', title: 'Guided definition & copilot' },
  },
  interviews: {
    title: 'Interviews',
    summary:
      'Stakeholders talk to an AI interviewer through a public link — no account needed — and candidate user needs are recorded as it learns them.',
    tips: [
      'Copy invite link creates a token-authenticated URL; participants just enter their name and chat.',
      'Link interviews to a persona to compare how different people in the same role describe their needs.',
      'Closing an interview revokes its invite links permanently — create a new interview for more sessions.',
      'Candidate needs land as draft user-need artifacts tagged with their session — review them in Requirements.',
      'The interviewer agent needs a runner online; a hosted runner keeps interviews responsive around the clock.',
    ],
    chapter: { slug: 'interviews', title: 'Interviews' },
  },
  vv: {
    title: 'V&V',
    summary:
      'How well your requirements are verified: coverage rollups, gaps, test runs, and a downloadable status report.',
    tips: [
      "Each requirement's rollup comes from its verification method plus the latest result of every verifying test.",
      'Start with the Gaps section — an empty one means full coverage.',
      'Pin a test run to a baseline to record a campaign against a fixed snapshot.',
      'Completed or aborted test runs become read-only; the latest result per test case feeds the rollup and the Matrix chips.',
      'Download the V&V report (PDF) for the live project or any baseline.',
    ],
    chapter: { slug: 'vv', title: 'V&V & test runs' },
  },
  matrix: {
    title: 'Traceability Matrix',
    summary:
      'One row per requirement: the user needs it derives from, design outputs, verifying test cases with their latest results, and linked hazards.',
    tips: [
      "Result chips show each verifying test's latest recorded result — pass, fail, blocked, or not-run.",
      'Links are typed and directional (verifies goes from test-case to requirement); invalid combinations are rejected with an explanation.',
      'Use the baseline selector to view the matrix as of any snapshot.',
      'The quick filter matches across all columns; Export CSV downloads the matrix for spreadsheets or reports.',
    ],
    chapter: { slug: 'links', title: 'Traceability links' },
  },
  board: {
    title: 'Board',
    summary:
      'A kanban view of work items — and cards assigned to an AI agent launch a run when you drag them to To Do.',
    tips: [
      'Assign a card to an agent (🤖) and drag it to To Do to kick off an agent run.',
      "A pulsing blue dot means a live run — the card is locked until the run finishes (cancel it from the Runs page if needed).",
      'Attach requirements by listing artifact IDs in the card composer; they show as links in the card drawer.',
      "Crew-assigned cards don't launch from the board — run crews from the Crew page or an automation.",
      "The agent's progress and comments land on the card's activity feed.",
    ],
    chapter: { slug: 'board', title: 'Kanban board' },
  },
  crew: {
    title: 'Crew',
    summary:
      'A crew is a group of agents (and optionally people) arranged in an org-chart graph — for work that naturally splits into draft, review, and sign-off.',
    tips: [
      'Click Connect, then the source and target nodes, to add a delegates-to, hands-off-to, or reviews edge.',
      'Hands-off or review edges pointing at a person create a board card assigned to them when the work arrives.',
      'Drag nodes to arrange them — positions are saved per view (Org Chart and Network).',
      '▶ Run crew launches with a prompt and takes you to the Runs page to follow along.',
      "Start from default crew clones the workspace's default crew as a starting point.",
    ],
    chapter: { slug: 'crews', title: 'Crews' },
  },
  agents: {
    title: 'Agents',
    summary:
      'Agent definitions — provider, model, write mode, tools, and system prompt — plus a launch button for one-off runs.',
    tips: [
      'Keep launch prompts lean — the agent fetches requirement content itself through its OpenV tools; name the work instead of pasting text.',
      'Prefer proposal write mode: every agent write waits for your approval on the Runs page before it lands.',
      'Set Model to provider default to inherit the workspace-wide default (Workspace settings → AI Providers).',
      'Runs needing repository access execute only on personal or workspace runners — never the hosted runner.',
      'Agents are markdown files under the hood — Sync from disk re-reads definitions edited outside the app.',
    ],
    chapter: { slug: 'agents', title: 'Agents' },
  },
  'agent-runs': {
    title: 'Runs',
    summary:
      'Every piece of agent work is a run, executed by runners — worker processes on real machines. This page lists them with status, progress, and cost.',
    tips: [
      'Runs stuck at queued mean no runner is online — pair the Agent Connector, or ask an admin to provision the hosted runner.',
      'Pending approvals at the top opens proposal review — approve or reject each proposed change before it is applied.',
      'Your personal runner claims only runs you launched; ownerless runs (automations, board triggers) go to any live runner.',
      'Click a run for its prompt, live log, results, and related runs such as crew delegations.',
      '"Reserved for launcher\'s runner" means a queued run is briefly waiting for that person\'s personal runner before others may claim it.',
    ],
    chapter: { slug: 'runs-runners', title: 'Runs & runners' },
  },
  automations: {
    title: 'Automations',
    summary:
      'Launch agent or crew runs without anyone clicking a button — manually, on a cron schedule, or triggered by project events.',
    tips: [
      'Triggered automations listen to project events (artifact.created, workitem.moved, …) and can filter on the event payload.',
      'Use the cooldown and max-runs-per-hour safety valves to keep triggered automations from running away.',
      'Scheduled and triggered runs are ownerless — a hosted runner keeps them moving when nobody is online.',
      'Keep prompt templates lean; the agent fetches artifact content itself at run time.',
      'Run now launches immediately and jumps you to the Runs page focused on the new run.',
    ],
    chapter: { slug: 'automations', title: 'Automations' },
  },
  activity: {
    title: 'Activity',
    summary:
      "The project's event log. If something looks stuck or read-only, the answers are usually in Troubleshooting.",
    tips: [
      'Runs staying queued means no runner is online — start your Agent Connector or use the hosted runner.',
      "Everything suddenly read-only? You're probably viewing a baseline — switch the selector back to Live.",
      "Can't find an agent's output? Check Pending approvals on the Runs page — proposal-mode writes wait there.",
      'A board card that won\'t drag has a live run (pulsing blue dot) — wait for it to finish or cancel it.',
    ],
    chapter: { slug: 'troubleshooting', title: 'Troubleshooting & FAQ' },
  },
  settings: {
    title: 'Project Settings',
    summary:
      'Per-project access, agent run authentication, and connected repositories. Workspace-wide people and provider settings live in Workspace settings.',
    tips: [
      'Workspace membership gets someone into the workspace; project access is granted here (Access) — directly or via teams.',
      'Adding a member by email requires that they already have an OpenV account — no invite emails are sent.',
      'The Agents tab picks run auth: user account (the launcher\'s own CLI sign-in) or the workspace API key.',
      'Repositories connect here; each member sets their own local checkout path for their runner.',
      'Teams (groups of people, for access) are not crews (graphs of AI agents that execute work).',
    ],
    chapter: { slug: 'org-settings', title: 'Workspace settings & teams' },
  },
};

// Resolve the help topic from the current pathname. Inside ProjectLayout the
// path is /projects/:projectId/<module>/…, so key off the segment after the
// project id (vv/runs/:runId and baselines/:id/compare resolve via their
// first segment; team/* redirects to crew/* before this ever renders).
export const helpTopicForPath = (pathname: string): HelpTopic => {
  const segments = pathname.split('/').filter(Boolean);
  // /projects with no id is the project list, which has no module segment of
  // its own; inside a project the default topic is the overview.
  if (segments[0] === 'projects' && segments.length === 1) {
    return HELP_TOPICS.projects;
  }
  const moduleSegment = segments[0] === 'projects' ? segments[2] : segments[0];
  return (moduleSegment && HELP_TOPICS[moduleSegment]) || DEFAULT_TOPIC;
};

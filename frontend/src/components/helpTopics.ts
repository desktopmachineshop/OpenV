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
    'Switch workspaces from the workspace name at the left of the top bar (at the top of the menu on a phone); your choice is remembered between visits.',
    'For AI agents to work in this project, a runner must be online — see Runs & runners in the manual.',
    'Press ? or F1 on any page to open this help; the rail down the left reaches every page of the project.',
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
      'Import Project takes a JSON file produced by a project export, so a project can move between workspaces or instances, or a ReqIF file from another requirements tool; either becomes a new project.',
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
      '‹ / › beside an artifact read the project in tree order — J and K do the same from the keyboard, and a sideways swipe does it on a phone.',
    ],
    chapter: { slug: 'artifacts', title: 'Requirements & artifacts' },
    also: [{ slug: 'links', title: 'Traceability links' }],
  },
  baselines: {
    title: 'Baselines',
    summary:
      'A baseline is an immutable snapshot of the whole project — artifacts plus links — captured at a point in time. The Baselines page lists them as a timeline and compares any two, or a baseline and the working copy.',
    tips: [
      'Select a baseline in the timeline to compare from it; choose what to compare to beside the counts.',
      'Select a change to read its text word by word: struck-out words were removed, underlined words added.',
      'Text only and Links narrow the list to what you are reviewing.',
      'Capture and delete baselines from the top bar of the Requirements view; baseline views are read-only.',
      'The V&V dashboard and the Matrix have their own baseline selectors for historical views.',
    ],
    chapter: { slug: 'project-pages', title: 'Home, Design, Tests and Baselines' },
    also: [{ slug: 'artifacts', title: 'Requirements & artifacts' }],
  },
  home: {
    title: 'Home',
    summary:
      "The project's home: its figures, what needs you, the audit trail, your workspace's projects and the agent proposals waiting for review.",
    tips: [
      'Needs you lists the artifacts in review, suspect links, test runs in progress, failing test cases and new comments.',
      'Traced is the share of requirements with a test case; verified the share verified by their tests.',
      'Full history opens the Activity page with every event of the project.',
      'Review proposals opens the agent runs, where proposals are approved or rejected.',
    ],
    chapter: { slug: 'project-pages', title: 'Home, Design, Tests and Baselines' },
  },
  design: {
    title: 'Design',
    summary:
      "The project's design items as a tree, with the selected item's description, attributes, the requirements it satisfies and the test cases that verify them.",
    tips: [
      'Design items nest under one another as they do in the Requirements view; type in Filter to find one.',
      'Verified through lists the test cases verifying the requirements the item satisfies, with their last result.',
      'Edit opens the item in the Requirements view; Show in trace opens its impact.',
      'Add attributes to a design item in its editor; they appear here as a table.',
    ],
    chapter: { slug: 'project-pages', title: 'Home, Design, Tests and Baselines' },
    also: [{ slug: 'links', title: 'Traceability links' }],
  },
  tests: {
    title: 'Tests',
    summary:
      "The newest test runs with their pass, fail and blocked counts, and the project's test cases with their method, what they verify and their last result.",
    tips: [
      'Each run card opens the run, where results are recorded.',
      'Last result: fail, blocked narrows the table to what needs attention; Method to automated, manual or physical cases.',
      "Steps counts the numbered steps in a test case's text.",
      'Start a new test run on the V&V page.',
    ],
    chapter: { slug: 'project-pages', title: 'Home, Design, Tests and Baselines' },
    also: [{ slug: 'vv', title: 'V&V & test runs' }],
  },
  guided: {
    title: 'Guided Definition',
    summary:
      'An eight-step wizard from a blank product to a committed, traceable requirement set — with the V&V Assistant suggesting entries you can add with one click.',
    tips: [
      'Moving Next materializes new entries as draft artifacts (green dot); edit them later in the Requirements view.',
      "Click an assistant suggestion card's add button to insert it into the current step; replacements update an entry in place.",
      'Abandon session keeps drafts already created — only unsaved entries are discarded.',
      "If the assistant doesn't answer, no runner is online; your messages are saved and answered once one connects.",
      'After committing, take the offered "Initial requirements" baseline as your starting snapshot.',
    ],
    chapter: { slug: 'guided-wizard', title: 'Guided definition & the V&V Assistant' },
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
      'Start with Top gaps — an empty list means full coverage; click a section to see its requirements.',
      'Pin a test run to a baseline to record a campaign against a fixed snapshot.',
      'Completed or aborted test runs become read-only; the latest result per test case feeds the rollup and the Matrix chips.',
      'Export report (PDF) downloads the V&V report for the live project or any baseline.',
    ],
    chapter: { slug: 'vv', title: 'V&V & test runs' },
  },
  releases: {
    title: 'Releases',
    summary:
      "Plan requirements for target releases such as MVP and V1.0, and follow each release's review, verification and burn-up.",
    tips: [
      'Releases run in order: a requirement introduced in MVP is in V1.0 and every later release too, unless you give it a last release.',
      "Set a requirement's releases in its editor; set them on a heading and everything under it inherits them.",
      'Ready means approved and verified: the review status and the V&V rollup both count.',
      'The burn-up draws verification day by day from the test result history.',
      "A release a requirement still targets can't be deleted; move its requirements first.",
    ],
    chapter: { slug: 'releases', title: 'Target releases' },
    also: [{ slug: 'vv', title: 'V&V & test runs' }],
  },
  evidence: {
    title: 'Evidence',
    summary:
      'The record behind physical and manual tests: each evidence bundle is one capture session — what was done, what was observed, when, by whom, under which conditions, and the files it produced.',
    tips: [
      'Record a capture once and cite it from every test result it supports, rather than attaching the same files to each run.',
      'Each bundle gets a reference like EVD-1 that is never reused, so a report that cites it keeps meaning the same thing.',
      'Each file’s SHA-256 is recorded at upload, so the dataset can be checked against the record later.',
      'Enter conditions one per line as name: value — rig, serial numbers, calibration date, ambient temperature.',
    ],
    chapter: { slug: 'vv', title: 'V&V & test runs' },
  },
  impact: {
    title: 'Impact analysis',
    summary:
      'What a change to one artifact touches, before you make it: what depends on it downstream, and what it depends on upstream.',
    tips: [
      'Open it from Show impact → on an artifact or a row of the Traceability Matrix, or pick the artifact here.',
      'Downstream is what a change could affect; Upstream is the sources and rationale the artifact rests on.',
      'Each result shows the link it was reached through and how many hops away it is.',
      'Run it before editing an approved requirement: the downstream list is what may need review again.',
    ],
    chapter: { slug: 'links', title: 'Traceability links' },
  },
  review: {
    title: 'Review Queue',
    summary:
      'Where artifacts wait for sign-off. Approve or send back one row at a time or a whole selection, and send the whole project for review in one action.',
    tips: [
      'Send project for review moves every draft into review at once; run it again each cycle to pick up only what changed.',
      'Approving clears the suspect flag on every link that touches the artifact.',
      'Send back needs a reason, which is posted as a note on the artifact before it returns to draft.',
      'Tick rows (or the header box) to approve or send back several at once.',
    ],
    chapter: { slug: 'artifacts', title: 'Requirements & artifacts' },
  },
  todos: {
    title: 'To-dos',
    summary:
      "The board's work items arranged by person instead of by column, so the page answers who owes what.",
    tips: [
      'Moving a card on the board updates this page, and the other way round — it is the same work.',
      'Just mine narrows the page to your own list; Show done brings finished work back.',
      'Raise a to-do from a note with + Add to-do: the person named and the note’s first line are filled in.',
      'Work for an agent or a team is under Agents and teams; anything with no assignee is under Unassigned.',
    ],
    chapter: { slug: 'board', title: 'Kanban board' },
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
      'Your personal runner claims the runs you launched, never a teammate\'s. Ownerless runs (automations, board triggers) go to workspace and hosted runners, and to the personal runner of each member with a role in the run\'s project or of a workspace admin; one with no project goes only to an admin\'s runner or the shared runners.',
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
      "Run now launches immediately and jumps you to the Runs page focused on the new run; a whole-workspace automation's run, which no project's Runs page lists, opens beside the automations.",
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
  // The workspace-level pages outside a project, keyed by their path
  // (helpTopicForPath tries the first two segments, then the first).
  'org/settings': {
    title: 'Workspace settings',
    summary:
      'Settings for the whole workspace: its name and plan, members and their roles, teams, limits and usage, AI providers and runners.',
    tips: [
      'Workspace membership gets someone into the workspace; project access is granted per project, in its Settings → Access.',
      'Members are told when they are added, removed or change role, and every other admin hears about arrivals and departures.',
      'Teams group people for project access; they are not crews, which are graphs of AI agents.',
      'AI Providers (admin only) sets the default model and the API keys runs use; Runners is where workspace runners are paired.',
    ],
    chapter: { slug: 'org-settings', title: 'Workspace settings & teams' },
    also: [{ slug: 'runs-runners', title: 'Runs & runners' }],
  },
  'org/runs': {
    title: 'Workspace runs',
    summary:
      "The agent runs that belong to no project: a whole-workspace automation's runs, products invented from the projects list, and runs of deleted projects.",
    tips: [
      "Click a run to follow it, cancel it or retry it, with the same detail panel as a project's Runs page.",
      'The address names the open run (?run=), so a reload or a shared link opens it again.',
      'Admins see every such run; other members see the ones they launched.',
      'A notification that one of these runs failed opens it here.',
    ],
    chapter: { slug: 'runs-runners', title: 'Runs & runners' },
  },
  admin: {
    title: 'Platform admin',
    summary:
      "The deployment's own administration: who the platform admins are, password reset links for support, and every workspace with its plan.",
    tips: [
      'The first account registered on a deployment is its platform admin; Make admin and Remove manage the rest.',
      'Reset link shows a one-time password reset link once: hand it only to someone you have identified.',
      "A workspace's plan sets its limits and its default release channel.",
      'The Open source plan publishes every project in the workspace as of its latest baseline.',
    ],
    chapter: { slug: 'platform-admin', title: 'Platform admin' },
  },
  'whats-new': {
    title: "What's new",
    summary:
      'Every release of OpenV, newest first: what you can now do, what quietly improved and what stopped being broken.',
    tips: [
      'A new feature reaches nightly workspaces in the release it ships in, and stable-channel workspaces with their next stable release.',
      'Press ? or F1 on any page to open the help for that page.',
    ],
    chapter: { slug: 'getting-started', title: 'Getting started' },
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
  if (segments[0] !== 'projects') {
    const page = HELP_TOPICS[segments.slice(0, 2).join('/')] || HELP_TOPICS[segments[0]];
    if (page) return page;
  }
  const moduleSegment = segments[0] === 'projects' ? segments[2] : segments[0];
  return (moduleSegment && HELP_TOPICS[moduleSegment]) || DEFAULT_TOPIC;
};

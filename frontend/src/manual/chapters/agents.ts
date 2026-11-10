// User manual chapter: Agent definitions.
const content = `
# Agents

Agents are AI workers that operate inside your projects — drafting
requirements, reviewing for testability, running interviews, doing V&V chores.
The **Agents** page in the project menu lists them and hosts the editor.

## The agent hierarchy

The Agents page shows the workspace's agents as a hierarchy of cards: who
leads, which group each specialist is in, and who reports to whom. It is a
picture of how the work is divided; no agent starts another, and each run is
a run of one agent.

- **The top card** is the lead: an agent in no group that reports to no one.
  Below the top cards, a line totals everything on the page.
- **One column per group**, each with a coloured marker and a summary line
  that totals the group's numbers. A group's colour comes from its name, so
  it is the same for everyone and on every visit.
- Inside a column, the group's **lead** comes first and the agents that
  report to it are **indented beneath it**, joined by a guide line in the
  group's colour; nesting can go deeper. An agent with no group of its own
  sits in the group of the agent it reports to.

Each card has a coloured top border and a chip for its group, and shows its
**live numbers over the last 7 days**: **open** runs (queued, running or
waiting for approval), **done** runs, their **cost**, and the proposals of its
runs waiting **to approve**. The numbers refresh while the page is open.

- **Click a card** to open the Runs page filtered to that agent: its runs, and
  the proposals waiting from them. The chip at the top of the Runs page clears
  the filter.
- **Mission and tools** opens the agent's description, provider and model,
  write mode and allowed tools on the card.
- **▶ Launch** starts a run; **Edit agent** opens the editor.

### Arranging the hierarchy

A workspace admin clicks **Edit** (top right) to arrange the cards. Each card
then shows:

- **Reports to** — the agent it sits under, or *No one* to put it at the top.
  An agent cannot report to itself or to an agent below it.
- **Group** — type a group's name, pick an existing one, or clear it so the
  agent takes the group of the agent it reports to.
- **↑ ↓** — move it among the agents beside it.

Each change is saved at once, to the agent's definition file
(\`reports_to\`, \`group\` and \`sort_order\` in its frontmatter). Click
**Done** to leave Edit mode.

## The default agents

Every workspace starts with a small, flat team, already arranged:

- **V&V Assistant** on top: the general agent you chat with beside the
  wizard and on every project page. It answers questions, reviews, and
  proposes any change to the project as a card you apply.
- **Requirements Auditor**: checks requirements and user needs against the
  project's quality rules, and proposes rewrites.
- **Test Auditor**: checks verification coverage and test cases, and drafts
  missing test cases with their *verifies* links. **Draft test cases** runs
  it.
- **Link Auditor**: checks traceability (suspect, missing and mistyped links)
  and proposes fixes.
- **Requirements Interviewer**: holds elicitation interviews with the people
  you invite.

The three auditors write only proposals: nothing they change lands until
someone approves it. Run one from its card with **▶ Launch run**, and say
what to audit.

A workspace that had the earlier team (Chief of Staff, Requirements Analyst,
V&V Engineer, Developer, Reviewer and Test Case Author) moved to this one
with the release: those agents were **archived**, not deleted, so their runs
and proposals are kept. An agent your workspace had edited or locked, or that
an automation runs, was left as it was; archive it yourself if you no longer
need it.

## Archived agents

An archived agent is out of the team: it leaves the hierarchy and cannot be
run, and its runs and proposals stay. They are listed under **Archived**
beneath the hierarchy; **Reinstate** puts one back where it was. To archive
an agent, open it with **Edit** and tick **Archive this agent**. Archiving
is the way to retire an agent you may want back: deleting one also deletes
its runs and the automations that use it.

On a phone the groups stack in one column; tap a group's header to fold it.

Crews, the graphs of agents the Crew page used to build, have been removed;
the hierarchy is how agents are arranged now.

## The agent list

Where the page shows a list instead of the hierarchy (a workspace on the
stable channel until its stable release has the hierarchy), each agent card
shows:

- Its **provider** badge (which AI vendor/CLI runs it).
- Its **write mode** badge — *proposal* (orange) or *direct* (red).
- 📁 if the agent has **repository access**.
- **▶ Launch run** and **Delete** buttons.

**Sync from disk** re-reads agent definition files from the server's data
directory — agents are stored as plain markdown files with YAML frontmatter,
so they can also be edited outside the app and synced in.

## Launching a run

Click **▶ Launch run**, enter a prompt, and launch. Keep prompts lean — the
agent fetches requirement content itself through its OpenV tools, so name the
work rather than pasting requirement text. You're taken to the **Runs** page
to watch progress.

## The agent editor

**New agent** or clicking an existing agent opens the editor. Fields:

| Field | Meaning |
| --- | --- |
| Slug | Stable identifier (can't change after creation) |
| Name / Description | Display info |
| Provider | claude-code, codex-cli, gemini-cli, anthropic-api, openai-api, google-api |
| Model | Dropdown of the provider's known models, or *provider default*, or **Custom…** to type any model id the vendor accepts |
| Effort | Reasoning effort: low, medium, high, xhigh, max — or provider default. Applies to claude-code; codex-cli caps it at high; gemini-cli ignores it |
| Write mode | *proposal* (changes need approval) or *direct* (writes immediately) |
| Repository access | Allow runs that clone a connected repository |
| Max turns | Cap on agentic turns per run |
| Timeout (seconds) | Wall-clock limit per run |
| Allowed tools | Comma-separated tool allowlist — **required**, see below |
| System prompt | The agent's instructions (markdown) |

Existing agents also have a **Raw markdown** tab showing the underlying
definition file (frontmatter + system prompt) for direct editing.

## Every agent needs an allowlist

**Allowed tools** cannot be left empty. An empty list is not "no tools" — a
vendor CLI started without an allowlist runs with *every* tool it has, so
saving an agent without one is refused, and a run of an agent that somehow has
none fails before the CLI starts. \`mcp__openv__*\` grants the OpenV tools;
name the vendor's own tools (Read, Edit, WebSearch…) alongside it only where
the agent genuinely needs them. Granting a tool you never mention in the system
prompt mostly wastes it — say what it is for.

Agents that read text written outside your workspace — the interviewer, any
agent with repository access, any agent holding WebSearch or WebFetch — run
with **nothing auto-approved beyond that allowlist**: a file edit or a shell
command they were not granted is refused rather than waved through. That is the
protection against a stray instruction hidden in a web page, a repository file,
or an interview answer.

**What the interviewer may do.** The seeded **Requirements Interviewer** talks
to people on public invite links, so it holds the narrowest allowlist of all:
the read-only OpenV tools, plus \`record_candidate_need\`, and nothing else. It
cannot create or edit artifacts, create links, delegate to another agent, read
files, or run commands. The worst a participant can talk it into is recording a
candidate need — which is a suggestion you review like any other.

## Write mode: proposal vs direct

- **proposal** (default, recommended) — every write the agent makes (create or
  update an artifact, create a link, record a test result) is captured as a
  **proposal**. You review and approve or reject it on the Runs page before it
  is applied. Nothing merges unreviewed.
- **direct** — writes apply immediately. Reserve for low-risk, well-trusted
  agents.

Agent-drafted artifacts can also carry a *draft* status so generated content
never silently masquerades as reviewed work.

## Repository access

Agents with repository access can clone repositories connected in
**Project Settings → Repositories**. Such runs only execute on personal or
workspace runners — never on the hosted runner (see *Runs & runners*).

## Default model & providers

Workspace admins set each provider's **default model** and see CLI detection
status under **Workspace settings → AI Providers**. An agent whose Model field
is *provider default* inherits that setting.
`;

export default content;

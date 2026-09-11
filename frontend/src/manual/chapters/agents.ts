// User manual chapter: Agent definitions.
const content = `
# Agents

Agents are AI workers that operate inside your projects — drafting
requirements, reviewing for testability, running interviews, doing V&V chores.
The **Agents** page in the project sidebar lists them and hosts the editor.

## The agent list

Each agent card shows:

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

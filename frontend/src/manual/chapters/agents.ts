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
| Allowed tools | Comma-separated tool allowlist |
| System prompt | The agent's instructions (markdown) |

Existing agents also have a **Raw markdown** tab showing the underlying
definition file (frontmatter + system prompt) for direct editing.

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

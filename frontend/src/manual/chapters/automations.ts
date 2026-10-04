// User manual chapter: Automations.
const content = `
# Automations

Automations launch agent (or crew) runs without anyone clicking a button. Open
**Automations** in the project sidebar.

## What an automation covers

An automation covers either **this project**, the one whose Automations page
it was made on, or the **whole workspace**:

- A project's automation fires on that project's events, and its runs work
  in that project.
- A whole-workspace automation fires on matching events in every project of
  the workspace, each run working in the project of the event that set it
  off, and on the workspace's own membership and invitation events, whose
  runs belong to no project. A scheduled or manual one runs with no project.

Only a **workspace admin** makes, edits, runs or deletes a whole-workspace
automation: the form's **Covers** choice (*This project* or *Whole
workspace*) is theirs alone, and they can move an existing automation between
its project and the whole workspace. Every project's Automations page lists
the whole workspace's automations beside its own, marked *Whole workspace*;
other members see them there but cannot change them.

A whole-workspace automation runs an agent, or one of the workspace's own
crews such as its default crew; a crew made in one project runs only that
project's automations.

## Kinds of automation

| Kind | When it runs |
| --- | --- |
| manual | Only when you click **Run now** |
| scheduled | On a cron schedule, read in UTC (presets: Hourly, Daily at 9am, Every 15 minutes — or any cron expression) |
| triggered | When a matching event happens in the project (or, for a whole-workspace automation, anywhere in the workspace) |

## Trigger events

Triggered automations listen to the workspace's events. Available events:

- artifact.created / artifact.updated / artifact.deleted /
  artifact.status_changed / artifact.restored
- link.created / link.updated / link.deleted
- baseline.captured / baseline.deleted
- project.review_round_started
- chatter.created
- testrun.recorded
- workitem.created / workitem.moved / workitem.updated
- agentrun.finished / agentrun.successors_skipped
- proposal.created
- project.member_added / project.member_role_changed / project.member_removed
- org.member_added / org.member_role_changed / org.member_removed /
  org.invitation_sent / org.invitation_accepted

The org.* events belong to the workspace, not to a project, so they fire only
automations that cover the whole workspace (see *What an automation covers*);
an automation that covers one project never sees them.

**Filters** narrow the match with key/value pairs on the event payload (for
example only artifacts of a certain type). Two safety valves keep triggered
automations from running away:

- **Cooldown (seconds)** — minimum time between runs.
- **Max runs per hour** — hard cap.

## Creating an automation

**New automation** opens the form:

1. Name it.
2. A workspace admin chooses what it **covers**: this project or the whole
   workspace.
3. Choose the **target** — a single agent or a crew.
4. Pick the kind and its schedule/event settings.
5. Write the **prompt template** — the prompt each run starts with. Keep it
   lean; the agent fetches artifact content itself at run time.

Automations are created **enabled**; toggle them on/off from the table at any
time. The table also shows each automation's last run and (for scheduled ones)
the next run time. If a scheduled automation's cron expression can no longer
be read, it is not run: it is switched off, so correct its schedule and switch
it back on.

## Running and reviewing

- **Run now** launches immediately and jumps you to the Runs page focused on
  the new run. A whole-workspace automation's run belongs to no project, so
  no project's Runs page lists it: it opens beside the automations instead.
- Scheduled and event-triggered runs are **ownerless**: the workspace's
  hosted and workspace runners can claim them, and so can the personal runner
  of any member with a role in the automation's project, or of a workspace
  admin; a run with no project goes only to an admin's runner or the shared
  runners (see *Runs & runners*). A **Run now** launch counts as yours, so —
  like any manual launch — it's briefly reserved for your personal runner
  first. If the target agent uses *proposal* write mode, its output still
  waits for your approval on the Runs page.

## Example uses

- Nightly gap-analysis review of requirement coverage (scheduled, daily).
- Testability review whenever a requirement is created (triggered on
  artifact.created with a type filter).
- Summarize an agent run's results into the board card when it finishes
  (triggered on agentrun.finished).
`;

export default content;

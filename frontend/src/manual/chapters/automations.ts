// User manual chapter: Automations.
const content = `
# Automations

Automations launch agent (or crew) runs without anyone clicking a button. Open
**Automations** in the project sidebar.

## Kinds of automation

| Kind | When it runs |
| --- | --- |
| manual | Only when you click **Run now** |
| scheduled | On a cron schedule (presets: Hourly, Daily at 9am, Every 15 minutes — or any cron expression) |
| triggered | When a matching event happens in the project |

## Trigger events

Triggered automations listen to the project event bus. Available events:

- artifact.created / artifact.updated / artifact.deleted
- link.created / link.deleted
- baseline.captured
- testrun.recorded
- workitem.created / workitem.moved
- agentrun.finished

**Filters** narrow the match with key/value pairs on the event payload (for
example only artifacts of a certain type). Two safety valves keep triggered
automations from running away:

- **Cooldown (seconds)** — minimum time between runs.
- **Max runs per hour** — hard cap.

## Creating an automation

**New automation** opens the form:

1. Name it.
2. Choose the **target** — a single agent or a crew.
3. Pick the kind and its schedule/event settings.
4. Write the **prompt template** — the prompt each run starts with. Keep it
   lean; the agent fetches artifact content itself at run time.

Automations are created **enabled**; toggle them on/off from the table at any
time. The table also shows each automation's last run and (for scheduled ones)
the next run time.

## Running and reviewing

- **Run now** launches immediately and jumps you to the Runs page focused on
  the new run.
- Scheduled and event-triggered runs are **ownerless**: any live runner in
  the workspace can claim them, including the hosted runner. A **Run now**
  launch counts as yours, so — like any manual launch — it's briefly reserved
  for your personal runner first. If the target agent uses *proposal* write
  mode, its output still waits for your approval on the Runs page.

## Example uses

- Nightly gap-analysis review of requirement coverage (scheduled, daily).
- Testability review whenever a requirement is created (triggered on
  artifact.created with a type filter).
- Summarize an agent run's results into the board card when it finishes
  (triggered on agentrun.finished).
`;

export default content;

// User manual chapter: Kanban board & work items.
const content = `
# Kanban board

The **Board** is a kanban view of the project's work items. Its special trick:
cards can be assigned to AI agents or crews, and moving such a card kicks off
an agent run.

## Columns

Backlog · To Do · In Progress · Review · Done. Each column shows its card
count and has a **+ Add card** composer at the bottom.

## Creating a card

The composer takes:

- **Title** (required) and an optional description.
- **Assignee** — Unassigned, *Me*, any **🤖 agent**, or any **👥 crew**.
- **Artifact IDs** — optional comma-separated artifact IDs to attach the
  relevant requirements to the card.

## Driving agents from the board

> Tip shown on the board itself: *assign a card to an agent and drag it to
> To Do to launch the agent.*

- Moving a card assigned to an agent or crew into an agent-owned column
  enqueues an **agent run** for that card.
- A **pulsing blue dot** on a card means a run is live (queued, claimed, or
  running). While a run is live the card is **locked** — it can't be dragged
  until the run finishes.
- The agent's progress and comments land back on the card's **activity feed**.

## The card drawer

Click a card to open its drawer:

- Edit the **title** and **description**.
- See **linked artifacts** (from the artifact IDs) with links into the
  Requirements view.
- Follow the **activity feed** — comments, moves, status changes, and agent
  run events — and add your own comments.
- Delete the card.

## Moving cards

Drag any unlocked card between columns or within a column. Human-assigned and
unassigned cards behave like a normal kanban; only agent/crew cards with live
runs are locked.

Work items can also be **created by crews**: when a crew edge hands work off
to (or requests review from) a *person*, a board card assigned to that person
appears automatically — see *Crews*.
`;

export default content;

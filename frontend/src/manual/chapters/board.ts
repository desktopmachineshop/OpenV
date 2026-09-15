// User manual chapter: Kanban board & work items.
const content = `
# Kanban board

The **Board** is a kanban view of the project's work items. Its special trick:
cards can be assigned to AI agents or crews, and dragging an agent's card to
**To Do** kicks off an agent run.

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

- When **you** move a card assigned to an **agent** into **To Do**, an
  **agent run** is enqueued for that card (skipped if a live run is already
  attached). Only human moves trigger this — the automatic card moves that
  track run progress don't. Crew-assigned cards don't launch from the board;
  launch crews from the Crew page or an automation.
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

## To-dos: the same work, by person

**Plan → To-dos** lists the project's work items under the person each one
belongs to, instead of under the column it sits in. It is a different
arrangement of the board, not a second list: move a card on the board and
the To-dos page says so, and vice versa.

- Everyone with access to the project gets a section, including people who
  owe nothing — that is what makes it answer *who owes what*.
- Each row shows the item's **status**, its due date if it has one, and
  whether it came from a note. Anything overdue is called out.
- **Just mine** narrows the page to your own list. **Show done** brings
  finished work back into view; by default the page shows only what is
  still open.
- Click a row to open the same **card drawer** the board uses.
- Work assigned to an agent or a team is gathered under *Agents and teams*,
  and anything with no assignee under *Unassigned*, so nothing is hidden by
  having nobody's name on it.

### Raising a to-do from a note

Mentioning someone with **@name** in a note on an artifact tells them about
it — it does not create work for them. To turn the ask into something
tracked, use **+ Add to-do** on the note:

- Whoever the note names is filled in as the assignee, and the note's first
  line as the title. Both can be changed, and you can set a due date.
- The full note becomes the to-do's description, and the artifact the note
  is on is linked to it, so the card arrives with its context.
- The to-do lands in **To Do** and appears on the board and on the To-dos
  page straight away.

Afterwards the note carries a small link to the to-do showing its **current
status** — so a thread from three weeks ago tells you whether the thing
being discussed ever got done, without going to look. The status is read
from the card each time, not copied, so moving the card updates the note.

A note can raise one to-do. System and agent entries in the feed do not
offer the control: those are a record of what happened, not somebody asking
for something.
`;

export default content;

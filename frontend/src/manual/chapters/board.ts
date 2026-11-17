// User manual chapter: Kanban board & work items.
const content = `
# Kanban board

The **Board** is a kanban view of the project's work items. Its special trick:
cards can be assigned to AI agents, and dragging an agent's card to
**To Do** kicks off an agent run.

Every card is also a task of the project's plan: the **Gantt** page draws
the cards on a timeline, with start dates, sub-tasks and dependencies. See
[The plan (Gantt)](/manual/gantt).

A card can carry **To-dos**: open the card, type one under **To-dos** and
press **Add**, and tick it off when it is done. They are what fills the
task's bar on the Gantt, and the **To-dos** page lists them with the
project's others.

## Columns

Backlog · To Do · In Progress · Review · Done. Each column shows its card
count and has a **+ Add card** composer at the bottom.

## Creating a card

The composer takes:

- **Title** (required) and an optional description.
- **Assignee** — Unassigned, *Me*, or any **agent**.
- **Artifact IDs** — optional comma-separated artifact IDs to attach the
  relevant requirements to the card.

## Driving agents from the board

> Tip shown on the board itself: *assign a card to an agent and drag it to
> To Do to launch the agent.*

- When **you** move a card assigned to an **agent** into **To Do**, an
  **agent run** is enqueued for that card (skipped if a live run is already
  attached). Only human moves trigger this — the automatic card moves that
  track run progress don't.
- A **pulsing blue dot** on a card means a run is live (queued, claimed, or
  running). While a run is live the card is **locked** — it can't be dragged
  until the run finishes.
- The agent's progress and comments land back on the card's **activity feed**.
- A chat is not a task. Talking to the assistant, or typing a prompt to an
  agent with **▶ Launch run**, puts no card on the board: the run is on the
  **Runs** page. A card is something you assigned; an automation or a
  schedule that starts an agent on a project still gets one, so its work is
  tracked.

## The card drawer

On a computer, click a card to open it beside the board: the board gives
it room and stays in view, so you can go from card to card. The open mark
at a card's corner, or in the open card's header, opens the card in full,
wide in the middle of the window. On a phone a card opens over the board.
Either way the card lets you:

- Edit the **title** and **description**.
- See **linked artifacts** (from the artifact IDs) with links into the
  Requirements view.
- Follow its **History** — comments, moves, status changes, and agent
  run events — and add your own comments. It is the same History an
  artifact has: **Comments**, **Changes** and **All** choose what it
  lists, and with the card opened in full it is a card of its own.
- Delete the card.

## Moving cards

Drag any unlocked card between columns or within a column. Human-assigned and
unassigned cards behave like a normal kanban; only agent cards with live
runs are locked.

A card that was assigned to a crew, before crews were removed, is now
assigned to the crew's first (entry) agent, or is unassigned if the crew had
none.

## To-dos: small jobs, kept off the board

A **to-do** is one paragraph of text: small enough that you just do it and
**tick it off**, like *check the wording of REQ-12* or *ask Sam about the
seal supplier*. It has no column, no discussion thread and no agent runs —
those are what a board card is for. The board shows only cards, so a list of
small asks no longer crowds out the real pieces of work.

A to-do has its text, a tick box (and when it was ticked), who has it, an
optional due date, and, when it was raised from a note, a link back to the
note's artifact.

### The project's to-do list

**Plan → To-dos** is the project's list:

- **Quick add** at the top: type the to-do and press **Enter**. Choose who
  has it and a due date beside the field if you want to; both are optional.
- Tick the box to complete a to-do. Completed to-dos collapse into a
  **Completed** line under the list; open it to see them, or untick one to
  open it again.
- **Edit** changes the text, who has it and the due date. Anything overdue
  is called out.
- **Just mine** narrows the list to the to-dos assigned to you.
- A to-do is one paragraph, at most 1000 characters. Anything longer is a
  piece of work: put it on the board.

### My to-dos

**My to-dos**, the **To-dos** tab under **Plan** in the workspace's rail (and
linked from every project's To-dos page), gathers your to-dos across the workspace: every
to-do assigned to you in a project you can open, each labelled with its
project, and your **personal** to-dos. A personal to-do belongs to no
project and nobody else sees it; the quick add on My to-dos makes one.

### Promote to board task

When a to-do turns out to be bigger than it looked, **Promote to board
task** on its row makes a card of it: the card lands in **To Do** carrying
the to-do's text, who has it, its due date and the artifact it came from.
The to-do is ticked off with the note *promoted to a board task*, which
links to the card.

### Raising a to-do from a note

Mentioning someone with **@name** in a note on an artifact tells them about
it — it does not create work for them. To turn the ask into something
tracked, use **+ Add to-do** on the note:

- Whoever the note names is filled in as the person who has it, and the
  note's first paragraph as the to-do. Both can be changed, and you can set
  a due date.
- The to-do goes on the project's to-do list, and on the My to-dos of the
  person who has it, straight away. It does not go on the board.

Afterwards the note carries a small link to the to-do showing whether it is
**open** or **done**, so a thread from three weeks ago tells you whether the
thing being discussed ever got done, without going to look. The status is
read from the to-do each time, not copied.

A note raised before to-dos had a list of their own keeps its link to the
board card it raised, and the card stays on the board.

A note can raise one to-do. System and agent entries in the feed do not
offer the control: those are a record of what happened, not somebody asking
for something.

### For agents

Agents reach to-dos through the openv tools \`list_todos\`, \`create_todo\`
and \`tick_todo\`, so a run can leave a small follow-up for a person, or
tick one off once it is done.
`;

export default content;

// User manual chapter: the project plan, a Gantt over the board.
const content = `
# The plan (Gantt)

The **Gantt** page, under **Plan** in the project menu, draws the project's
plan on a timeline: its tasks as bars, the relationships between them as
arrows, and its milestones as diamonds. Nothing on it is typed in as a percentage. A
task's progress comes from the to-dos ticked off on its board card and from
its sub-tasks, and a milestone's from criteria measured against the project's
artifacts, such as *every user need addressed*.

The tasks are the cards of the **Board**. A card added on the board is a
task here, and a task added here is a card there: one title, one assignee,
one due date. The plan adds what a schedule needs and a board does not: a
start date, sub-tasks, and the relationships between tasks.

## Reading the chart

The outline on the left lists the tasks, numbered as an outline (1, 1.1,
1.2, 2). A task with sub-tasks has an arrow that folds them away. The
figure at the end of each row is the task's progress.

On the timeline, each task is a bar from its start to its due date:

- The bar **fills** from the left as the task progresses.
- A **green** bar is a task in **Done**. A **red** bar is late: it is past
  its due date and not done.
- A task with sub-tasks is drawn as a thinner, darker bar that spans all of
  them.
- An **arrow** joins two related tasks, from the task that drives to the
  task it drives. Where it leaves and arrives shows the type of the
  relationship: see [Relationships between tasks](#relationships-between-tasks).
  A dashed red arrow is a relationship the dates break; the outline marks
  that task with a warning sign.
- A task with no dates yet shows **+ Schedule** at today.

The row of **Milestones** above the tasks holds a diamond for each
milestone, with its name and progress. The thin band leading up to a
diamond, from the milestone before it, fills as that milestone's criteria
are met. The red vertical line is today.

**Days**, **Weeks**, **Months** and **Quarters** change the scale. The page
opens at the scale that shows the whole plan. The figures above the chart
count the plan: how far it is complete, its tasks, those done, late and
not yet scheduled, and the milestones met.

## Scheduling tasks

**Add task** makes a card in the board's To Do column and places it on the
plan, with a start and a due date if you give them.

To move a task, drag its bar. To change one date, drag that end of the
bar. Both move in whole days. To place a task that has no dates, choose
**+ Schedule** in its row: it gets a week from today, which you then drag
to where it belongs.

Select a task to open it beside the chart. There you can:

- set or clear its **Start** and **Due** dates. The due date is the card's
  own, so the board shows the same day;
- make it a **sub-task of** another task, or a top-level task again;
- set its **relationships** to the tasks that drive it;
- add **sub-tasks**.

**Sub-task of** and the task chooser under **Relationships** are searchable:
open one and type part of a task's name or its outline number to narrow a
long plan to the task you want.

A task cannot start after it is due, or be a sub-task of one of its own
sub-tasks.

## Relationships between tasks

A relationship says how one task drives another. Each has one of the three
types project plans use:

| Type | Meaning | The arrow |
| --- | --- | --- |
| **Finish to start** (FS) | The task starts after the other finishes | From the other's end to the task's start |
| **Start to start** (SS) | The task starts no earlier than the other starts | Joins the two starts |
| **Finish to finish** (FF) | The task finishes no earlier than the other finishes | Joins the two ends |

Under **Relationships** in a task's panel, choose a type and then the task
that drives this one. A task can have several relationships, one to each
task that drives it, so two tasks can together decide when a third starts:
*Build the prototype* can wait for *Wheel guard design* and for
*Electronics* to finish. Change a relationship's type in its row, or remove
it with the cross.

OpenV does not move a task when its dates break a relationship. It marks
the relationship **Broken**, draws its arrow dashed and red, and leaves the
dates to you. A relationship to a task that is done is never broken. A
relationship cannot be added to a task that this one already drives, since
the two would wait on each other.

## Progress from to-dos and sub-tasks

A task's progress is worked out, so it never goes stale:

| The task | Its progress |
| --- | --- |
| Is in **Done** on the board | 100% |
| Has to-dos or sub-tasks | The share of its to-dos ticked off, with each sub-task counting by its own progress |
| Has neither | 0% until it is moved to Done |

To-dos are not shown on the Gantt. Add and tick off a task's to-dos on its
card: open the card on the **Board** and use **To-dos**. They are ordinary
project to-dos, so the **To-dos** page lists them with the others, and
ticking one off in either place moves the task's bar. A task with two of its four to-dos ticked is 50%; a task with
one finished sub-task and one at 50% is 75%.

## Milestones and their criteria

A milestone marks a point the project must reach. Each has **criteria**: a
measure of the project's artifacts and the percentage it must reach.

| Criterion | What it counts |
| --- | --- |
| User needs addressed | User needs that at least one requirement derives from |
| Requirements approved | Requirements whose review status is approved |
| Requirements covered by design | Requirements that a design item satisfies |
| Requirements with a test case | Requirements that a test case verifies |
| Requirements verified | Requirements whose verification passes |
| Test cases passing | Test cases whose latest result is a pass |
| Hazards mitigated | Hazards that a design item mitigates |
| Plan tasks done | Tasks due by the milestone's date that are in Done |

**Add milestone** starts from a preset, such as **Definition complete**
(user needs addressed: 100%), which you can change: give the milestone a
name, a date, and any criteria with their targets. An early milestone
might ask only that every user need is addressed; a later one that 80% of
requirements are verified.

A milestone's progress is how far its criteria have come toward their
targets, averaged. It is **met** when every criterion has reached its
target, and **late** when its date has passed and it is not met. Select a
milestone to see each criterion's figure and the count behind it, and to
edit or delete it. A milestone with no criteria shows no progress.

## Release milestones

Every release on the **Releases** page is a milestone on the plan, drawn as
a filled diamond at the release's target date. Its name, date and status
are the release's: change them on the Releases page, which **Open the
release** leads to.

A release milestone starts with two criteria: every requirement in the
release approved, and every one verified, which is what the Releases page
calls ready. **Edit criteria** sets its own, and **Reset to defaults** puts
those two back. Its requirement criteria count only the requirements in
that release. A release marked **Released** is met.

## Exporting the plan

**Export** saves the plan as a file:

- **PDF**: the chart on landscape pages, with the outline, dates, bars,
  milestones and today's line, then the milestones with their criteria.
- **Excel**: the tasks with a timeline of weeks drawn beside them, and the
  milestones on a second sheet.
- **CSV**: the tasks, then the milestones and their criteria.

In Excel and CSV, the **relationships** column names each driving task by
its outline number and type, as in *1.1FS, 2SS*.
- **JSON**: the whole plan as the app reads it.

Agents read the plan with the \`get_plan\` tool and place a task, with its
relationships, with \`schedule_work_item\`.

## On a phone

A timeline needs more width than a phone has, so the phone shows the same
plan as a schedule. Four tiles give the figures. Under **Schedule**, the
tasks are grouped by the milestone they are due before, each with its
dates and a progress bar; **Milestones** lists the milestones with their
progress. Tap a task or a milestone to open it and make the same edits as
on a computer, by setting dates rather than dragging.

On a tablet the chart is shown. With a touch screen, bars do not drag: set
a task's dates in its panel.
`;

export default content;

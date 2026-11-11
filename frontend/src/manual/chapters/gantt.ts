// User manual chapter: the project plan, a Gantt over the board.
const content = `
# The plan (Gantt)

The **Gantt** page, under **Plan** in the project menu, draws the project's
plan on a timeline: its tasks as bars, the relationships between them as
arrows, and its milestones as diamonds. Nothing on it is typed in as a percentage. A
task's progress comes from the to-dos ticked off on it and from its
sub-tasks, and a milestone's from criteria measured against the project's
artifacts, such as *every user need addressed*.

The tasks are the cards of the **Board**. A card added on the board is a
task here, and a task added here is a card there: one title, one assignee,
one due date. The plan adds what a schedule needs and a board does not: a
start date, sub-tasks, and the relationships between tasks.

## Reading the chart

The outline on the left lists the tasks, numbered as an outline (1, 1.1,
1.2, 2). A task with sub-tasks has an arrow that folds them away. The
figure at the end of each row is the task's progress, and the small
**open** button after it opens the task beside the chart. Each milestone
carries the same button.

On the timeline, each task is a bar from its start to its due date:

- The bar **fills** from the left as the task progresses.
- A **green** bar is a task in **Done**. A **red** bar is late: it is past
  its due date and not done.
- A task with sub-tasks is drawn as a thinner, darker bar that spans all of
  them: see [Tasks with sub-tasks](#tasks-with-sub-tasks).
- An **arrow** joins two related tasks, from the task that drives to the
  task it drives. Where it leaves and arrives shows the type of the
  relationship: see [Relationships between tasks](#relationships-between-tasks).
  A dashed red arrow is a relationship the dates break; the outline marks
  that task with a warning sign.
- With **Critical path** on, a thin line after a bar, ended by a tick, is
  the task's float: how far its end can slip before it pushes the next
  task.
- A task with no dates yet has an empty row that you click, or drag across,
  to schedule it.
- Days the plan does not work are shaded at the **Days** scale: see
  [Working days](#working-days).

The row of **Milestones** above the tasks holds a diamond for each
milestone, with its name and progress. The thin band leading up to a
diamond, from the milestone before it, fills as that milestone's criteria
are met. The red vertical line is today.

**Days**, **Weeks**, **Months** and **Quarters** change the scale. The page
opens at the scale that shows the whole plan, on the whole plan. A plan too
long to fit opens with today a third of the way in from the left, so that
most of what you see is ahead of you.

On a computer the chart reaches the bottom of the window, and so does the
panel of a task or a milestone opened beside it; each scrolls on its own
when it is longer than the window.

The timeline scrolls sideways past both ends of the plan, further forward
than back: use the scroll bar, a trackpad or a wheel, or drag an empty part
of the timeline. The scroll bar under the chart belongs to the timeline, so
it starts where the task column ends. **Today** brings today back into
view.

The figures above the chart count the plan: how far it is complete, its
tasks, those done, late, on the critical path and not yet scheduled, the
effort in it, and the milestones met.

## Scheduling tasks

**Add task** makes a card in the board's To Do column and places it on the
plan, with a start and a due date if you give them. Or add one in the
chart itself: type a title in the last row of the outline, **Add a
task…**, and press Enter. The task appears as a new row with no dates,
ready to be scheduled on the timeline.

To move a task, drag its bar. To change one date, drag that end of the
bar. Both move in whole days.

To place a task that has no dates, use its empty row on the timeline:

- **Click** a date, and the task is scheduled for the period of the scale
  that holds it: that day at **Days**, that week at **Weeks**, that month at
  **Months**, that quarter at **Quarters**. The row shows the period as you
  point at it.
- **Drag** across the row, and the task is scheduled for the range you drag
  over, drawn as it grows with its dates beside it.

Open a task, with the button on its row or by selecting its bar, to see it
beside the chart. There you can:

- set its **Start** and **Due** dates, its **Duration** and its **Effort**:
  see [Duration and effort](#duration-and-effort). The due date is the
  card's own, so the board shows the same day;
- make it a **sub-task of** another task, or a top-level task again;
- set its **relationships** to other tasks, whichever of the two drives;
- add **to-dos** and tick them off;
- add **sub-tasks**.

**Sub-task of** and the task chooser under **Relationships** are searchable:
open one and type part of a task's name or its outline number to narrow a
long plan to the task you want.

A task cannot start after it is due, or be a sub-task of one of its own
sub-tasks.

## Tasks with sub-tasks

A task with sub-tasks is a folder for them, and has no dates of its own. It
starts when its first sub-task starts and finishes when its last one
finishes; its duration is the working days between the two, and its effort
is its sub-tasks' effort added up. Its panel shows these, with no fields to
set, and its bar does not drag: schedule the sub-tasks to move it. Dates a
task had before it was given sub-tasks are no longer used.

Fold a task with the arrow in the outline and its bar stands for everything
under it:

- A relationship between one of its sub-tasks and a task outside it is still
  drawn. The arrow leaves or lands on the folded bar at the point along it
  where the sub-task's own start or finish falls, marked with a small dot,
  so an arrow from a sub-task that finishes halfway through leaves the bar
  halfway along.
- A relationship between two of its own sub-tasks is hidden with them.
- If the critical path runs through its sub-tasks, the folded task carries
  the amber critical marks.

## Working days

**Working days**, above the chart, has a button for each day of the week.
The days switched on are the days the plan works: Monday to Friday until
you change it. Switch on Saturday for a six-day week, or switch a day off
that your team never works. A plan works at least one day.

Durations and float count working days only. With Monday to Friday, a task
that starts on a Thursday and lasts 3 days is due the following Monday, and
a task from a Monday to the Friday of the next week has a duration of 10.
A start that falls on a day off counts from the next working day. The
chart shades the days off at the **Days** scale. Changing the working days
does not move any task's dates: durations and float are counted again.

## Duration and effort

A task has a **duration**: how many [working days](#working-days) it runs,
counting its first and its last. A task from Monday 1 March to Friday
5 March has a duration of 5 days. The task
keeps its duration, so its dates move together:

| You change | What follows |
| --- | --- |
| **Start** | The due date moves, the same number of working days later or earlier |
| **Due** | The start moves with it |
| **Duration** | The due date moves; the start stays |
| The bar, by dragging it | Both dates move |
| One end of the bar, by dragging it | The duration changes |

A task with no dates yet can still be given a duration: it takes effect when
the task gets a start or a due date. If a card's due date is changed on the
board, the task's duration becomes what its two dates now span.

**Effort** is the work in the task, in days of one person's time, and may be
a part of a day. It is separate from the duration: three people working for
two days is a duration of 2 and an effort of 6. The figures above the chart
add up the effort of every task.

## The critical path

The **critical path** is the chain of tasks that decides when the plan ends:
each has no room to slip, so a day lost on any of them is a day lost at the
end. OpenV works out, for every task with dates, its **float**: how many
working days it could slip before the plan's last task would end later. A task can
slip until the plan's last day, or until it would push a task it drives:

- a **finish to start** relationship lets it slip until the day before the
  other task starts;
- a **start to start** relationship lets its start slip until the other
  task's start;
- a **finish to finish** relationship lets its end slip until the other
  task's end.

The path starts at the tasks with no float and runs back from each task on
it to the task that drives it most tightly. It is shown whole even where
the dates leave room between two of its tasks: a task on the path, and not
yet done, is **critical** whether or not it has float. With **Critical
path** on, which it is when the page opens, critical tasks have an amber
outline and an amber dot in the outline, and the arrows between them are
amber and heavier.

Float is drawn on the chart as well. After the bar of a task that drives
another, a thin line runs to the last day its end can slip to before it
pushes that task, and ends in a tick, so the room between one task and the
next is the length of the line. The line is amber on the critical path and
grey elsewhere; a task with no room has none. A task's panel says **Critical** with any float it
has, or how many days of float it has. A task with sub-tasks is never critical itself: its panel
says **Critical sub-tasks** when the path runs through them, and it carries
the amber marks on the chart while it is folded. The float is worked out from the dates as they stand: OpenV
does not move tasks for you. A task with no relationships has float up to
the plan's last day, so only relationships make a path longer than one task.

## Relationships between tasks

A relationship says how one task drives another. Each has one of the three
types project plans use:

| Type | Meaning | The arrow |
| --- | --- | --- |
| **Finish to start** (FS) | The task starts after the other finishes | From the other's end to the task's start |
| **Start to start** (SS) | The task starts no earlier than the other starts | Joins the two starts |
| **Finish to finish** (FF) | The task finishes no earlier than the other finishes | Joins the two ends |

A relationship belongs to both of its tasks, and each one's panel lists it
under **Relationships**, named from that task's side: by this task's end,
then the other task's end. Open *Write the requirements* and its
relationship to *Design* reads **Finish to start: that starts after this
finishes**; open *Design* and the same relationship reads **Start to
finish: this starts after that finishes**. You can add, change or remove
it from either one:

| From this task | Type | Which drives |
| --- | --- | --- |
| Finish to start: that starts after this finishes | FS | This task |
| Start to start: that starts after this starts | SS | This task |
| Finish to finish: that finishes after this finishes | FF | This task |
| Start to finish: this starts after that finishes | FS | The other task |
| Start to start: this starts after that starts | SS | The other task |
| Finish to finish: this finishes after that finishes | FF | The other task |

To add one, choose how this task relates to the other, then the other task.
A task can have several relationships, so two tasks can together decide
when a third starts: *Build the prototype* can wait for *Wheel guard
design* and for *Electronics* to finish. Two tasks have one relationship
between them: setting another replaces it. Change a relationship in its
row, or remove it with the cross.

### Drawing a relationship on the chart

Point at a bar, or select its task, and a small **+** appears at each end.
Press one and drag to another bar: a line follows the pointer, and a label
says what releasing would make. Which ends you join decides the type:

| From | To | Makes |
| --- | --- | --- |
| The **+** at a bar's end | The start of another bar | Finish to start: the other task starts after this one finishes |
| The **+** at a bar's start | The start of another bar | Start to start |
| The **+** at a bar's end | The end of another bar | Finish to finish |
| The **+** at a bar's start | The end of another bar | Finish to start, the other way round: this task starts after the other finishes |

Release over the left half of a bar for its start, the right half for its
end. Release anywhere else, or press Escape, to abandon the line.

OpenV does not move a task when its dates break a relationship. It marks
the relationship **Broken**, draws its arrow dashed and red, and leaves the
dates to you. A relationship to a task that is done is never broken. A
relationship that would make a loop, where two tasks end up waiting on each
other, is refused.

## Progress from to-dos and sub-tasks

A task's progress is worked out, so it never goes stale:

| The task | Its progress |
| --- | --- |
| Is in **Done** on the board | 100% |
| Has to-dos or sub-tasks | The share of its to-dos ticked off, with each sub-task counting by its own progress |
| Has neither | 0% until it is moved to Done |

A to-do is a small job, a day or two of work at most, so it has one date
only: an optional **deadline**. It has no start, duration or bar, and is
never drawn on the chart. Add a task's to-dos in its panel, under
**To-dos**, with a deadline if you like, and tick them off there; a
deadline that has passed is shown in red. The same list is on the task's
card on the **Board**. They are ordinary project to-dos, so the **To-dos**
page lists them with the others, and ticking one off anywhere moves the
task's bar. A task with two of its four to-dos ticked is 50%; a task with
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
its outline number and type, as in *1.1FS, 2SS*, and each task has its
duration and float in working days, its effort and whether it is critical. The PDF and
the Excel timeline mark critical tasks in amber.
- **JSON**: the whole plan as the app reads it.

Agents read the plan with the \`get_plan\` tool, place a task, with its
relationships, with \`schedule_work_item\`, and set the working days with
\`set_plan_working_days\`.

## On a phone

A timeline needs more width than a phone has, so the phone shows the same
plan as a schedule. Four tiles give the figures. Under **Schedule**, the
tasks are grouped by the milestone they are due before, each with its
dates, duration and a progress bar, and **Critical** on a task with no
float; **Milestones** lists the milestones with their
progress. Tap a task or a milestone to open it and make the same edits as
on a computer, by setting dates rather than dragging.

On a tablet the chart is shown. With a touch screen, bars do not drag and
links are not drawn: set a task's dates and relationships in its panel.
`;

export default content;

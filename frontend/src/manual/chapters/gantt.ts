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

On a computer the chart's header holds what changes how you look at the
plan: the **View** picker, **Critical path**, **Today** and the zoom. What
changes the plan itself is one step away in **Plan tools**: **Calendar**,
**Resources**, **Tracking**, **Recalculate** and, while the critical path
is marked, **List only the critical tasks**. Where this chapter says a
button is above the chart, look in **Plan tools**. The button turns red
and counts the people asked for more than they have.

## Reading the chart

The outline on the left lists the tasks, numbered as an outline (1, 1.1,
1.2, 2). A task with sub-tasks has an arrow that folds them away. The
figure at the end of each row is the task's progress. Select a task's
title, or its bar, to show it in the panel beside the chart. The small
**open** button at the end of the row opens the task in full, in a pane in
the middle of the window: its description, linked artifacts, to-dos and
activity on the left, and everything the plan holds on it on the right. A
milestone's open button shows the milestone beside the chart.

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
- With **Critical path** on, a thin dashed line after a bar, ended by a tick, is
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

**Days**, **Weeks**, **Months** and **Quarters** change the scale (**Weeks** numbers each week, as in *9 (W46)*). The page
opens at the scale that shows the whole plan, on the whole plan. A plan too
long to fit opens with today a third of the way in from the left, so that
most of what you see is ahead of you.

On a computer the chart reaches the bottom of the window, and so does the
panel of a task or a milestone opened beside it; each scrolls on its own
when it is longer than the window.

The timeline scrolls sideways past both ends of the plan, further forward
than back: use the scroll bar, a trackpad or a wheel, or drag an empty part
of the timeline. The scroll bar under the chart belongs to the timeline, so
it starts where the task column ends. **Today** scrolls the timeline so that today
sits a third of the way in from the left.

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

Select a task, by its title in the outline or by its bar, to see it beside
the chart. **Open the full task** in that panel, like the open button on
the row, shows everything else about it. In the panel you can:

- set its **Start** and **Due** dates, its **Duration** and its **Effort**:
  see [Duration and effort](#duration-and-effort). The due date is the
  card's own, so the board shows the same day;
- make it a **sub-task of** another task, or a top-level task again;
- assign it to a **milestone**: see
  [Assigning tasks to a milestone](#assigning-tasks-to-a-milestone);
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
task had before it was given sub-tasks are no longer used. Its float is the least of its sub-tasks': it has no room when one of them has none. It cannot be linked to another task, since it is one of its sub-tasks that the other task waits for: link that sub-task. A relationship it was given before stays until you remove it.

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

## Listing, ordering and editing tasks

The bar under the chart's header changes how the tasks are listed. It changes nothing in the plan itself.

- **Find a task** lists the tasks whose name contains what you type.
- The **filter** lists only tasks that are not done, late, critical, behind the status date, not scheduled or over-allocated. In the outline, a task that matches keeps the tasks it sits under.
- **Sort** lists the tasks by start, finish, name, float or progress instead of the outline, and **Group** gathers them under who does them, their milestone or their board column. A sorted or grouped list is flat: it shows the tasks that have no sub-tasks.
- **Columns** adds columns beside the task names: start, finish, days, effort, % done, float, cost and who. Click a start, finish, days, effort or % done cell to change it in place; press Enter to save or Escape to give up. A task with sub-tasks takes these from them, so its cells are not edited.

**Moving a task.** Sub-tasks and top-level tasks are listed by start date until you move one; from then on its siblings keep the order you give them. Select a task and use the arrows: **↑** and **↓** move it among its siblings, **⇥** makes it a sub-task of the task above, and **⇤** moves it out a level, after its parent. You can also drag a task by its outline number onto another task to put it straight after that one. Moving needs the outline, so the arrows are off while the list is sorted or grouped.

**Undo and redo.** **Undo** (Ctrl+Z) takes back your last edit to a task: its dates, duration, effort, progress, parent, order and the other fields of its panel. **Redo** (Ctrl+Y) makes it again. Up to 50 edits are kept until you leave the page. Relationships drawn on the chart, milestones and the plan's settings are not undone this way, and on an automatic plan the tasks an edit moved are placed again from where they now stand. 
## Working days

The days of the week a plan works are the workspace's, the same for every project in it: Monday to Friday until a workspace admin changes them, under **Workspace settings** → **Plan calendar**. Switch on Saturday there for a six-day week, or switch a day off that your team never works. A plan works at least one day.

**Time off**, on the same page, lists the days each member is away. A member records their own and an admin anyone's. A task stretches over the days its people are away: it keeps its days of work and ends later, so a five-day task whose only person is away for two of them ends two working days later. With several people on a task, a day counts for the share of them who are there, by their percentage on the task: with two people at equal shares, a day one is away is half a day. Each person's work and cost count the days they are there. Adding or removing time off moves the tasks that are not done, and reassigning a task fits it to its new people.

**Calendar**, above the chart, opens the plan's own calendar. Add each day the plan does not work whatever its weekday, a public holiday or a shutdown, with a name if you like; remove one with its cross. A holiday is skipped exactly as a weekend day is, and shaded on the chart.

Durations and float count working days only. With Monday to Friday, a task that starts on a Thursday and lasts 3 days is due the following Monday, and a task from a Monday to the Friday of the next week has a duration of 10. A start that falls on a day off counts from the next working day. The chart shades the days off at the **Days** scale. Changing the working days does not move any task's dates: durations and float are counted again. 
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

Tick **Elapsed time** for a task whose duration is time that simply
passes, such as paint curing or a delivery: its duration then counts
calendar days, weekends and holidays included.

A **Deadline** is the day a task should be finished by. It moves nothing:
it is a marker, a small triangle above the task's row on the chart, green
while the task is on course and red once the task ends after it or the day
has passed with the task not done, when the panel also says **Deadline
missed**.

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
another, a thin dashed line runs to the last day its end can slip to before it
pushes that task, and ends in a tick, so the room between one task and the
next is the length of the line. The line is amber on the critical path and
grey elsewhere; a task with no room has none. A task's panel says **Critical** with any float it
has, or how many days of float it has.

**Only critical**, beside **Critical path**, lists just the tasks on the
path, with the tasks they sit under. In the plan's calendar (**Holidays**)
you can raise the threshold: *a task is critical when its float is at most
N working days* also marks the tasks that are nearly critical. Every task
with room shows its float line, to the task it would push or, when nothing
waits for it, to the end of the plan. A task with sub-tasks is never critical itself: its panel
says **Critical sub-tasks** when the path runs through them, and it carries
the amber marks on the chart while it is folded. The float is worked out from the dates as they stand: OpenV
does not move tasks for you. A task with no relationships has float up to
the plan's last day, so only relationships make a path longer than one task.

## Relationships between tasks

A relationship says how one task drives another. Each has one of the four
types project plans use:

| Type | Meaning | The arrow |
| --- | --- | --- |
| **Finish to start** (FS) | The task starts after the other finishes | From the other's end to the task's start |
| **Start to start** (SS) | The task starts no earlier than the other starts | Joins the two starts |
| **Finish to finish** (FF) | The task finishes no earlier than the other finishes | Joins the two ends |
| **Start to finish** (SF) | The task finishes no earlier than the other starts | From the other's start to the task's end |

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
| Start to finish: that finishes after this starts | SF | This task |
| Start to finish: this starts after that finishes | FS | The other task |
| Start to start: this starts after that starts | SS | The other task |
| Finish to finish: this finishes after that finishes | FF | The other task |
| Finish to start: this finishes after that starts | SF | The other task |

To add one, choose how this task relates to the other, then the other task.
A task can have several relationships, so two tasks can together decide
when a third starts: *Build the prototype* can wait for *Wheel guard
design* and for *Electronics* to finish. Two tasks have one relationship
between them: setting another replaces it. Change a relationship in its
row, or remove it with the cross.

### Offsets

A relationship can carry an **offset**: time the driven task keeps after
what the type asks for. Its unit is **working days**, **elapsed days**
(calendar days, for a wait that does not stop at weekends) or a
**percentage of the driving task's duration** (50% of a ten-day task is
five working days). *Start to start* with an
offset of 5 means the second task starts five working days after the first
one starts; *finish to start* with an offset of 2 leaves two working days
between the first task's end and the second's start. A negative offset
lets the driven task begin that many days earlier.

Set it in the **Offset** field under the relationship, on either task, and
press Enter or leave the field; a new relationship takes one in the add
form. A relationship whose task does not keep its offset is **Broken**, and
float and the critical path count the offset as time the task cannot use.

### Recalculating the plan

In a manual plan, OpenV never moves a task on its own. **Recalculate**,
above the chart, moves them when you ask: every task that another task
drives, or that a constraint holds to a date, goes to the dates those ask
for. Tick **Only move tasks later** to treat relationships as minimums: a
task that already starts on or after the day asked is left where it is,
and nothing is pulled earlier.

| Relationship | The driven task |
| --- | --- |
| Finish to start | Starts the working day after the other ends, plus the offset |
| Start to start | Starts the day the other starts, plus the offset |
| Finish to finish | Ends the day the other ends, plus the offset |
| Start to finish | Ends the day the other starts, plus the offset |

Each task keeps its duration. A task driven by several others takes the
latest date any of them asks for. A task that nothing drives stays where
it is, and so does a task that is done. A driven task with sub-tasks moves
them, by as many working days as it has to move. The page asks before it
recalculates, and then says how many tasks moved; recalculating a plan that
already agrees with its relationships moves nothing.

### Drawing a relationship on the chart

Point at a bar, or select its task, and a small **+** appears at each end.
Press one and drag to another bar: a line follows the pointer, and a label
says what releasing would make. Which ends you join decides the type:

| From | To | Makes |
| --- | --- | --- |
| The **+** at a bar's end | The start of another bar | Finish to start: the other task starts after this one finishes |
| The **+** at a bar's start | The start of another bar | Start to start |
| The **+** at a bar's end | The end of another bar | Finish to finish |
| The **+** at a bar's start | The end of another bar | Start to finish: the other task finishes after this one starts |

Release over the left half of a bar for its start, the right half for its
end. Release anywhere else, or press Escape, to abandon the line.

OpenV does not move a task when its dates break a relationship. It marks
the relationship **Broken**, draws its arrow dashed and red, and leaves the
dates to you. A relationship to a task that is done is never broken. A
relationship that would make a loop, where two tasks end up waiting on each
other, is refused.

## Automatic scheduling and constraints

**Calendar**, above the chart, also says how the plan is scheduled.

| Setting | What it does |
| --- | --- |
| **Tasks are scheduled** manually | Tasks stay where they are put until you recalculate. This is how every plan starts |
| **Tasks are scheduled** automatically | After every change, each task is placed from its relationships and its constraint |
| **Plan starts** | An automatic task starts no earlier than this day |
| **Must finish by** | Float is counted to this day, and goes negative for work that runs past it |
| **Schedule from** the finish | Automatic tasks go as late as their float allows, not as early |

A task can go its own way: under **Scheduling** in its panel, set its
**Mode** to manual in an automatic plan, or automatic in a manual one.

An automatic task goes straight after the tasks that drive it. A task that
nothing drives stays where you put it: type a start, or drag its bar, and
it is held to **start no earlier than** that day, so the next change does
not put it back.

A **constraint** holds a task to a date:

| Constraint | The task |
| --- | --- |
| As soon as possible | Has no constraint |
| As late as possible | Goes as late as its float allows |
| Start no earlier than, finish no earlier than | Cannot be placed before the date |
| Start no later than, finish no later than | Loses float as it nears the date, and is flagged past it |
| Must start on, must finish on | Is placed on the date, whatever drives it |

A manual task is not moved by its constraint until you recalculate; until
then its panel says when its dates do not keep to it.

The same section shows what the plan works out for the task: the
**earliest** it can run (after the tasks that drive it, the plan's start
and its constraint), the **latest** it can run before the plan ends later,
and its **float**. Float is **negative**, shown as *N working days behind*,
when a task runs past its deadline, a "no later than" constraint or the
plan's finish date, or when a relationship it drives is broken; such a task
is critical.

Tick **A critical path for each chain of tasks** in the calendar to give
every independent chain its own path, to its own last task, where the
default is one path to the end of the plan.

## Resources, cost and levelling

A task's panel has a **Resources** section: the people and agents who do
the task. **Assign someone** adds one, and the percentage beside each name
is the share of their working time the task takes: 100% is full time, 50%
half. With nobody assigned there, the card's assignee on the board does the
task at 100%.

Tick **Effort decides the duration** and the task's length follows from its
effort and its people: six days of effort for two people at full time takes
three days, and for one person at 50% twelve. Add or remove someone and the
due date moves.

**Resources**, above the chart, lists everyone with work on the plan:

| Column | Meaning |
| --- | --- |
| Day rate | What a day of their time costs. Set it here |
| Capacity % | The share of a working day they can give this project: 100 unless you change it |
| Work (days) | Their share of each task's effort, or of its duration when it has no effort |
| Cost | That work at their day rate |
| Peak | The most asked of them on any day |

Someone asked for more than their capacity on a day is **over-allocated**.
The dialog shows when and by how much, the button turns red with the count,
and each task that loads them during those days says **Over-allocated** in
its panel.

**Level the plan** clears it. Where someone is over-allocated, the task
with the most float is delayed until after the first of the others ends,
and the tasks it drives follow. Levelling only ever delays; it does not
move a task earlier or one that is done, and it cannot help someone who is
over-allocated by a single task.

A task's **cost** is its people's work at their day rates plus its **Fixed
cost**, for materials or a supplier. A task with sub-tasks costs what they
cost. Set the plan's **Currency** and **Budget** in the Resources dialog:
the figures above the chart then show the plan's cost against its budget,
in red when it is over.

### Planned resources

A **planned resource** is a placeholder for someone you will choose later: a welder, a second developer. Under **Planned resources** in the Resources dialog, give it a name, a day rate and a capacity and choose **Add planned resource**. Assign tasks to it as you would to a person: it is in every task's **Assign someone** list, marked *Planned*, and the plan's cost, load, levelling and effort-driven durations count it like anyone else. So a plan can be sized and costed before anyone is named. When you know who it is, choose them under **Staff with…**: its tasks go to that person or agent at the same share (added to theirs on a task they are already on), and the placeholder goes. **Remove** drops it and takes it off its tasks.

### Plan defaults

**Defaults**, in **Plan tools**, sets what a task does until it says otherwise. **Scheduling mode** is manual or automatic (see [Automatic scheduling and constraints](#automatic-scheduling-and-constraints)); a task's own panel can override it. **Effort decides the duration of new tasks** makes a task that has not been scheduled yet effort-driven from the start, so its effort and the resources on it set its length; a task already scheduled keeps what it was given.

## Tracking progress against a baseline

**Tracking**, above the chart, is where you compare the plan with what was agreed and with what has happened.

**Baselines.** **Save a baseline** records every task's dates, duration, effort and cost as they stand. Save one when the plan is agreed. The chart then draws a thin grey bar under each task where the baseline had it, and the task's panel says how many working days later or earlier it now starts and finishes. A plan keeps up to 11 baselines. Pick the one to measure against in the list; the newest is used until you do. **Show on the chart** hides the grey bars.

**What happened.** A task's panel has a **Tracking** section. **Actual start** and **Actual finish** record the days work really began and ended. **% complete** replaces the progress counted from to-dos and sub-tasks with a figure you type; clear it to go back to the counted one. **Actual cost** records what the task has cost so far; left empty, the task is taken to have cost its earned share of the planned cost.

**The status date.** Progress is reported as of today unless you set a **Status date**. A dashed line marks it on the chart. A task that has done less than the status date asks is marked *Behind* in its panel.

**Earned value.** With a baseline and costs on the plan, Tracking shows the baseline's budget, what was planned and earned by the status date, what was spent, the schedule and cost variances, the two indices (below 1 is behind, or over cost) and the forecast cost at the present rate.

**Reschedule remaining work** moves every task that should have started by the status date and has not to that date, keeping its duration, and moves the tasks those drive after them. Tasks with progress or an actual start stay where they are. 
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
| Milestone tasks done | Tasks assigned to the milestone that are in Done |

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

## Assigning tasks to a milestone

A milestone can own tasks: the work that has to be finished to reach it.
Assign them from either side:

- In a **task's** panel, choose its **Milestone**.
- In a **milestone's** panel, under **Tasks**, choose **Assign a task**.
  The list there shows each of the milestone's tasks with its progress,
  and the cross takes a task off the milestone.

A task belongs to one milestone at a time, which may be a release's.
Assigning a task that has sub-tasks brings them with it: each sub-task
counts toward the milestone unless it is given a milestone of its own. The
milestone counts the tasks that have no sub-tasks, so a task and its
sub-tasks are not counted twice.

A milestone with tasks measures **Milestone tasks done**, the share of
those tasks in Done, as one of its criteria, with a target of 100%. You do
not have to add it: it is there as soon as the milestone has a task, so
finishing the tasks is part of the milestone's percentage alongside its
other criteria. To ask for less than all of them, add the criterion
yourself and set its target. A milestone whose only criterion is its tasks
is met when they are all done.

**Plan tasks done** is a different measure: it counts every task of the
plan due by the milestone's date, assigned or not.

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
  the lines between related tasks, milestones and today's line, then the
  milestones with their criteria. Each page ends with a legend. A line
  to a task on another page runs to the edge of the page and names that
  task's number and page, as in *→ 2.1 p.2*.
- **Excel**: the tasks with a timeline of weeks drawn beside them, and the
  milestones on a second sheet.
- **CSV**: the tasks, then the milestones and their criteria.
- **Microsoft Project (XML)**: a file Microsoft Project opens, with the tasks, their outline, dates, durations, progress, constraints, deadlines and relationships, the milestones, and the people with their assignments.

In Excel and CSV, the **relationships** column names each driving task by
its outline number and type, with any offset after it, as in *1.1FS,
2SS+5*, and each task has its
duration and float in working days, its effort and whether it is critical. The PDF and
the Excel timeline mark critical tasks in amber.
- **JSON**: the whole plan as the app reads it.

Agents read the plan with the \`get_plan\` tool, place a task, with its
relationships, with \`schedule_work_item\`, which also assigns it to a milestone and to people, set day rates with
\`set_plan_resource\`, level the plan with \`level_plan\`, recalculate it with
\`recalculate_plan\`, and set the working days with
\`set_plan_working_days\`.

## Importing from Microsoft Project

**Import**, beside Export, reads a Microsoft Project XML file (in Project: *Save as*, *XML format*) and adds its tasks to this project. Each task becomes a card on the board, in To Do, In Progress or Done by its percentage complete, with its dates, its place in the outline, its date constraint and its deadline. A task of no length marked as a milestone becomes a milestone, and the relationships between the tasks follow with their offsets. Resources and calendars are not read. Importing adds to the plan; it replaces nothing, so importing a file twice adds its tasks twice.

## Other views of the plan

The **View** picker at the start of the chart's header shows the same plan in other ways.

- **Network** draws a box for each task and an arrow for each relationship, each task one column to the right of the tasks that drive it. It has no time axis, so the time scale is not offered there.
- **Calendar** lists, on each day, the tasks that run on it and the milestones due, with the days the plan does not work shaded. The time scale chooses how much it shows: **Days** a week, with every task of each day; **Weeks** four weeks, each Monday with its week number, as in *9 (W46)*; **Months** a month; **Quarters** the three months of a quarter. The arrows step by that much, and **Today** returns to the present.
- **Resource usage** lists each person and agent with work on the plan and the days of their time each column asks for. The time scale makes a column a day, a week, a month or a quarter. A column that asks for more than they can give is red.
- **Timeline** draws the top-level tasks and the milestones on one band, under the same two rows of dates as the Gantt. The time scale sets how wide a day is, and the band scrolls sideways when the plan is longer than the page, so a long plan stays readable. **Today** scrolls it to the present. The critical path is its top row.

**Critical path** in the header works in every view. While it is on, critical tasks have an amber border in the Network, the Calendar (which lists them first in a day), the Timeline and All projects, and Resource usage underlines in amber the weeks in which someone has critical work. While it is off, no view marks them.

Select a task in any of them to open its panel.

## All projects, and links between them

**All projects** shows the plan of every project of the workspace that you can read, each on a line with its top-level tasks, its milestones and its progress. Each project's critical path is the top row of its line. The lines share one time scale and scroll sideways together, while the projects' names stay in place. A project's name opens its Gantt.

A task can wait on a task of another project of the same workspace. In the task's panel, under **Other projects**, choose **Wait on a task of another project**, then the project, the task, the relationship and an offset. The task then follows the other one when the plan is recalculated, or at once on an automatic plan. The other project's task is never moved from here, and it is not a row of this plan: it is named on the task it is linked to, in both projects, and under **Links between projects** in All projects. A link whose dates are broken is marked.

## On a phone

A timeline needs more width than a phone has, so the phone shows the same
plan as a schedule. Four tiles give the figures. Under **Schedule**, the
tasks are grouped by the milestone they are assigned to, or else the one
they are due before, each with its
dates, duration and a progress bar, and **Critical** on a task with no
float; **Milestones** lists the milestones with their
progress. Tap a task or a milestone to open it and make the same edits as
on a computer, by setting dates rather than dragging.

On a tablet the chart is shown. With a touch screen, bars do not drag and
links are not drawn: set a task's dates and relationships in its panel.
`;

export default content;

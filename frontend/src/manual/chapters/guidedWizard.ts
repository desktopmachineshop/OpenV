// User manual chapter: Guided requirements wizard + the V&V Assistant chat.
const content = `
# Guided definition & the V&V Assistant

**Guided Definition** walks you from a blank product to a committed, traceable
requirement set in eight steps — with the V&V Assistant alongside, suggesting
entries you can add with one click.

## Starting, resuming, modifying

Open **Guided Definition** from the project menu (the Overview page also
has a shortcut whose label reflects where you are: *Start*, *Resume*, or
*Modify guided definition*).

- **Start guided definition** — begins a fresh session.
- If a session is **in progress**, it resumes exactly where you left off.
- If a definition was already **committed**, you can **Modify guided
  definition**: the wizard reopens seeded with the committed answers. Entries
  that already became artifacts are shown locked (green dot); anything you add
  becomes new drafts to review and commit. **Start over from scratch** is also
  offered.
- **Abandon session** (top right) drops the current in-progress session and
  returns to the start page. Draft artifacts already created are **kept** —
  only unsaved entries are discarded.

## The eight steps

1. **Product framing** — vision, problem statement, target users. Saved to the
   product profile and shown on the Overview page.
2. **Personas** — the key people who will use the product (name, role, goals,
   pain points). Each becomes a draft *persona* artifact.
3. **User needs** — per persona, "As *persona*, I need *capability* so that
   *outcome*". Each becomes a draft *user-need* linked to its persona.
4. **Requirements** — turn each need into testable "The system shall …"
   statements with a **fit criterion** and a **verification method**
   (inspection, analysis, demonstration, or test). Each becomes a draft
   *requirement* linked *derives-from* its need.
5. **NFRs & constraints** — quality-attribute requirements grouped by category
   (Performance, Reliability, Usability, Security, Maintainability,
   Regulatory). Each category you use becomes its own section under
   *Non-Functional Requirements & Constraints*, so the category is the
   requirement's place in the document rather than a prefix on its title.
6. **Hazards** (optional) — what could go wrong, the potential harm, and a
   severity. Skip if not applicable.
7. **Verification stubs** (optional) — every requirement with method **test**
   is listed; checked ones get a draft test case ("Verify: …") linked with
   *verifies*, filed under *Functional* or the quality attribute of the
   requirement it verifies.
8. **Review & commit** — all draft artifacts created during the flow, grouped
   by type. **Discard** any you don't want, then **Commit** to make them all
   live. A reopened definition that adds nothing new has no drafts to review;
   **Commit definition** still closes it.

Moving **Next** from a step saves it and materializes any new entries as
**draft artifacts** — you'll see a green dot appear next to each one, and its
fields lock (it's now a real artifact; edit it later in Requirements). You can
jump back to any step you've reached from the step header.

After committing you're offered **Create baseline** — a snapshot named
"Initial requirements" — plus shortcuts to the Requirements view or straight
back into *Modify guided definition*.

## The V&V Assistant chat

The panel on the right is the V&V Assistant, which sees your current step and
everything you've entered so far.

- Ask it anything — brainstorming personas, tightening a requirement,
  challenging your framing.
- Its replies can include **suggestion cards** (persona, need, requirement,
  NFR, hazard, or framing text). Click a card's add button to insert it into
  the wizard — replacement suggestions update an existing entry in place.
  Entries already materialized as artifacts can't be replaced from chat.
- **Quick actions** (like *Review step*) send canned prompts, e.g. a
  quality-check of the current step's entries for clarity and testability.
- The assistant is nudged automatically when you advance or skip a step, so it
  follows along with context.

### The V&V Assistant on every page

The same assistant is on every page of the app, behind
the **✦ Assistant** button in the bottom corner. The small arrow at its side tucks it into a tab on the edge of the window, which brings it back, and your browser remembers which. It opens a chat panel over the
page:

- **One conversation per project.** What you asked in the wizard is there on
  every page and the other way round, and the panel keeps the conversation
  open while you move between pages. On the workspace Home, choose the
  project at the top of the panel.
- **Looking at:** under the title says what the assistant is told you are
  reading: the requirement open in the Requirements page (it reads the whole
  artifact), or the page you are on. **✕** stops sending it, until you open
  something else.
- **⤢** expands the panel, **⇆** moves it to the other corner when it covers
  something you need, and dragging its top corner resizes it. **—** or
  **Esc** minimises it back to the button, and the conversation is still
  there when you open it again.
- **Alt+A** (Option+A on a Mac) opens the panel and puts the cursor in the
  message box.
- On a phone it opens as a sheet from the bottom of the screen.
- The quick actions fit where you are: *Review this* and *How would I verify
  it?* beside a requirement, *Gap analysis* and *Draft tests* for the project
  as a whole.

Suggestion cards beside the project add a draft artifact, or make the change
they describe, when you press their button, and only then. Ask for any change
you could make yourself and the assistant answers with a card for it:

- **Release**: add a target release, or change one's name, date or status.
- **Release target**: put artifacts in a release, optionally until a later
  one, or clear their own targeting so they follow their parent's.
- **Link**: add a traceability link, remove one, or confirm a suspect link.
- **Review status**: move artifacts to draft, in review, approved or
  superseded. A change the review workflow does not allow is refused on the
  card, with the reason.
- **Archive**: archive artifacts, which keeps their history so they can be
  reinstated. **Apply all** never includes an archive card; each one is
  applied by its own button.
- **Comment**: post a comment on an artifact, in your name.
- **To-do** and **Board task**: raise a to-do or add a task to the board,
  assigned to a member and with a due date if the card names them.
- **Baseline**, **Review round** and **Test run**: capture a baseline, send
  every draft for review, or record a test run's results.
- **Board task** can also place the task on the plan: a start date, a
  duration, the task it is a sub-task of, its milestone and the tasks it
  waits on. Ask the assistant to plan a piece of work and it proposes the
  tasks in order, each waiting on the one before.
- **Plan task**: change a task that exists, named by its title or outline
  number: its dates, duration, effort, percentage complete, deadline, parent
  task, milestone, the tasks it waits on, its title or its board column.
- **Milestone**: add a milestone with a date and completion criteria, or
  change one. A release's milestone takes its name and date from the
  release.
- **Task to-do** and **To-do**: add a to-do to a task, which counts toward
  its progress, and tick a to-do off or open it again.
- **Plan**: recalculate the plan, level it, save a baseline of it, or move
  work that has not started up to the status date.

Each card does what the same action on its page would, with your access: a
change you could not make there is refused on the card too. Project
settings, members, uploads and evidence files stay on their own pages. Other pages can
start a turn for you: **Draft tests** on the V&V dashboard's *Top gaps* card
opens the panel and asks for test cases for those gaps (see *V&V* → *Gaps*).

Starting a conversation needs editor access to the project, and a workspace
its plan has made read-only cannot start one. In either case the panel says so
and what to do instead of a chat.

On a workspace still on an earlier release, the assistant is a tab of the
**Notes** panel on the Requirements page instead, beside the artifact's
History.

Assistant turns execute on an agent runner like any other agent work. If no
runner is online the panel says so and pauses replies — messages you send are
saved and answered as soon as a runner connects (see *Runs & runners*).
`;

export default content;

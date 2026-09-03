// User manual chapter: Guided requirements wizard + the V&V Assistant chat.
const content = `
# Guided definition & the V&V Assistant

**Guided Definition** walks you from a blank product to a committed, traceable
requirement set in eight steps — with the V&V Assistant alongside, suggesting
entries you can add with one click.

## Starting, resuming, modifying

Open **Guided Definition** from the project sidebar (the Overview page also
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
   live.

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

The same assistant is also in the **Notes** panel on the Requirements page,
under its own tab beside Comments. It is one conversation per project, so what
you asked in the wizard is there in the notes and the other way round. With an
artifact selected the assistant is told which one you are reading and answers
about it; with nothing selected it answers for the project as a whole. Comments
stay where they were — they belong to an artifact, the conversation does not.

Suggestion cards appear in the notes chat too, but they can only be added from
the wizard, which is where the entry sections they fill in live.

Assistant turns execute on an agent runner like any other agent work. If no
runner is online the panel says so and pauses replies — messages you send are
saved and answered as soon as a runner connects (see *Runs & runners*).
`;

export default content;

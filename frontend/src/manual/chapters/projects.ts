// User manual chapter: Projects & members.
const content = `
# Projects & members

A **project** holds everything for one product or system: its requirements
artifacts, traceability links, board, agents, runs, interviews, and V&V data.

## The Projects page

After signing in you land on the Projects page for the active workspace. Each
project appears as a card showing its name, description, short ID, and creation
date. Hovering a card reveals its action buttons:

| Icon | Action |
| --- | --- |
| ⧉ | **Save as template** — snapshot this project as a reusable template |
| ✎ | **Edit project** — rename it or change the description |
| ↓ | **Download** — opens the download wizard (format, sections, attachments) |
| ✕ | **Delete project** — removes the project and all its artifacts |

Click anywhere else on the card to open the project.

## Creating a project

Click **+ New Project**. The form offers a **Project Type**:

- **Blank Project** — start empty; enter a name and optional description.
- **Templates** — start from a template you previously saved with ⧉.
- **Examples** — start from a bundled example project (when the server ships
  any), useful for exploring the product with realistic data.
- **🎲 Random product (for testing)** — start from a made-up product, when
  what you want is something to try OpenV on rather than your real work.

Choosing a template or example pre-fills the name and description; you can
change both before creating.

### Random products and the community pool

The random mode rolls a fake product — a name, a category, a one-line
description, a vision and an audience — and pre-fills the form with it.
**Creating** then drops you into the Guided Wizard with that framing already
in place.

Three things feed the roll: the built-in concepts, the products your own
connected agent invents (**✨ Invent with agent**, available while a runner is
online), and the **community pool** — the fake products other OpenV users have
shared. The pool is the one thing in OpenV every workspace reads and writes:
anything published there is visible to everyone, so it is deliberately jokes
and nothing else. What your agent invents joins it automatically, which is how
it grows without anyone doing chores — so keep real people, customers and
unreleased work out of the invention brief.

Above the card is a filter:

- **🎲 Random** — reroll over the whole pool, which is the mode's original
  behaviour.
- **Top 5 all time** — the five most-voted products ever.
- **Top 5 this week** — the five most-voted products of the last seven days,
  a rolling window rather than a Monday-to-Sunday one.

Each entry in a Top 5 list shows its votes and a **Use this** button that puts
it on the card and in the form.

**▲ Vote** on the card says "keep this one": one vote per account, pressed
again to take it back, and the counts you see are everyone's. The hint beside
it (*· N this week*) is that product's votes inside the weekly window. Only
products that actually reached the shared pool can be voted for — a built-in
concept, or an invention that stayed in this browser because the pool was
full or the name was taken, has nothing for anyone else to vote on, and the
arrow says so.

**⚑ Report** is the other side of the same coin: flag anything that does not
belong, and three different people reporting it hides it from everyone's roll
list pending review.

## Downloading a project

There is one way out of a project, and it is the **↓ Download** button — on the
project card here, and in the Requirements toolbar inside the project. It opens
a two-step wizard.

**1. Format** — what shape you need it in:

| Format | What it is |
| --- | --- |
| PDF specification | The document as a reader sees it: sections, artifacts, figures, traceability |
| Word document | The same specification as a .docx, for editing or review outside OpenV |
| JSON data | The complete project, and the only format an OpenV import reads back |
| CSV table | One row per artifact for a spreadsheet; links fold into a single column |
| Excel workbook (.xlsx) | A sheet per artifact type, a sheet of traceability links, and a cover naming the snapshot |
| ReqIF interchange | The OMG format read by DOORS and Polarion |

**2. Content** — how much of it:

- **Sections** — tick the top-level sections to include. A section brings
  everything underneath it, however deep. Each shows how many artifacts it
  holds, so you can see what leaving one out costs.
- **Artifact types** — tick the types to include (requirements, test cases,
  and whatever else this project holds), and whether **Headings** come with
  them. Untick headings for a plain list of artifacts with no document
  structure around them.
- **Attachments** — tick the categories of file to bring along: figures,
  unnumbered images, documents, data files. Only the categories this project
  actually holds are offered. Ticking any makes the download a **.zip**: the
  document plus an \`attachments/\` folder, each file named by its figure
  reference where it has one.

The line at the foot of the wizard says what you are about to get — "1 of 3
sections, no headings, with figures" — and the button refuses to download a
selection that would produce an empty file.

Whatever you pick applies to whichever format you chose: the filters are
applied once, to the project snapshot, before the PDF or the CSV is built from
it. A baseline selected in the Requirements toolbar is downloaded instead of
the live project.

## Import

- **↑ Import Project** takes a JSON file previously produced by the wizard's
  **JSON data** format and creates a new project from it.
- Download/import is a simple way to move a project between servers or keep an
  offline copy.
- The other formats are one-way: CSV, Excel, PDF, Word and ReqIF are snapshots to read
  or to hand to another tool, not something OpenV reads back.

## Inside a project

Opening a project shows the dark left sidebar with the project's modules:

| Entry | What it is |
| --- | --- |
| Overview | Product definition, metrics, personas, needs, interviews |
| Requirements | The artifact tree — create, edit, link, baseline |
| Guided Definition | Step-by-step wizard from framing to committed requirements |
| V&V | Coverage, gaps, test runs, PDF report |
| Matrix | Traceability matrix grid |
| Board | Kanban board with human and agent assignees |
| Crew | Crew builder — org chart of agents and people |
| Agents | Agent definitions and launcher |
| Automations | Scheduled and event-triggered agent runs |
| Runs | Agent run history and proposal review |
| Settings | Project access, repositories, agent auth, danger zone |

The project name under the workspace switcher takes you back to all projects.
Your user block at the bottom of the sidebar opens **Settings** (personal
runner and agent sign-ins) and **Sign out**.

## Project access (members)

Project membership is managed in **Settings → Access** inside the project:

- **Add a member by email.** The person must already have an OpenV account —
  if no account exists for that email you'll see a message asking them to sign
  up first.
- Each member has a **role** — **viewer**, **editor**, or **owner** (the add
  form defaults to editor; whoever creates a project becomes its owner) — you
  can change it from the members table, and members can be removed.
- **Team access** grants a whole workspace team access to the project at once,
  with a role per grant. Teams themselves are defined in Workspace settings —
  see *Workspace settings & teams*.

## Deleting a project

Use the ✕ on the project card, or **Settings → Danger Zone** inside the
project. Deletion removes the project and all of its artifacts — download first
if you might need it again.
`;

export default content;

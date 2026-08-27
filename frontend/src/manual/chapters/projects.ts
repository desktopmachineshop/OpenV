// User manual chapter: Projects & members.
const content = `
# Projects & members

A **project** holds everything for one product or system: its requirements
artifacts, traceability links, board, agents, runs, interviews, and V&V data.

## The Projects page

After signing in you land on the Projects page for the active workspace. Each
project appears as a card showing its name, description, short ID, and creation
date. Hovering a card reveals four icon buttons:

| Icon | Action |
| --- | --- |
| ⧉ | **Save as template** — snapshot this project as a reusable template |
| ✎ | **Edit project** — rename it or change the description |
| ↓ | **Export project** — download the whole project as a JSON file |
| ✕ | **Delete project** — removes the project and all its artifacts |

Click anywhere else on the card to open the project.

## Creating a project

Click **+ New Project**. The form offers a **Project Type**:

- **Blank Project** — start empty; enter a name and optional description.
- **Templates** — start from a template you previously saved with ⧉.
- **Examples** — start from a bundled example project (when the server ships
  any), useful for exploring the product with realistic data.

Choosing a template or example pre-fills the name and description; you can
change both before creating.

## Import & export

- **↑ Import Project** takes a JSON file previously produced by
  **Export project** and creates a new project from it.
- Export/import is a simple way to move a project between servers or keep an
  offline copy.

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
- Each member has a **role** (for example viewer or editor) you can change from
  the members table, and members can be removed.
- **Team access** grants a whole workspace team access to the project at once,
  with a role per grant. Teams themselves are defined in Workspace settings —
  see *Workspace settings & teams*.

## Deleting a project

Use the ✕ on the project card, or **Settings → Danger Zone** inside the
project. Deletion removes the project and all of its artifacts — export first
if you might need it again.
`;

export default content;

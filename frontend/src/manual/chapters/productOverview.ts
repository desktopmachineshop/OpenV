// User manual chapter: Product Overview page.
const content = `
# Product Overview

The **Overview** page is the project's landing page, first under **Plan**.
It says what the product is and where the project stands: the product's
definition, metrics, constraints, personas and needs on the left, and on the
right the project's figures, what needs you and its latest activity. On a
phone, where the project stands comes first.

## Where the project stands

Four tiles give the share of requirements **traced** and **verified**, how
many tests are **failing** and how many links are **suspect**; each opens the
page behind its number. **Needs you** lists what waits on the team: reviews,
suspect links, test runs in progress, failing tests, unread comments and the
changes agents have proposed (see *Home* in the project pages chapter for
each kind of line). **Recent activity** shows the newest events, and **Full
history** opens the **Activity** page.

## Product definition

Three fields, editable in place with **Edit**; beside it is the way into the
guided definition (labeled *Start*, *Resume* or *Modify guided definition*
depending on where the project is):

- **Vision** — what the world looks like once this product succeeds.
- **Problem statement** — what problem, for whom, and why now.
- **Target users** — the primary users and buyers.

Step 1 of the guided wizard reads and writes the same fields, so the wizard
and the overview always agree.

## Success metrics

A table of metrics with **target**, **current** value, and **unit** — e.g.
"Weekly active users, target 500". Add rows with **+ Add metric** and press
**Save metrics**.

## Constraints

Named constraints with descriptions (regulatory limits, budget, platform
requirements…). **+ Add constraint**, then **Save constraints**.

## Personas & user needs

Two cards summarizing the project's *persona* and *user-need* artifacts —
usually created by the guided wizard (which links each need to its persona).
Click **View in Requirements** to see and edit them as artifacts.

## User interviews

Create and manage AI-led stakeholder interviews from here — see the
*Interviews* chapter for the full flow.
`;

export default content;

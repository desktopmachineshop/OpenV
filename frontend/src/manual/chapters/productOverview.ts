// User manual chapter: Product Overview page.
const content = `
# Product Overview

The **Overview** page (the project's landing page) is the single source of
truth for what the product is and how success is measured.

## Quick links

At the top: a shortcut into the guided definition (labeled *Start*, *Resume*,
or *Modify guided definition* depending on where the project is), the **V&V
dashboard**, and the **Board**.

## Product definition

Three fields, editable in place with **Edit**:

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

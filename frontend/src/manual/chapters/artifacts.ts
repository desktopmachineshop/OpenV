// User manual chapter: Requirements artifacts, versions, baselines.
const content = `
# Requirements & artifacts

The **Requirements** view is the heart of a project: a tree of artifacts with
an editor, version history, image attachments, comments, and baselines.

## Artifact types

| Type | Purpose |
| --- | --- |
| requirement | What the system shall do (functional and non-functional) |
| test-case | A procedure that verifies requirements or validates needs |
| hazard | A potential source of harm, for safety-critical work |
| design-item | A design decision or component that realizes requirements |
| persona | A key user archetype (usually created by the guided wizard) |
| user-need | A need in "As persona, I need capability so that outcome" form |
| other | Anything else — assumptions, constraints, notes |

## The layout

The view has three resizable columns (drag the dividers to resize; widths are
remembered):

- **Left** — the artifact list/tree with search and filters.
- **Center** — the selected artifact: header, body, links, attachments.
- **Right** — the **Chatter** panel: comments on the selected artifact.

A floating **?** button opens a quick-reference help sidebar with artifact and
link type definitions.

## Creating and organizing artifacts

- Click **+ New Artifact**, choose a type, enter a title and a body
  (the body is markdown — headings, lists, tables, and images all render).
- Artifacts form a **hierarchy**: right-click an artifact in the list for
  **create before / create after / create child**. New siblings inherit the
  type and parent of the artifact you clicked.
- The same menu offers **copy**, **paste before / paste after** and
  **duplicate** (a copy placed directly after the original). A copy carries the
  type, title, body and attributes; links and figures stay with the original,
  because a copy that inherited "verifies REQ-12" would assert a verification
  nobody made. Pasted artifacts are titled "… (copy)".
- **Drag artifacts** within the same parent to reorder them.
- **Collapse all / Expand all** manage the tree at once.

## Referencing figures and linked artifacts

Type **\`#\`** in an artifact's description to cite something: its own figures
(\`REQ-17-FIG-1\`) and the artifacts it is already linked to. Filter by typing,
choose with the arrow keys or the mouse, and the reference is inserted as
\`#REQ-17-FIG-1\`.

The list is deliberately short. It offers only what this artifact is already
connected to, because a description citing a requirement it has no link to is
a claim the traceability matrix cannot see — link it first, and it appears in
the menu.

## Search & filters

The search box matches title, body, type, IDs and attributes. The ⚙ button
opens the filter panel:

- **Filter rows** — combine conditions on any field (Type, Title, Body,
  Version, dates, …) with comparators like contains, equals, starts with,
  greater than.
- **Filter logic** — AND (all rows must match) or OR (any row).
- **Presets** — name the current search + filters and save it; apply or delete
  saved presets from the dropdown. Presets are stored in your browser.

## Editing, versions & history

Select an artifact to see its details; **Edit** opens the editor.

- Every save creates a **new version** — the header shows "Version N".
- **History** (available once an artifact has more than one version) lets you
  preview any previous version and **restore** it. Restoring creates a new
  version with the old content; nothing is lost.

## Attachments

Images can be attached to an artifact (upload while editing). They appear in a
gallery on the artifact details, with a lightbox view. Attachments can be
deleted from the details or the editor.

## Comments (Chatter)

The right-hand Chatter panel holds a comment thread per artifact — use it for
review discussion instead of editing the requirement text itself.

## Baselines

A **baseline** is an immutable snapshot of the whole project (artifacts +
links) at a point in time. The top bar of the Requirements view controls them:

- **Capture Baseline** — name and save a snapshot of the live project.
- The **baseline dropdown** switches between "Live Project" and any baseline.
  Baseline views are **read-only** — you can browse but not edit.
- 🗑 deletes the selected baseline (the live project can't be deleted here).
- **Generate Report** — produces a report for the live project or the selected
  baseline.

Baselines also drive comparisons in the **V&V** dashboard and the
**Matrix** view, both of which have their own baseline selector.
`;

export default content;

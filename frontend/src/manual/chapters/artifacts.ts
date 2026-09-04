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
- **Drag an artifact** onto another to move it. Where you let go decides what
  happens: the **top half** drops it before that artifact, the **bottom left**
  after it, and the **bottom right** makes it a **child** of it. The row shows
  which it will be — a line on the edge it will land against, or an outline
  around the artifact it will go inside — so re-parenting and reordering are
  the same gesture. Dropping into a collapsed artifact expands it, and an
  artifact can never be dropped into its own subtree.
- **Collapse all / Expand all** manage the tree at once.

## Referencing figures and linked artifacts

Type **\`#\`** in an artifact's description to cite something: its own figures
(\`REQ-17-FIG-1\`) and the artifacts it is already linked to. Filter by typing,
choose with the arrow keys or the mouse, and the reference is inserted as
\`#REQ-17-FIG-1\`.

Outside edit mode a reference is a link: clicking an artifact reference selects
that artifact, and clicking a figure reference opens the figure where you are,
so following a citation never loses your place.

The list is deliberately short. It offers only what this artifact is already
connected to, because a description citing a requirement it has no link to is
a claim the traceability matrix cannot see — link it first, and it appears in
the menu.

## Making room

The **project menu** and the **Notes** panel each have three states, chosen
from the control at the foot of the menu and the button in the Notes header:

- **Pinned** — always open, taking its width from the page.
- **Auto-hide** — a thin strip at the edge; hovering it opens the panel over
  the document, and moving away closes it. The document does not reflow.
- **Hidden** — only the strip remains, which brings the panel back.

Each choice is remembered per person and per panel. The artifact tree fills the
height of its column, so a taller window shows more of the document rather
than more empty space.

The menu's groups — Define, Verify, Plan, Agents — start collapsed, so the menu
is a handful of lines instead of a full column of links. Click a group heading
to open or close it; the group holding the page you are on opens by itself
until you close it, and your choices are remembered. A menu longer than the
window scrolls on its own.

Nothing on the page is sized to the browser window, so the window itself never
scrolls: the tree, the document, and the Notes panel each scroll inside their
own column, and the toolbar takes a second row on a narrow window without
pushing anything off the bottom.

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

## Figures

Images attached to an artifact are **figures**. Each is numbered from the
artifact's own reference — \`REQ-17-FIG-1\` — and that number is never reissued,
so it stays a safe citation even after the figure it named is deleted. The
stored file takes the figure's name too, so downloading one saves
\`REQ-17-FIG-1.png\` rather than whatever the camera called it.

Figures are added, replaced and removed **while editing** the artifact — what
the document shows changes by a deliberate edit, not a stray click while
reading. Use **⬆** on a figure to upload a new version: the figure keeps its
reference, the artifact takes a new version, and the notes record the change.
**🕘** shows the history, and every superseded version stays viewable.

On the details view figures are read-only, with the lightbox for a closer look.

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
- **↓ Download** — opens the download wizard for the live project or the
  selected baseline: PDF, Word, JSON, CSV or ReqIF, narrowed to the sections,
  types and attachments you pick. See the Projects chapter.

Baselines also drive comparisons in the **V&V** dashboard and the
**Matrix** view, both of which have their own baseline selector.
`;

export default content;

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

The **?** button beside the notification bell opens a quick-reference help sidebar with artifact and
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
  nobody made. Pasted artifacts are titled "… (copy)", and the copy's own
  history starts with a single note — *Copied from REQ-12 (version 3)* — so
  you can always tell where it came from.
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

Type **\`##\`** to cite a figure on **any** artifact in the project. The menu
lists this artifact's own figures first, then everyone else's with the artifact
each belongs to, and the citation is written \`##REQ-99-FIG-2\` so a reader can
see at a glance that it reaches outside what they are reading.

Outside edit mode a reference is a link: clicking an artifact reference selects
that artifact, and clicking a figure reference opens the figure itself — the
drawing, the datasheet, the model — wherever in the project it lives, so
following a citation never loses your place.

The single-\`#\` list is deliberately short. It offers only what this artifact
is already connected to, because a description citing a requirement it has no
link to is a claim the traceability matrix cannot see — link it first, and it
appears in the menu. Figures are the exception \`##\` makes: pointing a reader
at a drawing asserts nothing about how two artifacts relate, so it needs no
link to justify it.

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

Files attached to an artifact are **figures**. Each is numbered from the
artifact's own reference — \`REQ-17-FIG-1\` — and that number is never reissued,
so it stays a safe citation even after the figure it named is deleted. The
stored file takes the figure's name too, so downloading one saves
\`REQ-17-FIG-1.png\` rather than whatever the camera called it.

### What can be attached

- **Images** — PNG, JPEG, GIF, WebP, SVG, TIFF, BMP.
- **PDFs** — a supplier datasheet, a standard, a test report.
- **CAD** — STEP, IGES, STL, 3MF, OBJ, PLY, glTF, DXF, DWG, and the common
  native formats (SolidWorks, Inventor, CATIA, Fusion, Parasolid, Rhino).

One figure may be up to 25 MB. Attach the file your team actually works from,
so the requirement points at the real thing rather than a picture of it.

### Looking at one

Clicking a figure opens it. An image is shown full size, a PDF opens in a
reader with the pages scrollable, and an **STL** is drawn as a 3D preview you
can drag to turn, with its triangle count and bounding box underneath.

Other CAD formats are **not** previewed: a STEP file or a native part needs a
geometry kernel to interpret, and an approximation of a part is worse than
none because a reviewer would trust it. Those open a panel naming the format
and its size, with a Download button — open it in the tool that owns it.

Every figure, previewable or not, has that Download button.

### Changing one

Figures are added, replaced and removed **while editing** the artifact — what
the document shows changes by a deliberate edit, not a stray click while
reading. Use **⬆** on a figure to upload a new version: the figure keeps its
reference, the artifact takes a new version, and the notes record the change.
**🕘** shows the history, and every superseded version stays viewable.

A figure may change format between versions — a sketch replaced by the real
drawing, a screenshot replaced by the STEP file — and keeps its number.

From that history you can **Restore** any earlier version. The figure goes
back to the image *and* the name it had then — a restore that brought back
the drawing but left a later rename in place would show one figure under
another's name.

Restoring works the way it does for an artifact: it is recorded as a **new
version** with the old content, and nothing is deleted. So the history after
restoring version 1 over version 3 reads v1, v2, v3, v4 — with v4 marked
*restored from v1* — and version 3's drawing is still there to open. That
matters when a requirement was reviewed, or a baseline captured, against
what the figure used to show: rewinding and discarding would destroy the
record of what the reviewer actually saw.

Restoring is an editor's action, and the version already showing cannot be
restored over itself.

A figure also has a **name**, shown under its reference and printed under
the picture in PDF and Word documents. Until you give it one, the name is
the filename it was uploaded with — which for a screenshot is usually a
timestamp. Use **✎** on a figure to rename it: the new name is a figure
version like a new image is, with who set it and when, so the history shows
every name the figure has had, and the artifact takes a new version with a
note recording the change. Clearing the name goes back to the uploaded
filename.

### In generated documents

Only images are laid into a generated PDF or Word document. A datasheet or a
model has nothing to draw, so it is listed and citable rather than printed as
an apology in the middle of the specification; its file rides along in a
download's zip, under the **Models** or **Documents** group rather than
**Figures**.

On the details view figures are read-only, with the viewer for a closer look.

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
  selected baseline: PDF, Word, JSON, CSV, Excel or ReqIF, narrowed to the sections,
  types and attachments you pick. See the Projects chapter.

Baselines also drive comparisons in the **V&V** dashboard and the
**Matrix** view, both of which have their own baseline selector.
`;

export default content;

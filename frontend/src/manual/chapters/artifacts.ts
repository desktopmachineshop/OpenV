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

Type **\`##\`** to reach the **whole project**: every figure, and every other
artifact, whether or not this one is linked to it. The menu lists this
artifact's own figures first, then everyone else's with the artifact each
belongs to, then the project's artifacts. The citation is written
\`##REQ-99-FIG-2\` so a reader can see at a glance that it reaches outside what
they are reading.

Outside edit mode a reference is a link: clicking an artifact reference selects
that artifact, and clicking a figure reference opens the figure itself — the
drawing, the datasheet, the model — wherever in the project it lives, so
following a citation never loses your place.

**The two markers mean different things, and that is the point.** A single
\`#\` offers only what this artifact is already connected to, so a citation
written with one marker is a claim the traceability matrix can see. \`##\`
says out loud, in the text as well as in the menu, that the citation reaches
outside those connections — useful for pointing at a drawing or a neighbouring
requirement, but not a substitute for linking two artifacts that genuinely
relate. If the relationship matters, link it, and it appears under \`#\`.

**The quality linter checks this for you.** Citing an artifact you have no
traceability link to raises **Citation with no link**, an error carrying the
same weight as unfinished placeholder text: the description claims a
connection the matrix does not hold, so coverage and impact analyses reading
the links will disagree with the requirement as written. Link the two
artifacts and the finding goes. Citing a **figure** never raises it — a
drawing is evidence, not a claim about how two artifacts relate. If your
project wants the check quieter, its severity is yours to set in the quality
rules, down to off.

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

Two boxes search, and they do different jobs. The one in the **sidebar**
searches every project you can see; the one above the **tree** narrows the
project you are in.

The sidebar box matches titles and bodies, and it also matches **refs**: type
**REQ-30** and REQ-30 comes back first, ahead of anything that merely mentions
it. Case does not matter, so **req-30** works, and a ref pasted out of a
report with spaces around it still finds its artifact. Every result shows its
ref beside the title, which is what tells two similarly named requirements
apart. A longer ref that contains what you typed still appears below the exact
match: **REQ-3** finds REQ-3 first and leaves REQ-30 further down.

The tree box matches title, body, type, IDs and attributes. The ⚙ button
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

## Review status

Every artifact carries a review status: **draft**, **in review**, **approved**
or **superseded**. Draft goes to in review when somebody submits it, in review
goes back to draft or on to approved, and approved artifacts are eventually
superseded. Editing an **approved** artifact's type, title or body puts the
new version back into draft — the approval was of the words that changed, and
the approved version itself stays in the artifact's history untouched.

### Sending the whole project for review

Submitting a hundred requirements one at a time is nobody's idea of a review
process, so the **Review Queue** has a **Send project for review** button that
moves every draft artifact in the project into review at once. It covers the
whole document — headings and descriptions included, because a heading in the
wrong place or a description that contradicts the requirements under it is
exactly what a review is for.

Run it again whenever a review cycle comes round. The second run is where the
work is saved:

- an approved requirement nobody has touched **stays approved**, so nobody is
  asked to sign off the same words twice;
- one that was **edited since it was approved** is back in draft, so the run
  pulls it into review again;
- anything still waiting on a reviewer is left where it is.

What comes back says how many artifacts were sent and how many stayed
approved, so a quiet cycle is visibly quiet rather than indistinguishable from
a button that did nothing.

### Working the queue

The **In review** table is where the reviewing happens. Each row carries what
you need to judge without opening anything: the type and reference, the title,
the start of the description, and small previews of the artifact's figures —
pictures as thumbnails, PDFs and CAD files as chips naming them. Any of them
opens the full file; the title opens the artifact.

Two buttons sit on every row:

- **Approve** signs the artifact off as it stands. Approving also clears the
  suspect flag on every link that touches it, because you have just vouched
  for the artifact its links were made from.
- **Send back** asks what needs to change before it does anything. The comment
  is required, and it is written in the same composer the notes panel uses —
  \`@name\` reaches a person, \`@@name\` raises them a to-do, \`#REQ-12\`
  cites — and it lands on the artifact's feed as an ordinary note. Then the
  artifact goes back to draft. Nobody gets work returned without being told
  why.

### Deciding several at once

Tick the box on any row, or the box in the header to take the whole table, and
the two buttons above it act on everything selected: **Approve selected** asks
once and signs them all off, **Send back selected** asks for one reason and
posts it on every one of them.

Anything that could not be decided — a refused permission, an artifact
somebody else moved while you were reading — is counted and reported, and
stays in the table. The rest still go through.

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

How big one figure may be follows your workspace's plan — the **Limits** tab
in workspace settings gives the number, under *Largest figure*. It is measured
in hundreds of megabytes, because a CAD assembly is a figure like any other.
Attach the file your team actually works from, so the requirement points at
the real thing rather than a picture of it.

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

## History

The **History** tab in the right-hand Notes panel is the artifact's record: what
OpenV wrote down each time the artifact changed, and what people have said about
it, newest first. A new version, a status move, a link added or removed, a figure
replaced, a test result recorded — each leaves an entry, alongside the notes
members write themselves.

Three buttons above the list decide what you are reading:

- **All** — the whole record, changes and comments interleaved.
- **Changes** — only what OpenV recorded. This is the audit trail: who changed
  what, and when.
- **Comments** — only what people wrote. This is the review discussion.

The panel opens on **Comments**, which is what most people come to it for; the
recorded changes are the backdrop.

Add a comment in the box at the foot of the panel. Use it for review discussion
rather than editing the requirement text itself — the text is the requirement,
the comment is what you think of it. Writing one while reading **Changes**
switches you back to **Comments**, so you can see what you just added.

### Tagging inside a comment

A comment can name people and point at things, with the same two-marker
convention the descriptions use.

- **\`@\`** offers the people on the project. Choosing one writes the name
  the mention resolves to, which matters: a handle typed from memory can name
  nobody at all, and there is nothing to see when it does. The person is
  notified.
- **\`@@\`** names someone **and raises a to-do for them** as the comment is
  posted — the comment becomes the card, assigned to whoever it named, linked
  to this artifact and carrying the whole note as its description. Use it when
  you already know you are asking for work rather than talking.
- **\`#\`** and **\`##\`** cite a figure or an artifact, exactly as they do in
  a description, and the citation is a link a reader can follow.

Raising a to-do **after** a comment is posted still works and has not changed:
the **Add to-do** control on a comment that names somebody lets you set the
title, the person and a due date. \`@@\` is the shortcut, not a replacement —
it takes the note's first line as the title and no due date.

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

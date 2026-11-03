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

The view has two parts side by side, and a third you can call up:

- **The list** — every artifact in the project, as a **table**, a **tree** or
  **layers** (see below). Above it sit the type tabs, the project search and
  its filters, and a line of figures for what is in view.
- **The detail panel** — on the right, the selected artifact: its status,
  quality and priority, its text, its place in the trace and its tests (see
  *The detail panel*). Drag its left edge to make it wider; the width is
  remembered.
- **Notes** — the **Chatter** panel at the far right: comments on the selected
  artifact and the V&V Assistant (see *Making room*).

The header holds the baseline picker and the project actions — **Compare**,
**Capture Baseline**, **Draft test cases**, **↓ Download** — with one blue
button, **+ New Artifact**, at its end.

**Help** in the top bar (or **?** on the keyboard) opens the help panel for
this page, with the artifact and link types.

## Table, tree and layers

The **Table / Tree / Layers** switch above the list changes how the project
is drawn; the choice stays in the page's address (\`?view=\`), so a link or a
reload keeps it. The **type tabs** (*All*, *Requirement*, *Test Case*, …) show
one type at a time in any of them, with a count beside each.

- **Table** — one dense row per artifact, grouped under the heading it sits
  in (click a heading to fold it). The columns are the **ref**, the **title**,
  the review **status**, the **priority** attribute, the **quality** score,
  **trace** (↑ how many artifacts this one answers to, ↓ how many answer to it,
  and ⚠ when one of those links is suspect), **V&V** (the verification
  rollup: a green dot passes, red fails, orange is blocked, grey is not run or
  not covered) and how long ago it was **updated**.
- **Tree** — the document's hierarchy with the same columns: guide lines
  show where each artifact sits, the caret opens and shuts a branch, and
  **Depth L1–L4** opens the whole tree to that level (**L1** collapses all,
  **All** expands all). For a heading or a parent, *V&V below* rolls up the
  requirements under it, as *passing/total*.
- **Layers** — one column per level of the hierarchy. Pick an artifact and
  the next column shows what sits under it, so a whole chain — need,
  requirement, sub-requirement, design — reads left to right, each card with
  a bar of how much below it is verified. The last column lists the tests
  that verify any artifact of the chain, with their latest result.

The figures above the list count what is in view: artifacts, the share
**traced** (with at least one link), the share **verified** (requirements
whose verification passes), **suspect links** and **quality findings**.

On a phone the list's views are chips under the search box: **List** (two
lines per artifact: ref, status and age, then the title, then quality, trace
and verification), **Outline** (the tree) and **By level**, which walks the
hierarchy one level at a time — the artifact you are on, what it traces up
to, what it decomposes into and the tests that verify it; tap a child to go
down, **‹** to come back up, **Open full artifact** to read it. The chips
**In review**, **Failing** and **Suspect** narrow the list to what needs you.

## The detail panel

The selected artifact opens on the right (on a phone, in the **Document**
pane). Its header carries the **ref**, the **status**, **quality** and
**priority** chips, and **Edit**, **History** and **Delete**; below the title
are its type, owner, verification method, version and when it last changed,
then the review step it can take next (**Submit for review**, **Approve**, …).

- **Trace** lists every artifact this one is linked to, ↑ above and ↓ below,
  with the link's type; a **suspect** link is marked in yellow. Click one to
  go to it.
- **Verification** shows how many of the tests verifying it pass, a
  pass / fail / blocked bar, each test with its latest result, and the
  requirement's rollup.
- Below them are the **description** (with **Show impact →**), the
  attributes, the figures and the full list of **traceability links**, where a
  link can be deleted.

On a phone the artifact ends in a bar under your thumb: **Comment** opens the
Notes pane, and the review step forward (**Submit for review**, **Approve**)
is the blue button beside it.

## Creating and organizing artifacts

- Click **+ New Artifact**, choose a type, enter a title and a body
  (the body is markdown — headings, lists, tables, and images all render).
- Artifacts form a **hierarchy**: right-click an artifact in the table or
  the tree, or click its **⋯** button, for **create before / create after /
  create child**. New siblings inherit the
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
- **Move up** and **Move down** in the same menu swap an artifact with the
  one beside it; on the keyboard, focus a row in the tree and press
  **Alt+↑** or **Alt+↓**. The arrow keys move between rows, **→** and **←**
  open and shut a branch, and **Enter** opens the artifact.
- **Depth L1–L4** above the tree opens it to a level at once: **L1**
  collapses all, **All** expands all.

## Moving through the document

The artifact you are reading carries **‹** and **›** and its place in the
project — *12 of 148*. They step through it in the order the tree shows: a
heading, then what is under it, then the next heading. They follow whatever your
search and filters have left in view, so narrowing the tree to one type or one
owner turns ‹ / › into a reader for just those.

On a keyboard, **J** goes to the next artifact and **K** to the previous one.
They work from anywhere on the page except inside a field, an open editor or a
dialog — so typing a *j* into the search box stays a *j*. (They are the physical
J and K keys on a standard layout; on a layout that puts those letters elsewhere,
use the ‹ / › buttons.)

On a phone or tablet, swipe the document sideways: **left** for the next
artifact, **right** for the one before. Scrolling up and down is never
interrupted, and a table or code block wide enough to scroll keeps its own
sideways swipe. The **Tree / Document / Notes** buttons still switch panes:
**Tree** holds the list in whichever view you chose.

Wherever a step lands, the tree opens the sections above it so you can see where
you are — and only those, so a section you collapsed on purpose stays shut.

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

The project menu takes only a narrow rail at the left of the page (see
*Getting started* → *Finding your way around*). The **Notes** panel has three
states, chosen from the button in its header:

- **Pinned** — always open, taking its width from the page.
- **Auto-hide** — a thin strip at the edge; hovering it opens the panel over
  the document, and moving away closes it. The document does not reflow.
- **Hidden** — only the strip remains, which brings the panel back.

The choice is remembered per person. Until you choose, the panel starts
auto-hidden on anything narrower than a wide desktop (1680 pixels), so the
list and the detail panel have the room. The list fills the height of the
page, so a taller window shows more of it rather than more empty space.

Nothing on the page is sized to the browser window, so the window itself never
scrolls: the tree, the document, and the Notes panel each scroll inside their
own column, and the toolbar takes a second row on a narrow window without
pushing anything off the bottom.

## Search & filters

Two boxes search, and they do different jobs. The one in the **top bar**
searches every project you can see; the one above the **list** narrows the
project you are in.

The top bar's box matches titles and bodies, and it also matches **refs**: type
**REQ-30** and REQ-30 comes back first, ahead of anything that merely mentions
it. Case does not matter, so **req-30** works, and a ref pasted out of a
report with spaces around it still finds its artifact. Every result shows its
ref beside the title, which is what tells two similarly named requirements
apart. A longer ref that contains what you typed still appears below the exact
match: **REQ-3** finds REQ-3 first and leaves REQ-30 further down.

The list's box matches title, body, type, IDs and attributes. **Filter**
beside it opens the filter panel, and while a search or a filter is on, a
blue chip beside it says what it is; click the chip to clear them all:

- **Filter rows** — combine conditions on any field (Type, Title, Body,
  Version, dates, …) with comparators like contains, equals, starts with,
  greater than.
- **Filter logic** — AND (all rows must match) or OR (any row).
- **Presets** — name the current search + filters and save it; apply or delete
  saved presets from the dropdown. Presets are stored in your browser.

## Editing, versions & history

Select an artifact to see its details; **Edit** opens the editor.

- Every save creates a **new version** — the header shows "Version N". Adding
  a figure, uploading a new file for one or renaming one creates a version
  too.
- **History** (available once an artifact has more than one version) lets you
  preview any previous version and **restore** it. Restoring creates a new
  version with the old content; nothing is lost, the artifact keeps its
  reference, and the restore is recorded in the activity log.

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

A **baseline** is an immutable snapshot of the whole project at a point in
time: its artifacts and links, the attribute definitions in effect, and each
attachment's name, type and size (the files themselves are not copied). The
top bar of the Requirements view controls them:

- **Capture Baseline** — name and save a snapshot of the live project.
- The **baseline dropdown** switches between "Live Project" and any baseline.
  Baseline views are **read-only** — you can browse but not edit.
- 🗑 deletes the selected baseline (project owners only; the live project
  can't be deleted here). The deletion is recorded in the activity log.
- **↓ Download** — opens the download wizard for the live project or the
  selected baseline: PDF, Word, JSON, CSV, Excel or ReqIF, narrowed to the sections,
  types and attachments you pick. See the Projects chapter.

Baselines also drive comparisons in the **V&V** dashboard and the
**Matrix** view, both of which have their own baseline selector.
`;

export default content;

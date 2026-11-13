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

The view has two parts side by side:

- **The list** — every artifact in the project, as a **table** or a **tree**
  diagram (see below). Above it sit the project search and its filters
  (with the chip search bar, the bar alone, across the list's width; without
  it, the type tabs and the search box), and a line of figures for what is
  in view.
- **The detail panel** — on the right, the selected artifact: its status,
  quality and priority, its text, its place in the trace and its tests (see
  *The detail panel*). Drag its left edge to make it wider; the width is
  remembered.

The selected artifact's comments and recorded changes are its **History**, the
last section of the detail panel (see *History*). The V&V Assistant floats over
the page from the **✦ Assistant** button in the bottom corner (see *Guided
definition & the V&V Assistant* → *The V&V Assistant on every page*).

One bar runs across the top: the search and its filters, the **Table / Tree** switch, then **Working copy** (it reads
**Baseline** while you view one), which opens the baseline picker with **Compare**, **Capture Baseline**, **Delete baseline**,
**Draft test cases** and **↓ Download**, and one blue button, **+ New Artifact**, at its end.

The **?** button in the top bar (or **?** on the keyboard) opens the help panel for
this page, with the artifact and link types.

## Table and tree

The **Table / Tree** switch above the list changes how the project is drawn;
the choice stays in the page's address (\`?view=\`), so a link or a reload
keeps it. To show one type at a time in either, open the search bar's
caret and tick a value under **Filters → Type**, which counts each type
beside it. (Without the chip search bar, the **type tabs** — *All*,
*Requirement*, *Test Case*, … — do the same.)

- **Table** — the document's hierarchy, one dense row per artifact, every
  artifact nested under the heading or parent it sits in, with guide lines
  showing where it sits. The caret beside a heading or a parent folds and
  unfolds what is under it. Headings carry the same columns as every other
  row: the **ref** and **title**, the review **status**, the **priority**
  attribute, the **quality** score, **trace** (↑ how many artifacts this one
  answers to, ↓ how many answer to it, and ⚠ when one of those links is
  suspect), **V&V below** (the verification rollup: a green dot passes, red
  fails, orange is blocked, grey is not run or not covered; for a heading or a
  parent it rolls up the requirements under it, as *passing/total*) and how
  long ago it was **updated**.
- **Tree** — the project drawn as a diagram of connected boxes (see *The tree
  diagram*).

The **fold all / unfold all** button beside the search folds every branch of
the view in front, so only the top level shows, or opens every level; it turns
to the other one each time you press it. Hover over it to see which it will do.

The figures above the list count what is in view: artifacts, the share
**traced** (with at least one link), the share **verified** (requirements
whose verification passes), **suspect links** and **quality findings**.

On a phone the list's views are chips under the search box: **List** (two
lines per artifact: ref, status and age, then the title, then quality, trace
and verification) and **Outline** (the hierarchy, one line per artifact, with
the fold all / unfold all button beside the chips). The chips **In review**,
**Failing** and **Suspect** narrow the list to what needs you.

Older links to the **Layers** view open the tree diagram, which shows the same
levels and their verification at a glance.

## The tree diagram

**Tree** draws the project's hierarchy as an org chart that flows left to
right: one box per artifact, with its type, **ref**, **status** and title, and
elbow connectors from each parent to its children, which are stacked in the
next column.

- **Folding** — the diagram opens with the first two levels showing. The small
  **⊖** on a parent's connector folds what is under it, and **⊕** (with the
  number of artifacts hidden beside it) unfolds it again. The fold all /
  unfold all button above the list does the same for every branch.
- **Colour by Type | Status** — colours each box by its artifact type (need,
  requirement, design item, test case, hazard, …) or by its review status
  (draft, in review, approved, superseded), with a key beside the switch. The
  type tag and the status stay written on every box, so the colours are a
  second cue, never the only one.
- **Opening an artifact** — click a box to select it and open it in the detail
  panel, as in the table. An artifact selected anywhere else, by a link or by
  **‹ / ›**, unfolds the branch it sits in and comes into view.
- **Moving around** — drag the background to pan, and use the mouse wheel or a
  pinch to zoom; **−**, **+** and **Fit** above the diagram zoom out, zoom in
  and fit the whole diagram on screen.
- **Keyboard** — **Tab** reaches the diagram; **↑** and **↓** move between the
  boxes in reading order, **→** unfolds a box or goes to its first child,
  **←** folds it or goes to its parent, **Home** and **End** go to the first
  and the last, and **Enter** opens the artifact. A screen reader announces the
  diagram as a tree, with each box's level and position.

On a phone, **Tree** shows the outline instead.

## The detail panel

The selected artifact opens on the right (on a phone, as a screen of its own: **‹ Requirements** at its top goes back
to the list). Its header carries the **ref**, the **status**, **quality** and **priority** chips, **Edit**, and a **⋯**
menu with **History** and **Archive**. Straight under the title is the artifact's own text (with **Show impact →** on a
requirement), then one line of facts: its type, owner, verification method, the **releases** it is planned for
(when it sets its own and the project plans target releases, such as *MVP onwards*), version and when it last changed.
**Details** opens them as a table with its UID. Then comes the review step it can take next (**Submit for review**, **Approve**, …).

- **Trace** lists every artifact this one is linked to, ↑ above and ↓ below,
  with the link's type; a **suspect** link is marked in yellow. Click one to
  go to it.
- **Verification** shows how many of the tests verifying it pass, a
  pass / fail / blocked bar, each test with its latest result, and the
  requirement's rollup.
- A design item also shows what it **satisfies** and **Verified through**: the test cases that verify those
  requirements, each with its last result (tests verify requirements, so this is the evidence that reaches the item).
- Below them are the attributes, the figures and the full list of **traceability links**, where a link can be deleted.

On a phone the artifact ends in a bar under your thumb: **Comment** scrolls to
its History, where the comment box is, and the review step forward (**Submit for review**, **Approve**)
is the blue button beside it. While an artifact is open, the app's own bottom bar steps aside for this one.

## Creating and organizing artifacts

- Click **+ New Artifact** (its form opens in the panel on the right; above the list on a phone), choose a type,
  enter a title and a body (the body is markdown — headings, lists, tables, and images all render).
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
- **Fold all / unfold all** above the table folds every branch at once, or
  opens them all.

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
sideways swipe. **‹ Requirements** at the top goes back to the list,
in whichever view you chose.

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
*Getting started* → *Finding your way around*). There is no third column: the
artifact's History is part of the detail panel, and the V&V Assistant floats
over the page and minimises back to its button, so the list and the detail
panel share the whole width. Drag the detail panel's left edge to change how
it is shared. The list fills the height of the page, so a taller window shows
more of it rather than more empty space.

On a workspace still on an earlier release, the comments and the assistant are
in a **Notes** panel at the far right instead, which is **Pinned**, **Auto-hide**
(a strip at the edge that opens the panel over the document on hover) or
**Hidden**, chosen from the button in its header.

Nothing on the page is sized to the browser window, so the window itself never
scrolls: the tree and the document each scroll inside their own column, and the toolbar takes a second row on a narrow window without
pushing anything off the bottom.

## Search & filters

Two boxes search, and they do different jobs. The one in the **top bar**
finds an artifact in any project you can see and opens it; it never filters
the page you are on. The one above the **list** filters the project you are
in.

### Finding and opening an artifact

Type in the top bar's box and the matches drop down below it, grouped by
project; click one, or pick it with the arrow keys and **Enter**, to open it
in its project. **Keyword** and **Semantic** at the top of the drop-down
choose how the text is matched. The box matches titles and bodies, and it also matches **refs**: type
**REQ-30** and REQ-30 comes back first, ahead of anything that merely mentions
it. Case does not matter, so **req-30** works, and a ref pasted out of a
report with spaces around it still finds its artifact. Every result shows its
ref beside the title, which is what tells two similarly named requirements
apart. A longer ref that contains what you typed still appears below the exact
match: **REQ-3** finds REQ-3 first and leaves REQ-30 further down.

### The search bar

The list's box is a **search bar with chips**. Each filter you add shows as a
small coloured chip inside the bar, such as **Status In review ×** or
**Owner Dave ×**, so you can see at a glance what is narrowing the list.
Click a chip's **×** to remove that filter alone. Whatever you type sits after
the chips and searches as before; with the box empty, **Backspace** removes
the last chip. Blue chips filter, purple chips group, and yellow chips are
saved searches.

**Typing suggests chips.** Type **dav** and the bar offers *Owner: Dave*,
*Search Owner for: dav*, *Search Title for: dav* and so on. Pick one with the
arrow keys and **Enter**, or click it, and it becomes a chip.

**The caret** at the right of the bar opens a panel with three columns:

- **Filters** — the quick filters (**Mine**, **In review**, **Failing**,
  **Suspect**, each with how many it would show), then **Status**, **Type**,
  **Priority**, **Created** and **Updated**, each opening its values (for the
  dates: today, this week, this month, this quarter, this year). On the
  requirements list, each **Type** shows how many artifacts of that type the
  other chips leave. Ticking two
  values of one field shows either. **Custom filter…** builds a condition on
  any field or attribute: *contains* or *is* a value.
- **Group by** — **Status**, **Type**, **Priority**, **Owner** or **Heading**,
  and **Custom group** for any attribute. The table then lists its rows under
  one heading per value; within a group, the rows keep their place in the
  hierarchy, and each group folds on its own. Two groupings combine
  ("Draft · Requirement"). Each grouping is a chip too.
- **Favourites** — your saved searches. **Save current search** names the
  chips and text in the bar; tick **Share with the workspace** to list it for
  everyone in the workspace. Saved searches are kept with your account, so
  they follow you to every device. Choosing one puts it in the bar as a
  single chip; **×** beside one of your own deletes it.

Different filters all apply together: **Status In review** and **Owner
Dave** shows Dave's artifacts in review. The arrow keys move through the
panel and **Escape** closes it. On a phone the chips scroll sideways and the
panel opens as a sheet from the bottom of the screen.

The chips are part of the page's address, so a filtered list can be
bookmarked or shared: whoever opens the link sees the same chips, a saved
search included.

### The list's bar

The list's bar narrows the project you are in, and spans the width of the
list. Its text matches the ref, title, body, type and attributes. It takes
the place of the type tabs: filter by type from **Filters → Type** in the
panel, where each type shows its count. The panel stays inside the window,
however narrow the list.

## Editing, versions & history

Select an artifact to see its details; **Edit** opens the editor.

- The editor's choice fields — **Parent Artifact**, **Type**, **Priority**,
  **Owner**, **How is this verified?**, **Releases** and the project's own
  list attributes — open a list of **every option**, with the current one
  ticked, whatever the field already holds. Move with **↑ ↓**, choose with
  **Enter**, close with **Esc**, or type a letter to jump to it. A list of
  more than eight options has a **search box**: **Parent Artifact** finds an
  artifact by its title or its ref (*REQ-12*) and shows the document's
  structure, indented under its headings. **Type** shows each type's colour
  and what it is for. **Owner** lists the project's **parties** and then its
  **members**; typing narrows the list, and **Use "…"** sets a name that is
  on neither, which an owner may be. **×** clears it.
- A requirement has a **Priority**: **Must**, **Should** or **Could**, the
  value the table, the detail panel and **Group by Priority** show. A
  project that defines its own attribute called *priority* keeps that field
  instead.
- With **Priority from the requirement text** on (**Project settings →
  Quality rules**), Priority follows the requirement's own wording while
  nobody has picked one: *shall* or *must* gives **Must**, *should* gives
  **Should**, *may* or *could* gives **Could**, and the strongest wins. An
  **auto** tag and a line under the field say which word it came from. Pick a
  priority yourself and it stays; if the text later says otherwise, the line
  offers **Use Must** (or Should, or Could) to follow it again. A requirement
  that already has a priority keeps it. Leave the setting off in a project
  that writes every requirement with *shall*, as ISO/IEC/IEEE 29148 does, or
  every requirement would be a Must.
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

The Review Queue is a board of four columns, so the state of the review reads
at a glance:

- **In review** — what is waiting for a decision.
- **Suspect links** — links flagged because an artifact at one end changed;
  **Confirm** on a card vouches that the link still holds.
- **Changes requested** — what you sent back from the board, with your reason.
- **Approved** — what you approved from the board.

The last two are a record of your visit: a decision moves its card across
instead of making it vanish, and they start empty again when the page is
reloaded. Above the board, **All**, **Suspect links** and **In review** narrow
it to those columns. On a phone the columns become a row of tabs at the top,
each with its count, and the board shows one column at a time.

Each card in **In review** carries what you need to judge without opening
anything: the reference and type, how long ago it last changed, the title, the
start of the description, and small previews of the artifact's figures —
pictures as thumbnails, PDFs and CAD files as chips naming them. Any of them
opens the full file; the title and **Open** open the artifact.

Two more buttons sit on every card:

- **Approve** signs the artifact off as it stands. Approving also clears the
  suspect flag on every link that touches it, because you have just vouched
  for the artifact its links were made from.
- **Send back** asks what needs to change before it does anything. The comment
  is required, and it is written in the same composer the History uses —
  \`@name\` reaches a person, \`@@name\` raises them a to-do, \`#REQ-12\`
  cites — and it lands on the artifact's feed as an ordinary note. Then the
  artifact goes back to draft. Nobody gets work returned without being told
  why.

### Deciding several at once

Tick the box on any card, or **All** at the top of the column to take every
card in it, and the column offers two buttons that act on everything
selected: **Approve selected** asks once and signs them all off, **Send back
selected** asks for one reason and posts it on every one of them. Suspect
links work the same way, with **Confirm selected**.

Anything that could not be decided — a refused permission, an artifact
somebody else moved while you were reading — is counted and reported, and
stays in its column. The rest still go through.

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

**History** is the last section of the artifact's detail panel, below its
links and figures. It is the artifact's record: what OpenV wrote down each time the artifact changed, and what people have said about
it, newest first. A new version, a status move, a link added or removed, a figure
replaced, a test result recorded — each leaves an entry, alongside the notes
members write themselves.

Three buttons above the list decide what you are reading:

- **All** — the whole record, changes and comments interleaved.
- **Changes** — only what OpenV recorded. This is the audit trail: who changed
  what, and when.
- **Comments** — only what people wrote. This is the review discussion.

It opens on **Comments**, which is what most people come to it for; the
recorded changes are the backdrop.

Add a comment in the box under the **History** heading. Use it for review discussion
rather than editing the requirement text itself — the text is the requirement,
the comment is what you think of it. Writing one while reading **Changes**
switches you back to **Comments**, so you can see what you just added.

### Archiving and reinstating

**Archive** takes an artifact out of the project and keeps every version. A
link to it (from **Activity**, a notification or a shared link) opens it
read-only, marked **archived**, at its last version; **History** previews
every version with its links. **Reinstate** (editor access) brings it back
as it was, as a new version: reference, content, status and links. Under an
archived heading, it comes back at the top level.

### Read and unread comments

**Needs you**, on Home, lists the comments you have not read. Comments
count as read once you see them here: as soon as the **History** comes into
view, or straight away when you came from a **Needs you** line. A comment
written since you last read the artifact's comments shows **New**.

To keep a comment in **Needs you** for later, click **Mark unread** on it.
That comment, and every comment written after it on the same artifact,
show **Unread** and stay in **Needs you** until you open them again or mark
them read there. Your own comments are never unread to you.

### Tagging inside a comment

A comment can name people and point at things, with the same two-marker
convention the descriptions use.

- **\`@\`** offers the people on the project. Choosing one writes the name
  the mention resolves to, which matters: a handle typed from memory can name
  nobody at all, and there is nothing to see when it does. The person is
  notified.
- **\`@@\`** names someone **and raises a to-do for them** as the comment is
  posted — the comment's first paragraph becomes the to-do, assigned to
  whoever it named and linked back to this artifact. Use it when you already
  know you are asking for something rather than talking. The to-do goes on
  the project's to-do list, not the board (see *Kanban board*, To-dos).
- **\`#\`** and **\`##\`** cite a figure or an artifact, exactly as they do in
  a description, and the citation is a link a reader can follow.

Raising a to-do **after** a comment is posted still works and has not changed:
the **Add to-do** control on a comment that names somebody lets you set the
text, the person and a due date. \`@@\` is the shortcut, not a replacement —
it takes the note's first paragraph as the to-do and no due date.

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

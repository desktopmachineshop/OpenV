// User manual chapter: Traceability links and the matrix.
const content = `
# Traceability links

Links are typed, directional relationships between artifacts. They are what
turns a pile of requirements into a traceable system — and they feed the V&V
coverage rollups and the traceability matrix.

## Link types

| Type | Direction | Meaning |
| --- | --- | --- |
| verifies | test-case → requirement | The test demonstrates the requirement is met |
| validates | test-case → user-need | The test confirms the user need is met |
| satisfies | design-item → requirement | The design fulfills the requirement |
| mitigates | design-item → hazard | The design reduces or eliminates the hazard |
| decomposes-to | requirement → requirement | Parent requirement broken into sub-requirements |
| derives-from | requirement → user-need | The requirement originates from a user need |
| impacts | any → any | A change to one artifact affects the other |
| relates-to | any ↔ any | Loose, non-semantic association |

Direction matters: a *verifies* link always goes **from** the test case **to**
the requirement. When you look at the requirement, the same link is shown with
its inverse label ("verified by").

## Creating links

Links are created from an artifact's editor / link panel:

1. Select the source artifact and open its editor.
2. Pick a **link type** — only types valid for the source artifact's type are
   offered.
3. Pick the **target artifact** — the list is filtered to types the chosen
   link type allows.

Both the app and the server enforce these rules, so an invalid combination
(say, a requirement "verifying" a hazard) is rejected with an explanation.

## Viewing links

On the artifact details:

- **Links from this artifact** — outgoing links, grouped by type.
- **Links to this artifact** — incoming links, shown with inverse labels.

Clicking a linked artifact navigates to it.

## Where links come from automatically

- The **guided wizard** creates links as it materializes drafts:
  user needs *relate to* their persona, requirements *derive from* their need,
  and verification stubs *verify* their requirement.
- **Agents** can propose links; with the default proposal write mode you
  approve them before they land (see *Runs & runners*).

## The Traceability Matrix

**Traceability**, under **Verify** in the project menu, shows one row per requirement with columns
for:

- **Linked needs** — user needs the requirement derives from
- **Design outputs** — design items that satisfy it
- **Test cases + latest result** — verifying tests, each chip showing its
  latest recorded result (pass / fail / blocked / not-run)
- **Linked hazards**

Extras:

- **Baseline selector** — view the matrix as of any baseline (read-only).
- **Quick filter** — type to filter rows across all columns.
- **Export CSV** — download the matrix for spreadsheets or reports.

On a phone the matrix is a coverage list instead of a grid:

- A summary at the top counts the requirements that are **fully traced** —
  a user need, a design output and passing tests — and those failing, with
  a suspect link or with a gap.
- **Needs attention**, **Failing**, **Suspect** and **All** filter the list;
  **Needs attention** puts failing tests first, then suspect links, then
  missing links and tests not yet run.
- Each requirement has a square for **Need**, **Design**, **Test** and
  **Hazard**, holding how many are linked. The square turns green when every
  test passes, red when one fails, amber for a blocked test or a suspect
  link, and is a dashed outline when nothing is linked where something
  should be. A hazard is not required, so an empty one shows a dash.
- Tap a requirement for a sheet that names what needs attention and every
  linked artifact, each test with its latest result, with **Show impact**
  and **Open requirement**. Export CSV is on the desktop view.

## Impact analysis

**Impact**, under **Verify** in the project menu, answers "what does a change
to this requirement touch?" before you make it. Pick the requirement, or open
the page from **Show impact →** on a requirement or a row of the
Traceability Matrix. Only requirements can be picked: the analysis always
starts from one, and flows from it to every artifact its links lead to.

- The result is a tree with the requirement in the middle. **Upstream** —
  what it depends on, its sources and rationale — branches out to the left;
  **Downstream** — what depends on it, what a change here could affect —
  branches out to the right. Each artifact hangs off the one it was reached
  through.
- Each line, and the edge of the box it leads to, is coloured by its link
  type, with a key below the tree, so a *satisfies* reads apart from a
  *verifies* or a *mitigates* at a glance. Select a box to open the artifact.
- **Depth** — **1 level** shows only the artifacts linked directly to the
  requirement; **All levels** follows every link to the end of the chain.
- The walk follows every link, hop after hop. Downstream it reaches the
  sub-requirements, the design items that satisfy them, the test cases that
  verify them and the hazards those design items mitigate; upstream it reaches
  the user needs and parent requirements. An **impacts** link counts as the
  change flowing from its source to its target, and a **relates-to** link,
  having no direction, is followed both ways.
- On a phone the tree opens on the requirement; swipe sideways to follow it.

Run it before changing an approved requirement: the downstream list is the set
of artifacts whose links will turn **suspect** and that may need review again.
`;

export default content;

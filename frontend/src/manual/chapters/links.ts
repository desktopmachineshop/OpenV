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

**Matrix** in the project sidebar shows one row per requirement with columns
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
`;

export default content;

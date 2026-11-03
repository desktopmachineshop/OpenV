// User manual chapter: the project pages Home, Design, Tests and Baselines.
const content = `
# Home, Design, Tests and Baselines

Four project pages bring together what you would otherwise collect from
several others. **Home** and **Design** are under **Define** in the project
menu, **Tests** and **Baselines** under **Verify**. They show what the
project already holds; you edit artifacts, record results and capture
baselines where you always have.

## Home

**Home** is the project at a glance. Along the top are its figures: how many
artifacts it holds, the share of requirements that are **traced** (a test
case verifies them) and **verified**, and how many fail their tests. Beside
the project's name are its last baseline and the number of members.

**Needs you** lists what waits on the team, each with a link to deal with it:

- **approve**: an artifact in review.
- **suspect**: a link whose ends changed after it was made; confirm it or
  remove it on the **Review Queue** page.
- **test run**: a run still in progress, with how many test cases are left.
- **failing**: a test case whose last result is a fail, and the
  requirements it verifies.
- **comment**: a recent comment on an artifact.

The **Audit trail** shows the newest events of the project, who did what to
which artifact; **Full history** opens the **Activity** page. **Projects**
lists your workspace's projects with how far each is traced and verified,
and when agents have proposed changes, a card counts the proposals waiting
and opens the agent runs to review them.

On a phone, Home shows the figures as four tiles, then **Needs you**, the
proposals and the recent activity.

## Design

**Design** shows the project's design items as a tree: an item nested under
another in the **Requirements** view is nested here too. The number beside
an item counts the design items beneath it. Type in **Filter** to find an
item by its reference or title.

Select an item to read it: its description, its **Attributes** as a table,
the requirements it **satisfies** (and anything else it links to, such as
the hazards it mitigates), and **Verified through**: the test cases that
verify those requirements, each with its last result. Test cases verify
requirements rather than design items, so this is the evidence that reaches
the item through them. **Properties** gives its parent, how many items it
contains, its version and when it last changed.

**Edit** opens the item in the **Requirements** view, and **Show in trace**
opens its impact. On a phone the page walks the tree instead: the path to
the item, the item, what it contains and the other items beside it.

## Tests

**Tests** starts with the project's newest test runs as cards. Each bar
shows the run's passes in green, fails in red and blocked results in amber
out of every test case in the project; the rest are still to go. Select a
card to open the run and record results.

Below, **Test cases** lists every test case with its method (automated,
manual or physical), the requirements it **verifies**, its **last result**
and the run that recorded it, and its number of **steps**: the numbered
steps in its text. **Last result: fail, blocked** narrows the list to what
needs attention, and **Method** to one kind of test. The **Runs** tab lists
the newest runs as a table.

On a phone, Tests leads with the run in progress and a button to continue
it, then the recent runs and the test cases failing or blocked.

## Baselines page

**Baselines** lists the project's baselines as a timeline, newest first, with
the **Working copy**, the project as it is now, at the top. Select a
baseline to compare from it; it is marked **from**, and what it is compared
to is marked **to**. Choose what to compare to under **Compare to**: the
working copy or another baseline.

The comparison counts the artifacts **added**, **changed** and **removed**,
and lists every change with the fields that changed. **Text only** narrows
the list to changes of title or text, and **Links** to links added or
removed.

Select a change to read its text word by word: removed words are struck out
in red and added words underlined in green. When you compare to the working
copy, the links of that artifact that are suspect now are named under it.
**Open the full comparison** opens the comparison page with every link.
`;

export default content;

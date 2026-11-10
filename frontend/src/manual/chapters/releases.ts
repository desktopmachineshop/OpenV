// User manual chapter: target releases and the progress against them.
const content = `
# Target releases

A **release** is a milestone you plan requirements for, such as *MVP*,
*V1.0* or *V2.0*. The **Releases** page, under **Verify** in the project
menu, lists a project's releases in order and shows how far each one has
come: how many of its requirements are approved, how many are verified, and
a burn-up of verification over time.

## Planning releases

Choose **Add release** and give the release a name, and if you like a target
date, a status and a description. A new release goes at the end of the list.
Each name is used once per project.

Releases run in the order of the list: use the arrows beside a release to
move it earlier or later. The order matters, because a requirement planned
for one release is in every later release too.

A release's status is **Planned**, **In progress** or **Released**. Marking
a release released records the day it shipped, and its burn-up stops there.

You can't delete a release that requirements still target: move them to
another release first, or clear their release, then delete it.

## Targeting requirements

Set an artifact's releases in its editor, under **Releases**:

- **From** a release: the artifact is introduced in that release and stays
  in every later one.
- With a last release as well, it is in the releases from the first to the
  last, inclusive. Choose **only** to plan it for one release alone, or a
  later release for a capability that is retired after it.
- Leave the first release unset and the artifact takes its parent's
  releases. Set the releases on a heading, and every requirement under it
  is planned for them, unless a requirement sets its own.

The editor says where an inherited range comes from. In a project with no
releases yet, the **Releases** field says so and links to **Create a
release** on the Releases page.

Agents set the same thing with the \`set_release_target\` tool, and read
progress with \`get_release_progress\`.

## Release progress

Select a release to see its progress. The counts cover every requirement in
the release, the ones it introduces and the ones carried over from earlier
releases:

| Count | Meaning |
| --- | --- |
| Requirements | Requirements in the release, with how many are new in it |
| Approved | Requirements whose review status is approved |
| Verified | Requirements whose V&V rollup is *pass* or *verified-manually* |
| Ready | Requirements that are both approved and verified |
| Failing | Requirements whose rollup is *fail*, shown when there are any |

The figures stand beside the release's name, above its burn-up and its
requirements. The rollup is the one the **V&V** page shows, including
verification rolled up from child projects. The bar beside each release in
the list shows its requirements that pass (green, counting those verified
by hand), fail (red) and are blocked (amber); the rest are grey. Below the
list, a line says how many requirements no release targets yet. The release
whose progress is showing is marked with a blue edge in the list.

On a phone the page opens on the current release's progress: its figures as
four tiles, then **Failing or blocked**, the burn-up and its requirements.
The **Releases** tab beside it holds the list, **Add release** and each
release's controls; choosing a release there opens its progress.

The table lists the release's requirements with their review status, their
rollup and the releases they are planned for; *(inherited)* marks a range
that comes from a parent.

## The burn-up

The burn-up draws two lines, day by day, from the day the release was added
(or its oldest requirement, if that is older) to today, or to the day the
release was released:

- **Requirements in the release**: the release's requirements that existed
  on that day.
- **Verified**: those verified by then. A requirement verified by test
  counts from the day all its test cases had passed, read from the test
  result history; one verified by inspection, analysis or demonstration
  counts from the day its current version was saved.

A target date inside the span is marked on the chart. A long span is shown
as one point every few days.

## Releases on the plan

Each release is also a milestone on the **Gantt** page, at its target date,
where its progress is measured against criteria you can set. See
[The plan (Gantt)](/manual/gantt).
`;

export default content;

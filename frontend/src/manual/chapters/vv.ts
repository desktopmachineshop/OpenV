// User manual chapter: V&V dashboard and test runs.
const content = `
# V&V & test runs

The **V&V** view rolls up how well your requirements are verified: coverage,
gaps, test runs, and a downloadable status report.

## Coverage rollup

Every requirement gets a **rollup status** computed from its verification
method and linked test results:

| Rollup | Meaning |
| --- | --- |
| pass | Method is *test* and the latest results for all verifying tests pass |
| fail | At least one verifying test's latest result is a fail |
| blocked | No fails, but a verifying test is blocked |
| unrun | No fails or blocks, but a verifying test has no recorded result yet |
| verified-manually | A non-test method (inspection, analysis, demonstration) marked verified on the requirement |
| uncovered | Method is *test* but no test case verifies the requirement, or a non-test method not yet marked verified |
| method-missing | The requirement has no verification method set |

### What the page shows

The top of the page is a row of figures, each with a small bar:
**Requirements traced** (requirements with a way to be verified — anything
but *uncovered* or *method-missing*), **Verified (pass)**, **Failing**,
**Suspect links** still waiting to be confirmed (live project only) and
**No test case**.

**Coverage by section** groups the requirements by the document headings
they sit under. Each row gives the section's requirement count, how much of
it is traced, a bar of its rollups — pass, fail, blocked and not-run, with
the untested rest as the empty track — and the pass, fail, blocked and gap
counts. The legend above the table carries the totals. Click a section to
list its requirements with their reference, verification method and rollup
chip (and any child-project refinements); **Show all requirements** opens
every section at once.

## Gaps

**Top gaps**, beside the sections, lists the first few artifacts that need
attention, worst first — failing requirements, then those without a
verification method, without a test case, and so on — each with the reason.
**All** opens the full list, grouped by kind of gap (for example
requirements without a verification method, or without any verifying test).
An empty list means full coverage. When agents have proposals waiting, the
card says how many and links to them; otherwise it points you to the agents
that can draft test cases.

**Unverified (demonstration, analysis, inspection)** is the section for
requirements whose method is not *test*: nothing links a test case to them,
so they can never show up as "without a test case". They stay listed until
the requirement is marked verified, which is the same point at which the
coverage rollup turns them from *uncovered* into *verified-manually*.

## Baselines

The **Baseline** selector switches the whole dashboard between the live
project and any captured baseline. Baseline views are read-only historical
snapshots — useful for "where were we at design review?".

## The PDF report

**Export report (PDF)** produces a status report for the current
selection (live or baseline) — coverage rollups, gaps, and test run status in
a shareable document.

## Test runs

A **test run** is one execution campaign: run your test cases and record the
results.

1. Click **New test run** — name it (e.g. "Design verification — rev B"), add
   a description, and optionally pin it to a **baseline**. A project owner can
   still delete that baseline later: the run keeps the reference as part of
   its record, and the run's card shows **Baseline deleted** for it.
2. Every run is a card under **Test runs**, with its status chip, a bar of its
   pass, fail and blocked results against the project's test cases, and how
   many are still to go. A run in progress also appears beside the sections
   as the **active test run**, with **Continue run**; its card has
   **Complete** and **Abort**.
3. Open the run from its card. Every *test-case* artifact in the project gets
   a row with:
   - **Ref** and **Test case** — the test case's reference and title.
   - **Verified by** — *automated*, or *manual (human)* / *physical test*
     tagged in purple, since agents may not run those.
   - **Verifies** — the requirements the test case verifies.
   - **Status** — pass, fail, blocked, or not-run, picked from a chip.
   - **Notes** — free text, edited inline.
   - **Version tested** — which version of the test case the result was
     recorded against.
   - **Evidence** — the capture the result rests on (see below).
   - **Executed at** — timestamp of the recorded result.
4. Record results as you execute. Each change saves immediately.
5. **Complete run** when finished (or **Abort run**). Completed and aborted
   runs become **read-only**.

### Recording on a phone

On a phone the run lists its test cases, each with its reference, what it
verifies, its method and its result. **Record results** (or tapping a test
case) opens the recorder, one test case at a time: the test case's
procedure, four large **Pass**, **Fail**, **Blocked** and **Not run**
buttons, **Notes and measured values**, and **Attach evidence**. **Record
and next** saves the result and moves on; the arrow goes back one. A test
case you leave untouched is skipped, not recorded. On a completed or aborted
run the recorder steps through the results without changing them.

The latest recorded result per test case is what feeds the coverage rollup and
the result chips in the **Matrix** view.

Agents can also record test results — with *proposal* write mode those arrive
as proposals for approval first.

## Evidence for physical and manual tests

An automated test is its own evidence: the run either passed or it did not. A
**physical** or **manual** test case is different — somebody goes to a rig or
inspects the thing, and what they bring back is the only reason to believe the
result. The **Evidence** view is where that lives.

An **evidence bundle** is one capture session, not one result. Record it once:

- **What was done** — "Noise sweep, 90 minutes, all load conditions".
- **What was observed** — a written account. For an inspection or a
  demonstration this may be the whole of the evidence; files are optional.
- **When** and **who** — the capture date, and who carried it out. "Who" is
  free text, because the person on the rig is often not an OpenV user.
- **Conditions** — rig, serial numbers, calibration date, ambient temperature,
  entered one per line as \`name: value\`.
- **Files** — the dataset itself, in any format. Each file's SHA-256 is
  recorded at upload so it can be checked against the record later.

Each bundle gets a citable reference like **EVD-1**, unique in the project and
never reused, so a capture named in a report still means the same thing a year
later.

### Citing a capture

One long run on a rig usually answers **several** test cases at once — a
single noise sweep is the evidence for the idle, half-load and full-load
conditions alike. So a bundle is recorded once and **cited** from each result:
open the run, click the **Evidence** cell on a row, and pick the capture.

Rows whose test case is physical or manual say *evidence needed* until
something is cited, so a result that nothing backs is visible at a glance.

Removing a citation says this result no longer rests on that capture. It does
**not** delete the capture, which other results may still cite. Deleting the
bundle itself is the destructive path, and the confirmation names the results
that will lose their evidence.

### Limits

Each file is capped (200 MB by default) and each workspace has a total
evidence allowance; an upload that would exceed it is refused with a message
saying how much is in use. Ask an administrator if you need more.
`;

export default content;

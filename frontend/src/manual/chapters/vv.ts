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

The dashboard shows summary cards and a stacked bar for the distribution, plus
a **Requirement coverage** table listing every requirement with its method and
rollup chip.

## Gaps

The **Gaps** section groups problems that need attention — e.g. requirements
without a verification method, or without any verifying test. An empty gaps
section means full coverage.

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

**Download V&V report (PDF)** produces a status report for the current
selection (live or baseline) — coverage rollups, gaps, and test run status in
a shareable document.

## Test runs

A **test run** is one execution campaign: run your test cases and record the
results.

1. Click **New Run** — name it (e.g. "Design verification — rev B"), add a
   description, and optionally pin it to a **baseline**.
2. Open the run from the table. Every *test-case* artifact in the project gets
   a row with:
   - **Status** — pass, fail, blocked, or not-run (colored dropdown).
   - **Notes** — free text, edited inline.
   - **Version tested** — which version of the test case the result was
     recorded against.
   - **Evidence** — the capture the result rests on (see below).
   - **Executed at** — timestamp of the recorded result.
3. Record results as you execute. Each change saves immediately.
4. **Complete run** when finished (or **Abort run**). Completed and aborted
   runs become **read-only**.

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

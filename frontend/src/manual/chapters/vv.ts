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
| pass | Latest results for its verifying tests all pass |
| fail | At least one verifying test failed |
| blocked | A verifying test is blocked |
| unrun | Tests exist but haven't been run |
| uncovered | No test case verifies the requirement |
| method-missing | The requirement has no verification method set |

The dashboard shows summary cards and a stacked bar for the distribution, plus
a **Requirement coverage** table listing every requirement with its method and
rollup chip.

## Gaps

The **Gaps** section groups problems that need attention — e.g. requirements
without a verification method, or without any verifying test. An empty gaps
section means full coverage.

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
   - **Executed at** — timestamp of the recorded result.
3. Record results as you execute. Each change saves immediately.
4. **Complete run** when finished (or **Abort run**). Completed and aborted
   runs become **read-only**.

The latest recorded result per test case is what feeds the coverage rollup and
the result chips in the **Matrix** view.

Agents can also record test results — with *proposal* write mode those arrive
as proposals for approval first.
`;

export default content;

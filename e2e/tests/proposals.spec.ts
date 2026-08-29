import { test, expect, Page } from '@playwright/test';
import { makeRunId, makeUser, registerUser, createProject } from './helpers';

// Proposals / agent-run review journey (issue #189).
//
// COVERAGE BOUNDARY — read before extending this file.
//
// A proposal is only ever born from an *agent-run write*: a run whose agent
// has write_mode="proposal" calls the same artifact/link write endpoints a
// human does, and the API (Handler.maybePropose, internal/api/authz.go)
// diverts that write into the pending-review queue instead of applying it.
// The divert hinges on the request carrying an agent-run identity
// (CurrentRun) — a Bearer run token minted only by the worker claim endpoint
// (POST /api/v1/agent-runs/claim, worker-key gated). There is no user-facing
// or test-only endpoint that creates a proposal directly.
//
// So producing a real proposal in CI needs one of:
//   (a) a live runner to claim the queued run and perform the write, or
//   (b) the test itself standing in for the runner — mint a workspace worker
//       key, claim the run, then POST an artifact write with the run token.
//
// The E2E stack (docker-compose.yml) runs only postgres + api + frontend;
// there is no runner service and hosted runners are off (no docker.sock), so
// a proposal-mode run simply sits queued and (a) never happens. Option (b)
// would reimplement the worker claim/run-token handshake inside a suite whose
// whole design is black-box UI journeys (roles/labels/text only, no internal
// coupling) — that couples the test to internal worker protocol and is left
// out deliberately rather than faked. The proposal create/apply/bulk-review
// domain + handler logic is already covered by Go tests:
//   internal/domain/proposals/proposals_test.go
//   internal/api/proposal_appliers_test.go
//   internal/api/proposal_bulk_test.go
//
// What this spec DOES cover live: the human review surface renders and reports
// an honest pending count for a fresh project — the ProposalReviewPanel host
// (AgentRunsPage) that a real proposal would surface on.

const runId = makeRunId();
const user = makeUser('E2E Proposals', runId);
const projectName = `E2E Proposals ${runId}`;

test.describe.configure({ mode: 'serial' });

let page: Page;

test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
});

test.afterAll(async () => {
  await page?.close();
});

test('the proposal review surface renders with an honest empty count', async () => {
  await registerUser(page, user);
  await createProject(page, projectName);

  // The Runs module hosts ProposalReviewPanel; its "Pending approvals" pill
  // reflects the live count of pending proposals for the project.
  await page.getByRole('link', { name: 'Runs', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible();

  // A brand-new project has no agent runs and therefore no proposals: the
  // pill reads zero (the panel itself stays collapsed until there is one).
  await expect(page.getByRole('button', { name: 'Pending approvals (0)' })).toBeVisible();
});

// Full approve-a-proposal journey, enabled only when a runner (or a
// worker-protocol seed) can produce a proposal-mode agent-run write. See the
// COVERAGE BOUNDARY note above for why this cannot run against the runner-less
// CI stack. When a runner is wired into the E2E stack, unskip and flesh out:
//   1. create a proposal-mode agent, launch a run that writes an artifact,
//   2. open Runs -> "Pending approvals (1)", expand the panel,
//   3. Approve the proposal, and assert the proposed artifact now exists.
test.skip('approving an agent proposal applies the proposed write', async () => {
  // Requires an agent-run identity to create the proposal — no runner in CI.
});

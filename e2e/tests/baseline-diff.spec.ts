import { test, expect, Page } from '@playwright/test';
import {
  makeRunId,
  makeUser,
  registerUser,
  createProject,
  openRequirements,
  createRequirement,
  openArtifactForEdit,
} from './helpers';

// Baseline-diff journey (issue #189).
//
// register -> project -> requirement -> capture baseline -> edit the
// requirement's title -> Compare -> the Modified section shows the edited
// artifact with old -> new title.
//
// Zero new backend infra: it drives the shipped Capture Baseline dialog, the
// Compare button, and the /projects/:id/baselines/:baselineId/compare view.
// Additive on any stack — a per-run user and project, no pre-existing data
// touched. Selectors are roles / labels / text only; no data-testids added.

const runId = makeRunId();
const user = makeUser('E2E BaselineDiff', runId);
const projectName = `E2E BaselineDiff ${runId}`;
const oldTitle = `E2E Baseline REQ ${runId}`;
const newTitle = `E2E Baseline REQ (edited) ${runId}`;
const baselineName = `E2E Baseline ${runId}`;

test.describe.configure({ mode: 'serial' });

let page: Page;
let projectId = '';

test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
});

test.afterAll(async () => {
  await page?.close();
});

test('sets up a project with one requirement', async () => {
  await registerUser(page, user);
  projectId = await createProject(page, projectName);
  await openRequirements(page);
  await createRequirement(page, oldTitle, 'The system shall have a comparable baseline.');
});

test('captures a baseline of the current state', async () => {
  await page.getByRole('button', { name: 'Capture Baseline' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('heading', { name: 'Capture baseline' })).toBeVisible();
  await dialog.locator('input').fill(baselineName);
  await dialog.getByRole('button', { name: 'OK' }).click();

  // The new baseline appears in the baseline selector.
  const baselineSelect = page.locator('select[title="Select baseline"]');
  await expect(baselineSelect.locator('option', { hasText: baselineName })).toHaveCount(1);
});

test('edits the requirement title after the baseline', async () => {
  await openArtifactForEdit(page, oldTitle);

  const titleInput = page.locator('#title');
  await expect(titleInput).toHaveValue(oldTitle);
  await titleInput.fill(newTitle);
  await page.getByRole('button', { name: 'Update', exact: true }).click();

  // Back in read mode under the new title.
  await expect(page.getByRole('heading', { name: newTitle }).first()).toBeVisible();
});

test('Compare shows the edit in the Modified section (old -> new title)', async () => {
  // With "Live Project" selected, Compare diffs the newest baseline against
  // live and navigates into the compare view.
  await page.getByRole('button', { name: 'Compare' }).click();

  await expect(page).toHaveURL(new RegExp(`/projects/${projectId}/baselines/[0-9a-f-]{36}/compare`));
  await expect(page.getByRole('heading', { name: 'Compare Baseline' })).toBeVisible();
  // Comparing "from <baseline> to Live Project".
  await expect(page.getByText(baselineName).first()).toBeVisible();

  // The Modified section lists the requirement whose title changed, rendered
  // as "<old title> → <new title>". getByText normalises whitespace, so the
  // single-node assertion binds the struck-through old title to the new one.
  await expect(page.getByText('Modified', { exact: true })).toBeVisible();
  await expect(page.getByText(`${oldTitle} → ${newTitle}`)).toBeVisible();
  // The changed-field badge names the field that moved.
  await expect(page.getByText('title', { exact: true })).toBeVisible();
});

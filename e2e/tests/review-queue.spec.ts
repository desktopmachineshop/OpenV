import { test, expect, Page } from '@playwright/test';
import {
  makeRunId,
  makeUser,
  registerUser,
  createProject,
  openRequirements,
  createRequirement,
  openArtifactForEdit,
  openModule,
} from './helpers';

// Review-queue journey (issue #189).
//
// register -> project -> two requirements -> link (decomposes-to) ->
// submit one for review -> edit the other's title (a content change that
// flags every link it touches suspect) -> open the Review Queue -> assert the
// suspect link and the in-review artifact both appear -> Confirm clears the
// suspect link.
//
// Uses the shipped status flow + the content-edit suspect rule (a title/body
// change marks an artifact's links suspect); no new backend infra. Additive on
// any stack — a per-run user and project. Selectors are roles / labels / text
// only; no data-testids added.

const runId = makeRunId();
const user = makeUser('E2E ReviewQueue', runId);
const projectName = `E2E ReviewQueue ${runId}`;
const parentTitle = `E2E Parent REQ ${runId}`;
const childTitle = `E2E Child REQ ${runId}`;
const childTitleEdited = `E2E Child REQ (edited) ${runId}`;

test.describe.configure({ mode: 'serial' });

let page: Page;
let projectId = '';

test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
});

test.afterAll(async () => {
  await page?.close();
});

test('sets up two requirements linked decomposes-to', async () => {
  await registerUser(page, user);
  projectId = await createProject(page, projectName);
  await openRequirements(page);
  await createRequirement(page, parentTitle, 'A high-level requirement broken into parts.');
  await createRequirement(page, childTitle, 'A sub-requirement of the parent.');

  // Link parent --decomposes-to--> child via the edit-mode link panel.
  await openArtifactForEdit(page, parentTitle);
  const linkCard = page
    .locator('.card')
    .filter({ has: page.getByRole('heading', { name: 'Manage Links (Edit Mode)' }) })
    .last();
  await linkCard.getByRole('button', { name: '+ Add Link' }).click();
  await linkCard.locator('select').selectOption('decomposes-to');
  await linkCard.getByPlaceholder('Type to search by title or UID...').fill(childTitle);
  await linkCard.getByText(childTitle, { exact: true }).click();
  await linkCard.getByRole('button', { name: 'Create', exact: true }).click();
  await page.getByRole('button', { name: 'Update', exact: true }).click();

  // Read mode: the outgoing link group is visible on the parent.
  await expect(page.getByRole('heading', { name: parentTitle }).first()).toBeVisible();
  await expect(page.getByText('decomposes to (1)')).toBeVisible();
});

test('submits the parent for review', async () => {
  await page.getByText(parentTitle, { exact: true }).first().click();
  await expect(page.getByRole('heading', { name: parentTitle }).first()).toBeVisible();
  await expect(page.locator('span[title="Review status: Draft"]')).toBeVisible();
  await page.getByRole('button', { name: 'Submit for review' }).click();
  await expect(page.locator('span[title="Review status: In review"]')).toBeVisible();
});

test('editing the child flags the link suspect', async () => {
  await openArtifactForEdit(page, childTitle);
  const titleInput = page.locator('#title');
  await expect(titleInput).toHaveValue(childTitle);
  await titleInput.fill(childTitleEdited);
  await page.getByRole('button', { name: 'Update', exact: true }).click();
  await expect(page.getByRole('heading', { name: childTitleEdited }).first()).toBeVisible();
});

test('Review Queue shows the suspect link and the in-review artifact', async () => {
  await openModule(page, 'Verify', 'Review Queue');
  await expect(page.getByRole('heading', { name: 'Review Queue' })).toBeVisible();

  // Suspect-links section: the decomposes-to link from parent to the (edited)
  // child is flagged because the child changed. The row shows both endpoints.
  const suspectSection = page.locator('section').filter({ hasText: 'Suspect links' });
  await expect(suspectSection.getByText(parentTitle, { exact: true })).toBeVisible();
  await expect(suspectSection.getByText(childTitleEdited, { exact: true })).toBeVisible();

  // In-review section: the parent we submitted is listed.
  const inReviewSection = page.locator('section').filter({ hasText: 'In review' });
  await expect(inReviewSection.getByRole('link', { name: parentTitle })).toBeVisible();
});

test('Confirm clears the suspect link', async () => {
  const suspectSection = page.locator('section').filter({ hasText: 'Suspect links' });
  // The per-row Confirm button (not "Confirm selected").
  await suspectSection.getByRole('button', { name: 'Confirm', exact: true }).click();

  // The only suspect link is gone: the section shows its trusted empty state.
  await expect(
    suspectSection.getByText('No suspect links. Traceability is trusted.')
  ).toBeVisible();
  await expect(suspectSection.getByText(childTitleEdited, { exact: true })).toBeHidden();
});

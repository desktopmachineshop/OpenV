import { test, expect, Page } from '@playwright/test';

// OpenV E2E smoke journey (issue #135).
//
// One serial journey through the core loop, in a single shared page:
//   register -> personal workspace -> project -> two artifacts -> link
//   (derives-from) -> baseline capture -> status draft->in_review ->
//   global search -> JSON export.
//
// Selector strategy: roles, labels, placeholders, titles, and visible text
// only — no data-testids were needed. Everything the suite creates is scoped
// to a per-run unique id, so the pack is additive on any stack (including a
// developer's live one) and never touches pre-existing data.

// Unique per worker process. A serial-group retry runs in a fresh worker, so
// a retried journey registers a brand-new user instead of colliding with the
// half-finished one.
const runId = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`;

const user = {
  name: `E2E Smoke ${runId}`,
  email: `e2e-${runId}@example.com`,
  password: `e2e-pass-${runId}`,
};
const projectName = `E2E Project ${runId}`;
const reqTitle = `E2E REQ ${runId}`;
const needTitle = `E2E NEED ${runId}`;
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

test('registers a fresh user and lands in the personal workspace', async () => {
  await page.goto('/login');
  await page.getByRole('button', { name: 'Create a new account' }).click();
  await page.getByPlaceholder('Your name').fill(user.name);
  await page.getByPlaceholder('Email').fill(user.email);
  await page.getByPlaceholder('Password (min 8 characters)').fill(user.password);
  await page.getByRole('button', { name: 'Create account' }).click();

  await expect(page).toHaveURL(/\/projects$/);
  // Registration auto-provisions "<name>'s Space" as a personal workspace;
  // the org switcher shows it with a "personal" pill.
  await expect(page.getByText(`${user.name}'s Space`)).toBeVisible();
  await expect(page.getByText('personal', { exact: true })).toBeVisible();
});

test('creates a project', async () => {
  // A fresh account renders the button twice (empty-state placeholder and
  // the footer action row) — either one opens the same form.
  await page.getByRole('button', { name: '+ New Project' }).first().click();
  await page.getByLabel('Project Name *').fill(projectName);
  await page.getByLabel('Description').fill('Created by the E2E smoke pack. Safe to delete.');
  await page.getByRole('button', { name: 'Create Project' }).click();

  // Creation navigates straight into the project shell.
  await expect(page).toHaveURL(/\/projects\/[0-9a-f-]{36}$/);
  projectId = page.url().split('/').pop()!;
  // The project name shows in the shell (sidebar and/or overview).
  await expect(page.getByText(projectName).first()).toBeVisible();
});

test('creates a requirement and a user need', async () => {
  // exact: the overview page also renders "View in Requirements →" links.
  await page.getByRole('link', { name: 'Requirements', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Requirements' })).toBeVisible();

  // Requirement via the New Artifact form (default type is "requirement").
  await page.getByRole('button', { name: '+ New Artifact' }).click();
  await page.locator('#type').selectOption('requirement');
  await page.locator('#title').fill(reqTitle);
  await page.locator('#body').fill('The system shall be smoke-testable end to end.');
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByText(reqTitle, { exact: true })).toBeVisible();

  // User need via the same New Artifact form: the type dropdown carries a
  // "user-need" option (the backend catalog and the derives-from/validates
  // link rules require the type), so it is created through the UI like the
  // requirement above.
  await page.getByRole('button', { name: '+ New Artifact' }).click();
  await page.locator('#type').selectOption('user-need');
  await page.locator('#title').fill(needTitle);
  await page.locator('#body').fill('As a maintainer, I need an E2E smoke pack so that regressions surface early.');
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByText(needTitle, { exact: true })).toBeVisible();
});

test('links the requirement to the user need (derives-from)', async () => {
  await page.getByText(reqTitle, { exact: true }).first().click();
  // The read-mode header for the selected artifact.
  await expect(page.getByRole('heading', { name: reqTitle }).first()).toBeVisible();
  await page.getByRole('button', { name: 'Edit', exact: true }).click();

  // The link panel is a .card nested inside the editor .card; both match a
  // has-heading filter, so take the innermost.
  const linkCard = page
    .locator('.card')
    .filter({ has: page.getByRole('heading', { name: 'Manage Links (Edit Mode)' }) })
    .last();
  await linkCard.getByRole('button', { name: '+ Add Link' }).click();
  await linkCard.locator('select').selectOption('derives-from');
  await linkCard.getByPlaceholder('Type to search by title or UID...').fill(needTitle);
  // Pick the matching artifact from the link-target dropdown.
  await linkCard.getByText(needTitle, { exact: true }).click();
  await linkCard.getByRole('button', { name: 'Create', exact: true }).click();

  // Links persist on Update.
  await page.getByRole('button', { name: 'Update', exact: true }).click();

  // Back in read mode: the outgoing link group is visible on the requirement…
  await expect(page.getByRole('heading', { name: reqTitle }).first()).toBeVisible();
  await expect(page.getByText('derives from (1)')).toBeVisible();

  // …and the inverse label shows on the user need WITHOUT any reload:
  // ArtifactDetails fetches live links for the on-screen artifact, so the
  // server-side version bump on the counterpart can no longer hide the
  // fresh incoming link (issue #169 — this used to need a reload-retry
  // workaround here; its absence pins the fix).
  await page.getByText(needTitle, { exact: true }).first().click();
  await expect(page.getByRole('heading', { name: needTitle }).first()).toBeVisible();
  await expect(page.getByText('gives rise to (1)')).toBeVisible();
});

test('captures a baseline via the prompt dialog', async () => {
  await page.getByRole('button', { name: 'Capture Baseline' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('heading', { name: 'Capture baseline' })).toBeVisible();
  await dialog.locator('input').fill(baselineName);
  await dialog.getByRole('button', { name: 'OK' }).click();

  // The new baseline appears in the baseline selector.
  const baselineSelect = page.locator('select[title="Select baseline"]');
  await expect(baselineSelect.locator('option', { hasText: baselineName })).toHaveCount(1);

  // Viewing the baseline is read-only; back to live restores editing.
  await baselineSelect.selectOption({ label: baselineName });
  await expect(page.getByRole('button', { name: '+ New Artifact' })).toBeHidden();
  await baselineSelect.selectOption({ label: 'Live Project' });
  await expect(page.getByRole('button', { name: '+ New Artifact' })).toBeVisible();
});

test('moves the requirement from draft to in_review', async () => {
  await page.getByText(reqTitle, { exact: true }).first().click();
  await expect(page.getByRole('heading', { name: reqTitle }).first()).toBeVisible();
  await expect(page.locator('span[title="Review status: Draft"]')).toBeVisible();

  await page.getByRole('button', { name: 'Submit for review' }).click();

  await expect(page.locator('span[title="Review status: In review"]')).toBeVisible();
  // The state machine now offers the next legal transitions.
  await expect(page.getByRole('button', { name: 'Approve' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Return to draft' })).toBeVisible();
});

test('finds the artifact through global search', async () => {
  const search = page.getByLabel('Search artifacts across projects');
  await search.fill(reqTitle);

  // Search hits render as role=button rows (title + type + snippet).
  await page.getByRole('button', { name: reqTitle }).click();

  // Selecting a hit deep-links into the owning project's requirements view.
  await expect(page).toHaveURL(new RegExp(`/projects/${projectId}/requirements\\?artifact=`));
  await expect(page.getByRole('heading', { name: reqTitle }).first()).toBeVisible();
});

test('exports the project as JSON', async () => {
  await page.goto('/projects');
  const card = page.locator('.project-card').filter({ hasText: projectName });
  await expect(card).toBeVisible();

  const exportResponse = page.waitForResponse(
    (r) => r.url().includes(`/api/v1/projects/${projectId}/export`) && r.url().includes('format=json')
  );
  await card.getByTitle('Export project as JSON (full project, re-importable)').click();

  const response = await exportResponse;
  expect(response.status()).toBe(200);
  expect(response.headers()['content-type']).toContain('application/json');

  // The export is the real project payload: both artifacts and the link.
  const body = JSON.parse((await response.body()).toString('utf-8'));
  const titles = (body.artifacts || []).map((a: { title: string }) => a.title);
  expect(titles).toContain(reqTitle);
  expect(titles).toContain(needTitle);
  const linkTypes = (body.links || []).map((l: { type: string }) => l.type);
  expect(linkTypes).toContain('derives-from');
});

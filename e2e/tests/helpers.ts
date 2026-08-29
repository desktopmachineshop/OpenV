import { Page, expect } from '@playwright/test';

// Shared journey helpers (issue #189).
//
// The E2E pack is additive on any running stack: every spec mints a fresh
// user and project through these helpers, scoped to a per-run unique id, and
// never touches pre-existing data. Selectors are roles / labels / placeholders
// / visible text only — no data-testids — mirroring smoke.spec.ts so the two
// evolve together.

// The API origin the frontend's browser talks to (dev-compose default). Only
// used where a spec needs the app's own API with the browser session (e.g.
// minting an interview invite the same way the UI does); UI traffic otherwise
// flows through the rendered app.
export const apiURL = process.env.API_URL || 'http://localhost:8080';

export interface TestUser {
  name: string;
  email: string;
  password: string;
}

/** Per-run unique id. A serial-group retry runs in a fresh worker, so a retried
 *  journey regenerates this and registers a brand-new user rather than
 *  colliding with the half-finished one. */
export function makeRunId(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`;
}

/** A fresh user whose email/password are unique to this run. */
export function makeUser(prefix: string, runId: string): TestUser {
  return {
    name: `${prefix} ${runId}`,
    email: `e2e-${runId}@example.com`,
    password: `e2e-pass-${runId}`,
  };
}

/** Register a brand-new account and land in the personal workspace. */
export async function registerUser(page: Page, user: TestUser): Promise<void> {
  await page.goto('/login');
  await page.getByRole('button', { name: 'Create a new account' }).click();
  await page.getByPlaceholder('Your name').fill(user.name);
  await page.getByPlaceholder('Email').fill(user.email);
  await page.getByPlaceholder('Password (min 8 characters)').fill(user.password);
  await page.getByRole('button', { name: 'Create account' }).click();

  await expect(page).toHaveURL(/\/projects$/);
  // Registration auto-provisions "<name>'s Space" as a personal workspace.
  await expect(page.getByText(`${user.name}'s Space`)).toBeVisible();
}

/** Create a project and return its id; leaves the browser in the project shell. */
export async function createProject(page: Page, name: string): Promise<string> {
  // A fresh account renders the button twice (empty-state placeholder and the
  // footer action row) — either one opens the same form.
  await page.getByRole('button', { name: '+ New Project' }).first().click();
  await page.getByLabel('Project Name *').fill(name);
  await page.getByLabel('Description').fill('Created by the E2E journey pack. Safe to delete.');
  await page.getByRole('button', { name: 'Create Project' }).click();

  await expect(page).toHaveURL(/\/projects\/[0-9a-f-]{36}$/);
  return page.url().split('/').pop()!;
}

/** Open the Requirements module from the project shell. */
export async function openRequirements(page: Page): Promise<void> {
  // exact: the overview page also renders "View in Requirements →" links.
  await page.getByRole('link', { name: 'Requirements', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Requirements' })).toBeVisible();
}

/** Create a requirement artifact via the New Artifact form (Requirements view). */
export async function createRequirement(page: Page, title: string, body: string): Promise<void> {
  await page.getByRole('button', { name: '+ New Artifact' }).click();
  await page.locator('#type').selectOption('requirement');
  await page.locator('#title').fill(title);
  await page.locator('#body').fill(body);
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByText(title, { exact: true })).toBeVisible();
}

/** Open an artifact by its (exact) title and enter edit mode. */
export async function openArtifactForEdit(page: Page, title: string): Promise<void> {
  await page.getByText(title, { exact: true }).first().click();
  await expect(page.getByRole('heading', { name: title }).first()).toBeVisible();
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
}

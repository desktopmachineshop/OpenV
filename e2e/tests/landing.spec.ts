import { test, expect } from '@playwright/test';

// The public front page (REQ-111..REQ-114, TC-51). Runs signed out in the
// desktop projects: every test here gets Playwright's default fresh context,
// so no session from another spec can leak in and turn "/" into a redirect.

test('a visitor at / sees the landing page, not the sign-in card', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Requirements, traceability and V&V evidence');
  await expect(page.getByRole('link', { name: 'Sign in' }).first()).toBeVisible();
});

test('the pricing section states the three tiers, the limits and the data promise', async ({ page }) => {
  await page.goto('/pricing');
  const pricing = page.locator('#pricing');
  await expect(pricing).toBeInViewport();
  for (const name of ['Single User', 'Business Lite', 'Business', 'Enterprise', 'Self-hosted', 'Charities and open source']) {
    await expect(pricing.getByRole('heading', { name, exact: true })).toBeVisible();
  }
  await expect(pricing.getByText('Coming soon', { exact: true })).toHaveCount(3);
  await expect(pricing.getByText('Workspaces created during the alpha keep every tier', { exact: false })).toBeVisible();
  await expect(pricing.getByText('Hosted runner: 2 GB memory, 1 CPU.')).toBeVisible();
  await expect(page.getByText('Your data is never behind the paywall.', { exact: false })).toBeVisible();
  await expect(page.getByText('ReqIF interchange')).toBeVisible();
});

test('the calls to action reach sign-in and registration', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('link', { name: 'Create free account' }).first().click();
  await expect(page).toHaveURL(/\/login\?mode=register$/);
  await expect(page.getByPlaceholder('Your name')).toBeVisible();
  await page.getByRole('link', { name: 'About OpenV' }).click();
  await expect(page).toHaveURL(/\/$/);
  await page.getByRole('link', { name: 'Sign in' }).first().click();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByPlaceholder('Your name')).toBeHidden();
});

test('the manual is readable without a session', async ({ page }) => {
  await page.goto('/manual');
  await expect(page).toHaveURL(/\/manual/);
  await expect(page.getByRole('heading', { name: 'Getting started' }).first()).toBeVisible();
});

// The storefront's other pages (REQ-152): each is readable signed out, from
// the header, and the demo page carries the five recordings.
test('the site pages are reachable signed out and the demos page carries five videos', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('navigation', { name: 'Site' }).getByRole('link', { name: 'How it works' }).click();
  await expect(page).toHaveURL(/\/how-it-works$/);
  await expect(page.getByRole('heading', { level: 1 })).toContainText('One graph from need to evidence');
  await expect(page.getByRole('img', { name: /artifact graph/i })).toBeVisible();

  await page.getByRole('navigation', { name: 'Site' }).getByRole('link', { name: 'Demos' }).click();
  await expect(page).toHaveURL(/\/demos$/);
  await expect(page.locator('video')).toHaveCount(5);

  await page.getByRole('navigation', { name: 'Site' }).getByRole('link', { name: 'FAQ' }).click();
  await expect(page).toHaveURL(/\/faq$/);
  await expect(page.getByRole('heading', { name: 'Security, on every tier' })).toBeVisible();

  await page.getByRole('navigation', { name: 'Site' }).getByRole('link', { name: 'Open source' }).click();
  await expect(page).toHaveURL(/\/open-source$/);
  await expect(page.getByRole('heading', { name: 'Published projects' })).toBeVisible();

  for (const path of ['/customers', '/white-papers']) {
    await page.goto(path);
    await expect(page).toHaveURL(new RegExp(`${path}$`));
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  }
});

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
  await expect(pricing.getByRole('heading', { name: 'Hosted alpha' })).toBeVisible();
  await expect(pricing.getByRole('heading', { name: 'Self-hosted' })).toBeVisible();
  await expect(pricing.getByRole('heading', { name: 'Charities and open source' })).toBeVisible();
  await expect(pricing.getByText('Hosted runner: 2 GB memory, 1 CPU.')).toBeVisible();
  await expect(page.getByText('If hosted OpenV ever charges, your data will not be behind the paywall.', { exact: false })).toBeVisible();
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

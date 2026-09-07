import { test, expect, Page } from '@playwright/test';
import { createProject, makeRunId, makeUser, registerUser } from './helpers';

// Mobile journey (docs/plans/mobile-support.md, REQ-101..REQ-105).
//
// Runs only in the phone-emulating projects (see playwright.config.ts): a
// 390px-wide viewport with touch. It exercises the shape the app takes on a
// phone — the drawer navigation, the stacked requirements module and the
// no-sideways-scroll contract — around the same core loop as the smoke
// journey: register -> project -> requirement -> read it -> notes.

const runId = makeRunId();
const user = makeUser('E2E Mobile', runId);
const projectName = `E2E Mobile Project ${runId}`;
const reqTitle = `E2E Mobile REQ ${runId}`;

test.describe.configure({ mode: 'serial' });

let page: Page;

/** The page must never be wider than the phone: horizontal scrolling on a
 *  phone means something has a fixed desktop width. */
async function expectNoHorizontalScroll(p: Page): Promise<void> {
  const overflow = await p.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    innerWidth: window.innerWidth,
  }));
  expect(overflow.scrollWidth, `page scrolls sideways: ${JSON.stringify(overflow)}`).toBeLessThanOrEqual(
    overflow.innerWidth
  );
}

test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
});

test.afterAll(async () => {
  await page?.close();
});

test('registers on a phone-sized screen without sideways scrolling', async () => {
  await page.goto('/login');
  await expectNoHorizontalScroll(page);
  await registerUser(page, user);
  await expectNoHorizontalScroll(page);
});

test('creates a project and lands in the mobile shell', async () => {
  await createProject(page, projectName);
  // The compact shell: a top bar with the menu button, no permanent sidebar.
  const menuButton = page.getByRole('button', { name: 'Project menu' });
  await expect(menuButton).toBeVisible();
  await expect(page.getByRole('link', { name: 'Requirements', exact: true })).toBeHidden();
  await expectNoHorizontalScroll(page);
});

test('navigates through the drawer, which closes on arrival', async () => {
  await page.getByRole('button', { name: 'Project menu' }).click();
  const drawer = page.getByRole('complementary', { name: 'Project navigation' });
  await expect(drawer).toBeVisible();
  const define = drawer.getByRole('button', { name: 'Define', exact: true });
  if ((await define.getAttribute('aria-expanded')) !== 'true') {
    await define.click();
  }
  await drawer.getByRole('link', { name: 'Requirements', exact: true }).click();

  await expect(page.getByRole('heading', { name: 'Requirements' })).toBeVisible();
  await expect(drawer).toBeHidden();
  await expectNoHorizontalScroll(page);
});

test('the requirements module stacks into tree, document and notes panes', async () => {
  const panes = page.getByRole('tablist', { name: 'Requirements panes' });
  await expect(panes).toBeVisible();
  await expect(panes.getByRole('tab', { name: 'Tree' })).toHaveAttribute('aria-selected', 'true');

  // The tree pane holds the create form.
  await page.getByRole('button', { name: '+ New Artifact' }).click();
  await page.locator('#type').selectOption('requirement');
  await page.locator('#title').fill(reqTitle);
  await page.locator('#body').fill('The system shall be usable on a phone.');
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByText(reqTitle, { exact: true })).toBeVisible();
  await expectNoHorizontalScroll(page);

  // Tapping an artifact moves to its document, which is what the tap meant.
  await page.getByText(reqTitle, { exact: true }).first().click();
  await expect(panes.getByRole('tab', { name: 'Document' })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('heading', { name: reqTitle }).first()).toBeVisible();
  await expectNoHorizontalScroll(page);

  // Notes are a pane of their own rather than a third column.
  await panes.getByRole('tab', { name: 'Notes' }).click();
  await expect(page.getByRole('heading', { name: /Notes/ })).toBeVisible();
  await expectNoHorizontalScroll(page);

  // And back to the tree, which kept its state.
  await panes.getByRole('tab', { name: 'Tree' }).click();
  await expect(page.getByText(reqTitle, { exact: true }).first()).toBeVisible();
});

test('the project list and settings pages fit the phone', async () => {
  await page.goto('/projects');
  await expect(page.getByText(projectName).first()).toBeVisible();
  await expectNoHorizontalScroll(page);
});

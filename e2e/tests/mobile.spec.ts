import { test, expect, Page } from '@playwright/test';
import { createProject, expectNoHorizontalScroll, makeRunId, makeUser, registerUser } from './helpers';

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
let projectId = '';

test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
});

test.afterAll(async () => {
  await page?.close();
});

test('the landing page fits a phone', async () => {
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  await expectNoHorizontalScroll(page);
  await page.goto('/pricing');
  await expect(page.locator('#pricing')).toBeInViewport();
  await expectNoHorizontalScroll(page);
});

test('registers on a phone-sized screen without sideways scrolling', async () => {
  await page.goto('/login');
  await expectNoHorizontalScroll(page);
  await registerUser(page, user);
  await expectNoHorizontalScroll(page);
});

test('creates a project and lands in the mobile shell', async () => {
  projectId = await createProject(page, projectName);
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

  // Both settings pages carry a row of six or seven tabs, wider than a
  // phone: the row must scroll inside itself, and its last tab must still
  // be reachable, without the page moving sideways.
  await page.goto(`/projects/${projectId}/settings`);
  const projectTabs = page.getByRole('tablist');
  await expect(projectTabs).toBeVisible();
  await expectNoHorizontalScroll(page);
  await projectTabs.getByRole('button').last().click();
  await expectNoHorizontalScroll(page);

  await page.goto('/org/settings');
  const orgTabs = page.getByRole('tablist');
  await expect(orgTabs).toBeVisible();
  await expectNoHorizontalScroll(page);
  await orgTabs.getByRole('button', { name: 'Usage' }).click();
  await expect(orgTabs.getByRole('button', { name: 'Usage' })).toBeInViewport();
  await expectNoHorizontalScroll(page);
});

test('the personal settings sheet shows every theme option', async () => {
  // The example from the maintainer's phone: the System / Light / Dark
  // control was clipped after "System". The settings open from the drawer
  // footer and fill the screen; every option must be inside the viewport.
  await page.goto(`/projects/${projectId}`);
  await page.getByRole('button', { name: 'Project menu' }).click();
  const drawer = page.getByRole('complementary', { name: 'Project navigation' });
  await drawer.getByRole('button', { name: /^Account menu/ }).click();
  await drawer.getByRole('button', { name: 'Settings', exact: true }).click();

  const theme = page.getByRole('radiogroup', { name: 'Theme' });
  await expect(theme).toBeVisible();
  for (const option of ['System', 'Light', 'Dark']) {
    // The clipped control scored 0 here; 0.95 leaves room for WebKit's
    // sub-pixel rounding of a fully visible button.
    await expect(theme.getByRole('radio', { name: option })).toBeInViewport({ ratio: 0.95 });
  }
  await expectNoHorizontalScroll(page);
  await page.getByRole('button', { name: 'Close settings' }).click();
  await expect(theme).toBeHidden();
});

test('the workspace switcher menu stays on the screen', async () => {
  await page.goto('/projects');
  // The trigger's accessible name is the workspace's own name; its title
  // is the stable handle.
  await page.getByTitle('Switch workspace').click();
  const menu = page.getByRole('menu');
  await expect(menu).toBeVisible();
  await expect(menu).toBeInViewport({ ratio: 1 });
  await expect(menu.getByRole('button', { name: 'Workspace settings' })).toBeVisible();
  await expectNoHorizontalScroll(page);
  // A press outside the menu closes it.
  await page.mouse.click(200, 600);
  await expect(menu).toBeHidden();
});

test('the agent pages open their side panes as sheets', async () => {
  // Agents: the list takes the width; the editor is a full-screen sheet.
  await page.goto(`/projects/${projectId}/agents`);
  await expect(page.getByRole('heading', { name: 'Agents' })).toBeVisible();
  await expectNoHorizontalScroll(page);
  await page.getByRole('button', { name: 'New agent' }).click();
  const sheet = page.getByRole('dialog', { name: 'New agent' });
  await expect(sheet).toBeVisible();
  await expectNoHorizontalScroll(page);
  await sheet.getByRole('button', { name: 'Close editor' }).click();
  await expect(sheet).toBeHidden();

  // Runs and automations: the tables keep to the phone's width.
  await page.goto(`/projects/${projectId}/agent-runs`);
  await expect(page.getByRole('columnheader', { name: 'Agent' })).toBeVisible();
  await expect(page.getByRole('columnheader', { name: 'Tokens' })).toHaveCount(0);
  await expectNoHorizontalScroll(page);
  await page.goto(`/projects/${projectId}/automations`);
  await expect(page.getByRole('columnheader', { name: 'Name' })).toBeVisible();
  await expectNoHorizontalScroll(page);
});

test('runner control and the relayed sign-in work at phone size', async () => {
  // REQ-108. The workspace Runners tab is the runner surface every member
  // reaches; its controls must be on the screen and tappable.
  await page.goto('/org/settings?tab=worker-keys');
  await expect(page.getByRole('heading', { name: 'My personal runner' })).toBeVisible();
  await expectNoHorizontalScroll(page);
  // Reachable and tappable, not necessarily above the fold: the hosted-runner
  // card sits above this one, so on a short phone (the iPhone 13's 664 px
  // against the Pixel 5's 727 px) the control starts below the first screen.
  // What REQ-108 asks is that a thumb can reach it and hit it.
  const connector = page.getByRole('button', { name: 'Set up Agent Connector' });
  await connector.scrollIntoViewIfNeeded();
  await expect(connector).toBeInViewport();
  expect((await connector.boundingBox())!.height).toBeGreaterThanOrEqual(44);

  // The cloud runner lease lives in personal settings, beside the agent
  // sign-ins it feeds. A deployment with no pool renders no card at all, so
  // the lease control is asserted only where it exists; "Start a cloud
  // runner" on an unleased pool is the same full-width control as "Extend
  // the lease" and "End now" once a lease is held.
  await page.goto(`/projects/${projectId}`);
  await page.getByRole('button', { name: 'Project menu' }).click();
  const drawer = page.getByRole('complementary', { name: 'Project navigation' });
  await drawer.getByRole('button', { name: /^Account menu/ }).click();
  await drawer.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Agent sign-ins' })).toBeVisible();
  await expectNoHorizontalScroll(page);

  const lease = page.getByRole('button', { name: /Start a cloud runner|Extend the lease/ }).first();
  if (await lease.count()) {
    await lease.scrollIntoViewIfNeeded();
    await expect(lease).toBeInViewport();
    expect((await lease.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  }

  // The relay: a real sign-in needs a runner to claim it, so the worker's
  // side is answered from the test. What is under test is this browser's
  // half — the link a phone can follow, and the field that takes a paste
  // from a password manager and submits with the keyboard's send key.
  await page.route('**/api/v1/provider-logins**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: 'e2e-login',
        provider: 'claude-code',
        target: 'user',
        status: 'awaiting_code',
        auth_url: 'https://example.com/oauth/authorize?client_id=openv',
        detail: 'Open the sign-in link, authorize, then paste the code you are given back here.',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      }),
    })
  );
  await page.getByRole('button', { name: 'Connect', exact: true }).first().click();

  const codeField = page.getByRole('textbox', { name: 'Authorization code' });
  await expect(codeField).toBeVisible();
  await expect(codeField).toHaveAttribute('enterkeyhint', 'send');
  await expect(codeField).toHaveAttribute('inputmode', 'text');
  await expect(codeField).toHaveAttribute('autocapitalize', 'none');
  expect((await codeField.boundingBox())!.height).toBeGreaterThanOrEqual(44);

  const authLink = page.getByRole('link', { name: /Open sign-in page/ });
  await expect(authLink).toBeInViewport();
  await expect(authLink).toHaveAttribute('target', '_blank');
  await expectNoHorizontalScroll(page);
  await page.unroute('**/api/v1/provider-logins**');
  await page.getByRole('button', { name: 'Close settings' }).click();
});

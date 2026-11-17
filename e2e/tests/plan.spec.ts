import { test, expect, Page } from '@playwright/test';
import { makeRunId, makeUser, registerUser, createProject } from './helpers';

// The plan journey: one member plans a project from an empty Gantt to a
// baseline and takes it away.
//
//   register -> project -> two tasks with dates -> a milestone
//   -> a baseline saved from the Gantt, which is the project's
//   -> the Baselines page says where the plan stood
//   -> All projects, a level down -> the workspace's Plan page
//   -> the download wizard takes the plan along
//
// It is the journey the unit tests cannot make: every step goes through the
// real server, so the plan the browser draws is the one the board, the
// scheduler and the baseline repository agree on. Like smoke.spec.ts it is
// additive on any stack (its own user and project) and selects by role,
// label, placeholder, title and visible text.

const runId = makeRunId();
const user = makeUser('E2E Planner', runId);
const projectName = `E2E Plan ${runId}`;
const baselineName = `Gate ${runId}`;

test.describe.configure({ mode: 'serial' });

let page: Page;
let projectId = '';

test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
});

test.afterAll(async () => {
  await page?.close();
});

/** A bar on the chart, by the task it draws. */
const bar = (title: string) => page.getByRole('button', { name: new RegExp(`^${title}: `) });

const addTask = async (title: string, start: string, due: string) => {
  await page.getByRole('button', { name: 'Add task', exact: true }).click();
  const form = page.getByRole('form', { name: 'Add task' });
  await form.getByPlaceholder('What needs doing').fill(title);
  await form.getByLabel('Start').fill(start);
  await form.getByLabel('Due').fill(due);
  await form.getByRole('button', { name: 'Add task', exact: true }).click();
  await expect(bar(title)).toBeVisible();
};

test('opens an empty plan', async () => {
  await registerUser(page, user);
  projectId = await createProject(page, projectName);
  await page.goto(`/projects/${projectId}/gantt`);
  await expect(page.getByRole('button', { name: 'Add task', exact: true })).toBeVisible();
});

test('adds two dated tasks, which the chart draws', async () => {
  await addTask('Build the rig', '2031-03-03', '2031-03-14');
  await addTask('Paint the rig', '2031-03-17', '2031-03-21');
  await expect(bar('Build the rig')).toBeVisible();
});

test('adds a milestone with a date', async () => {
  await page.getByRole('button', { name: 'Add milestone', exact: true }).click();
  const form = page.getByRole('form', { name: 'Add milestone' });
  await form.getByPlaceholder('Definition complete').fill('Rig accepted');
  await form.locator('input[type="date"]').fill('2031-03-28');
  await form.getByRole('button', { name: 'Add milestone', exact: true }).click();
  await expect(page.getByText('Rig accepted').first()).toBeVisible();
});

test('saves a baseline from the Gantt, which is a baseline of the project', async () => {
  await page.getByRole('button', { name: /^Plan tools/ }).click();
  await page.getByRole('menuitem', { name: 'Tracking' }).click();
  const tracking = page.getByRole('dialog');
  await tracking.getByLabel('Baseline name').fill(baselineName);
  await tracking.getByRole('button', { name: 'Save a baseline' }).click();
  // It is listed as the project's, with the way to the page that compares it.
  await expect(tracking.getByText(baselineName)).toBeVisible();
  await expect(tracking.getByRole('link', { name: 'Project baseline' })).toHaveAttribute('href', `/projects/${projectId}/baselines`);
  await tracking.getByRole('link', { name: 'Project baseline' }).click();
  await expect(page).toHaveURL(new RegExp(`/projects/${projectId}/baselines$`));
});

test('the Baselines page says where the plan stood', async () => {
  await expect(page.getByRole('heading', { name: `${baselineName} → Working copy` })).toBeVisible();
  const plan = page.getByRole('region', { name: 'Plan' });
  await expect(plan).toBeVisible();
  // Nothing has moved since the baseline: both sides read the same.
  await expect(plan).toContainText('0/2');
  // The plan ends with its last milestone.
  await expect(plan).toContainText('28 Mar 2031');
  await expect(plan).toContainText('No milestone moved.');
  await expect(plan).toContainText('Every task reads as it did.');
});

test('a task that slips shows on the comparison', async () => {
  await page.goto(`/projects/${projectId}/gantt`);
  // Paint runs two weeks over, past the milestone: its bar opens its panel, where the date is typed.
  await bar('Paint the rig').click();
  const panel = page.getByRole('complementary', { name: 'Task Paint the rig' });
  await panel.getByLabel('Due', { exact: true }).fill('2031-04-04');
  await panel.getByRole('button', { name: 'Save schedule' }).click();
  await expect(bar('Paint the rig')).toHaveAccessibleName(/4 Apr/);

  await page.goto(`/projects/${projectId}/baselines`);
  const plan = page.getByRole('region', { name: 'Plan' });
  // The plan now ends a week after the baseline said it would.
  await expect(plan).toContainText('28 Mar 2031 → 4 Apr 2031');
  await expect(plan).toContainText('7 days later');
  // And it says which task did it.
  const changed = plan.getByRole('list', { name: 'Tasks that changed' });
  await expect(changed.getByRole('listitem')).toHaveCount(1);
  await expect(changed).toContainText('Paint the rig');
  await expect(changed).toContainText('ends 14 days later (21 Mar 2031 → 4 Apr 2031)');
});

test('All projects shows the project, and a level down', async () => {
  await page.goto(`/projects/${projectId}/gantt`);
  await page.getByLabel('View').selectOption({ label: 'All projects' });
  const line = page.getByRole('link', { name: projectName });
  await expect(line).toBeVisible();
  await expect(page.getByRole('button', { name: /^Build the rig: / })).toBeVisible();
  const level = page.getByLabel('Level of detail');
  await expect(level).toHaveValue('1');
  await level.selectOption('2');
  // Two top-level tasks with no sub-tasks: both are still there a level down.
  await expect(page.getByRole('button', { name: /^Paint the rig: / })).toBeVisible();
});

test('the workspace Plan page lists the project', async () => {
  await page.goto('/org/plan');
  await expect(page.getByRole('link', { name: projectName })).toBeVisible();
  await expect(page.getByRole('button', { name: /^Build the rig: / })).toBeVisible();
});

test('a download takes the plan along as an archive', async () => {
  await page.goto('/projects');
  const card = page.locator('.project-card').filter({ hasText: projectName });
  await card.getByRole('button', { name: `Actions for ${projectName}` }).click();
  await card.getByRole('menuitem', { name: 'Download' }).click();
  const wizard = page.getByRole('dialog');
  await wizard.getByText('JSON data').click();
  await wizard.getByRole('button', { name: 'Next' }).click();
  await wizard.getByLabel('The plan and its tasks').check();
  await expect(wizard.getByText('so this download is a .zip')).toBeVisible();

  const download = page.waitForResponse((r) => r.url().includes(`/api/v1/projects/${projectId}/download/json`));
  await wizard.getByRole('button', { name: 'Download', exact: true }).click();
  const response = await download;
  expect(response.status()).toBe(200);
  expect(response.url()).toContain('plan=1');
  expect(response.headers()['content-type']).toContain('application/zip');
  // A zip names its entries in the clear: the plan is one of them.
  const body = (await response.body()).toString('latin1');
  expect(body).toContain('plan/plan-');
});

import { test, expect, Page } from '@playwright/test';
import {
  createProject,
  createRequirement,
  expectNoHorizontalScroll,
  makeRunId,
  makeUser,
  openRequirements,
  registerUser,
} from './helpers';

// Desktop layout journey (docs/plans/mobile-support.md §8, "Desktop pass").
//
// Runs in the chromium project only; the viewport comes from test.use, not
// from a browser project, so one engine covers both ends of the desktop
// range: a 1024 px laptop, where the three-pane requirements module has the
// least room, and a 2560 px display, where a document or form must stop
// growing at the reading measure. The full sweep across ten profiles and
// 48 screens is the audit tool (`npm run audit:desktop`); this spec keeps
// the two contracts it found broken under CI.

const MEASURE = 1100; // --measure in frontend/src/theme.css
const MIN_DOCUMENT = 400; // the document pane's floor on a small laptop

async function journey(page: Page, label: string): Promise<{ projectId: string; reqTitle: string }> {
  const runId = makeRunId();
  const user = makeUser(`E2E Desktop ${label}`, runId);
  const reqTitle = `E2E Desktop REQ ${runId}`;

  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  await expectNoHorizontalScroll(page);

  await registerUser(page, user);
  await expectNoHorizontalScroll(page);

  const projectId = await createProject(page, `E2E Desktop ${label} ${runId}`);
  await expectNoHorizontalScroll(page);

  await openRequirements(page);
  await createRequirement(page, reqTitle, 'The system shall lay out on every desktop display.');
  await page.getByText(reqTitle, { exact: true }).first().click();
  await expect(page.getByRole('heading', { name: reqTitle }).first()).toBeVisible();
  await expectNoHorizontalScroll(page);
  return { projectId, reqTitle };
}

/** The artifact document's reading surface (the measured wrapper around the
 *  details card), as laid out. */
async function documentWidth(page: Page): Promise<number> {
  const box = await page.locator('.measure').first().boundingBox();
  expect(box, 'the artifact document is not on the page').not.toBeNull();
  return box!.width;
}

async function restOfTheShell(page: Page, projectId: string): Promise<void> {
  await page.goto(`/projects/${projectId}/agent-runs`);
  await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible();
  await expectNoHorizontalScroll(page);

  await page.goto(`/projects/${projectId}/settings`);
  await expect(page.getByRole('tablist')).toBeVisible();
  await expectNoHorizontalScroll(page);

  await page.goto('/org/settings');
  await expect(page.getByRole('tablist')).toBeVisible();
  await expectNoHorizontalScroll(page);
}

test.describe('a 1024 px laptop', () => {
  test.use({ viewport: { width: 1024, height: 768 } });

  test('every page fits, and the requirements document keeps its room', async ({ page }) => {
    const { projectId } = await journey(page, '1024');
    // With the tree at its saved width and the notes column present, the
    // document was a sliver at this width; the tree now yields first.
    expect(await documentWidth(page)).toBeGreaterThanOrEqual(MIN_DOCUMENT);
    await restOfTheShell(page, projectId);
  });
});

test.describe('stepping through the document', () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test('the controls and the reading keys walk the project', async ({ page }) => {
    const { reqTitle } = await journey(page, 'stepping');
    const second = `${reqTitle} second`;
    const third = `${reqTitle} third`;
    await createRequirement(page, second, 'The system shall be the second artifact.');
    await createRequirement(page, third, 'The system shall be the third artifact.');

    // Back to the first of the three: nothing before it.
    // The count is asserted exactly: the stepper renders it twice, once
    // visibly and once inside the screen-reader live region that also names
    // the artifact, and a substring match would find both.
    await page.getByText(reqTitle, { exact: true }).first().click();
    await expect(page.getByText('1 of 3', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Previous artifact' })).toBeDisabled();

    await page.getByRole('button', { name: 'Next artifact' }).click();
    await expect(page.getByText('2 of 3', { exact: true })).toBeVisible();
    await expect(page.getByRole('heading', { name: second }).first()).toBeVisible();

    // J and K, from the page rather than from a control.
    await page.keyboard.press('j');
    await expect(page.getByText('3 of 3', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Next artifact' })).toBeDisabled();
    await page.keyboard.press('k');
    await expect(page.getByText('2 of 3', { exact: true })).toBeVisible();

    // The guard that matters most: a j typed into a field is a j.
    await page.getByPlaceholder('Search...').fill('j');
    await expect(page.getByText('2 of 3', { exact: true })).toBeVisible();
    await page.getByPlaceholder('Search...').fill('');

    // The selection survives a reload, and so does its place in the document.
    await page.reload();
    await expect(page.getByText('2 of 3', { exact: true })).toBeVisible();
    await expectNoHorizontalScroll(page);
  });
});

test.describe('a 2560 px display', () => {
  test.use({ viewport: { width: 2560, height: 1440 } });

  test('every page fits, and the document reads at the measure', async ({ page }) => {
    const { projectId } = await journey(page, '2560');
    expect(await documentWidth(page)).toBeLessThanOrEqual(MEASURE);
    // The editor's single-line fields stop well short of the measure too.
    await page.getByRole('button', { name: 'Edit', exact: true }).click();
    const title = await page.locator('#title').boundingBox();
    expect(title?.width ?? 0).toBeLessThanOrEqual(720);
    await page.getByRole('button', { name: 'Cancel', exact: true }).click();
    await restOfTheShell(page, projectId);
  });
});

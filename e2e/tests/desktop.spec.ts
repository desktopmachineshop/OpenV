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

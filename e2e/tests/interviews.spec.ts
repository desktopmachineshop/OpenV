import { test, expect, Page, BrowserContext } from '@playwright/test';
import { makeRunId, makeUser, registerUser, createProject, apiURL } from './helpers';

// Interviews journey (issue #189).
//
// register -> project -> create an interview -> mint its public invite link ->
// open /interview/:token in a SECOND, unauthenticated browser context ->
// participant enters a name and sends a message -> back in the authed context,
// the session and its transcript show the participant's message.
//
// Boundary: sending a participant message records it synchronously and, if the
// interview has an interviewer agent, enqueues one priority LLM "turn" run.
// That AI reply needs an agent RUNNER, which CI does not have — so this journey
// asserts up to the participant message being recorded (in the participant's
// own chat AND in the reviewer's transcript) and deliberately does NOT wait on
// an assistant reply. See internal/api/suite_handlers.go PublicInterviewMessage
// (AppendMessage before launchInterviewTurn) and launchInterviewTurn.
//
// The invite is minted through the app's own API with the browser session —
// the same call the "Copy invite link" button makes — because that button
// copies to the clipboard, which is not reliably readable under test. Additive
// on any stack; a per-run user and project. No data-testids added.

const runId = makeRunId();
const user = makeUser('E2E Interviews', runId);
const projectName = `E2E Interviews ${runId}`;
const interviewName = `E2E Interview ${runId}`;
const participantName = `Participant ${runId}`;
const participantMessage = `E2E interview answer ${runId}`;

const baseURL = process.env.BASE_URL || 'http://localhost:3000';

test.describe.configure({ mode: 'serial' });

let page: Page;
let projectId = '';
let invitePath = '';

test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
});

test.afterAll(async () => {
  await page?.close();
});

test('creates an interview and mints a public invite link', async () => {
  await registerUser(page, user);
  projectId = await createProject(page, projectName);

  await page.getByRole('link', { name: 'Interviews', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Interviews' })).toBeVisible();

  await page.getByRole('button', { name: '+ New interview' }).click();
  // The create form's labels are not associated to their inputs, so target by
  // placeholder.
  await page.getByPlaceholder('e.g. Machinist onboarding feedback').fill(interviewName);
  await page
    .getByPlaceholder('What should the interview find out? Topics, tone, questions to cover…')
    .fill('Find out how the participant uses the product.');
  await page.getByRole('button', { name: 'Create interview' }).click();
  await expect(page.getByText(interviewName, { exact: true })).toBeVisible();

  // Mint the invite the same way the UI does, but read the path directly (the
  // button copies to the clipboard, which is not reliably readable in tests).
  const listRes = await page.request.get(`${apiURL}/api/v1/projects/${projectId}/interviews`);
  expect(listRes.status(), 'list interviews').toBe(200);
  const interviews = (await listRes.json()) as Array<{ id: string; name: string }>;
  const interview = interviews.find((iv) => iv.name === interviewName);
  expect(interview, 'created interview present in the list').toBeTruthy();

  const inviteRes = await page.request.post(`${apiURL}/api/v1/interviews/${interview!.id}/invites`, {
    data: { invitee_label: '' },
  });
  expect(inviteRes.status(), 'mint invite').toBeLessThan(300);
  const invite = (await inviteRes.json()) as { path: string; token: string };
  expect(invite.path, 'invite path points at the public interview page').toContain('/interview/');
  invitePath = invite.path;
});

test('a participant (no auth) opens the link and sends a message', async ({ browser }) => {
  // A brand-new context with no cookies: the public interview page is reached
  // with no app session, exactly as a real invitee would.
  const guestContext: BrowserContext = await browser.newContext({ baseURL });
  const guest: Page = await guestContext.newPage();
  try {
    await guest.goto(invitePath);

    // Name gate first.
    await expect(guest.getByRole('heading', { name: interviewName })).toBeVisible();
    await guest.getByPlaceholder('Your name').fill(participantName);
    await guest.getByRole('button', { name: 'Start interview' }).click();

    // Chat composer.
    await guest.getByPlaceholder('Type your answer… (Ctrl+Enter to send)').fill(participantMessage);
    await guest.getByRole('button', { name: 'Send' }).click();

    // The participant's own message renders in their transcript. We stop here:
    // the assistant reply needs a runner CI does not have (see file header).
    // .first(): the message can momentarily render twice (local echo + SSE
    // replay) before dedup settles.
    await expect(guest.getByText(participantMessage, { exact: true }).first()).toBeVisible();
  } finally {
    await guestContext.close();
  }
});

test('the reviewer sees the session and the participant message', async () => {
  // Reload the interviews view in the authed context and open the interview.
  await page.goto(`/projects/${projectId}/interviews`);
  await expect(page.getByRole('heading', { name: 'Interviews' })).toBeVisible();
  await page.getByText(interviewName, { exact: true }).click();

  // A session for the participant now exists under the interview. It is
  // located by its "active" status chip rather than by the participant name:
  // the typed name is NOT currently persisted onto the session (see the
  // known-bug annotation below), so the row reads "Anonymous participant".
  const sessionRow = page.getByText('active', { exact: true });
  await expect(sessionRow).toBeVisible();

  // Open the session transcript and confirm the participant message is recorded
  // — the durable signal the journey verifies. .first(): a message can briefly
  // render twice before SSE-dedup settles.
  await sessionRow.click();
  await expect(page.getByText(participantMessage, { exact: true }).first()).toBeVisible();

  test.info().annotations.push({
    type: 'known-bug',
    description:
      'Participant name from the interview name-gate is not stored on the session. ' +
      'PublicInterviewStream (internal/api/suite_handlers.go) opens the session with an ' +
      'empty name when the SSE stream connects — before the name gate is submitted — so ' +
      'the first message’s StartOrResumeSession resumes the anonymous session and the ' +
      'real name is dropped. Every session shows "Anonymous participant".',
  });
});

// Drive the app through one video's steps and record it (site_media.py runs
// this; README.md says how the pieces fit).
//
//   node record.mjs <video.json> <out-dir> <base-url> [--check]
//
// Signs in with SITE_MEDIA_EMAIL and SITE_MEDIA_PASSWORD, then runs each
// step's actions. A step lasts at least as long as its narration (the
// "seconds" site_media.py wrote beside each step), so the picture waits for
// the voice. Writes session.webm, raw-timeline.json (each step's start and
// end in the recording) and shots/<name>.png for every "shot" action.
// --check runs the actions without recording or waiting, to find a step
// whose button has moved.
import { createRequire } from 'node:module';
import fs from 'node:fs';
import path from 'node:path';

const require = createRequire(import.meta.url);
const { chromium, devices } = require('playwright');

const [specPath, outDir, baseURL, flag] = process.argv.slice(2);
const check = flag === '--check';
const spec = JSON.parse(fs.readFileSync(specPath, 'utf8'));
const phone = spec.viewport === 'phone';
const size = phone ? { width: 390, height: 844 } : { width: 1280, height: 720 };
const video = phone ? { width: 780, height: 1688 } : { width: 1920, height: 1080 };

fs.mkdirSync(path.join(outDir, 'shots'), { recursive: true });
const browser = await chromium.launch();
const context = await browser.newContext({
  ...(phone ? devices['iPhone 13'] : {}),
  viewport: size,
  deviceScaleFactor: 2,
  baseURL,
  colorScheme: 'light',
  reducedMotion: 'no-preference',
  ...(check ? {} : { recordVideo: { dir: outDir, size: video } }),
});
context.setDefaultTimeout(15000);

const login = await context.request.post('/api/v1/auth/login', {
  data: { email: process.env.SITE_MEDIA_EMAIL, password: process.env.SITE_MEDIA_PASSWORD },
});
if (!login.ok()) throw new Error(`sign-in failed: ${login.status()}`);

const page = await context.newPage();
const started = Date.now();
const now = () => (Date.now() - started) / 1000;

// One action is [name, ...arguments]; selectors are Playwright's.
const actions = {
  goto: (to) => page.goto(to, { waitUntil: 'networkidle' }),
  click: (sel) => page.locator(sel).first().click(),
  dblclick: (sel) => page.locator(sel).first().dblclick(),
  hover: (sel) => page.locator(sel).first().hover(),
  fill: (sel, text) => page.locator(sel).first().fill(text),
  type: (sel, text) => page.locator(sel).first().pressSequentially(text, { delay: check ? 0 : 45 }),
  press: (key) => page.keyboard.press(key),
  select: (sel, value) => page.locator(sel).first().selectOption(value),
  scroll: (sel) => page.locator(sel).first().scrollIntoViewIfNeeded(),
  wheel: (dy) => page.mouse.wheel(0, dy),
  see: (sel) => page.locator(sel).first().waitFor({ state: 'visible' }),
  wait: (ms) => (check ? null : page.waitForTimeout(ms)),
  shot: (name) => page.screenshot({ path: path.join(outDir, 'shots', `${name}.png`) }),
};

const timeline = [];
let failed = null;
for (const [index, step] of spec.steps.entries()) {
  const start = now();
  try {
    for (const [name, ...args] of step.do ?? []) {
      if (!actions[name]) throw new Error(`unknown action ${name}`);
      await actions[name](...args);
    }
  } catch (err) {
    failed = `step ${index + 1} (${step.id}): ${String(err.message).split('\n')[0]}`;
    await page.screenshot({ path: path.join(outDir, 'shots', `FAILED-${step.id}.png`) }).catch(() => {});
    break;
  }
  if (!check) {
    const least = (step.seconds ?? 0) + (step.pause ?? 0.6);
    const left = least - (now() - start);
    if (left > 0) await page.waitForTimeout(left * 1000);
  }
  timeline.push({ id: step.id, start, end: now() });
}

const recorded = check ? null : await page.video().path();
await context.close();
await browser.close();
if (recorded) fs.renameSync(recorded, path.join(outDir, 'session.webm'));
fs.writeFileSync(path.join(outDir, 'raw-timeline.json'), JSON.stringify(timeline, null, 1));
if (failed) {
  console.error(failed);
  process.exit(1);
}
console.log(`${check ? 'checked' : 'recorded'} ${timeline.length} steps of ${spec.title}`);

import { defineConfig, devices } from '@playwright/test';

// E2E smoke pack configuration.
//
// The suite runs against an ALREADY RUNNING OpenV stack — it does not boot
// one. Point it at the frontend with BASE_URL (default http://localhost:3000);
// the frontend's own build decides which API origin the browser talks to
// (http://localhost:8080 for the dev compose stack).
//
// Run it:
//   cd e2e && npm ci && npx playwright install --with-deps chromium webkit && npx playwright test
//
// Without Node on the host, run it in the official Playwright image (version
// must match the pinned @playwright/test version) with host networking so
// localhost:3000/8080 reach the compose stack:
//   docker run --rm --network host -v "$PWD/e2e":/work -w /work \
//     -e BASE_URL=http://localhost:3000 \
//     mcr.microsoft.com/playwright:v1.57.0-jammy \
//     bash -c "npm ci && npx playwright test"
//
// The tests are a single user journey (register -> project -> artifacts ->
// link -> baseline -> status -> search -> export) executed serially in one
// browser page; every run registers a fresh user so it is purely additive to
// whatever data the target stack holds.
export default defineConfig({
  testDir: './tests',
  // The journey is serial and shares one page: a single worker, no shuffling.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  // Serial suites retry as a whole group; a retry lands in a fresh worker,
  // which regenerates the run-unique user, so retries stay additive too.
  retries: process.env.CI ? 1 : 0,
  timeout: 60_000,
  expect: { timeout: 15_000 },
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.BASE_URL || 'http://localhost:3000',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  // Two engines: Chromium for the bulk of the desktop audience and WebKit
  // because Safari is the browser on every iPhone and iPad, and it is the
  // one that rejects cross-site cookies (docs/plans/mobile-support.md).
  // Each project runs the journey in its own worker with its own user.
  //
  // The phone projects run only the mobile journey (mobile.spec.ts), which
  // asserts the shape the app takes below the tablet breakpoint; the desktop
  // projects skip it for the same reason.
  projects: [
    {
      name: 'chromium',
      testIgnore: /mobile\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
    {
      // The core journey only: enough to catch an engine-specific break in
      // sign-in, navigation or editing without doubling the suite's time.
      name: 'webkit',
      testMatch: /smoke\.spec\.ts/,
      use: { ...devices['Desktop Safari'] },
    },
    {
      // Safari on an iPhone: the WebKit engine, a 390px viewport and touch.
      name: 'iphone',
      testMatch: /mobile\.spec\.ts/,
      use: { ...devices['iPhone 13'] },
    },
    {
      // Chrome on Android.
      name: 'android',
      testMatch: /mobile\.spec\.ts/,
      use: { ...devices['Pixel 5'] },
    },
  ],
});

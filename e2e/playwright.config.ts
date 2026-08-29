import { defineConfig, devices } from '@playwright/test';

// E2E smoke pack configuration.
//
// The suite runs against an ALREADY RUNNING OpenV stack — it does not boot
// one. Point it at the frontend with BASE_URL (default http://localhost:3000);
// the frontend's own build decides which API origin the browser talks to
// (http://localhost:8080 for the dev compose stack).
//
// Run it:
//   cd e2e && npm ci && npx playwright install --with-deps chromium && npx playwright test
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
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});

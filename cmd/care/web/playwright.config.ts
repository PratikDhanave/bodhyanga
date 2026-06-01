import { defineConfig, devices } from '@playwright/test';

// End-to-end browser tests for the Bodh clinician console. Each test boots
// the real cmd/care binary (rule-based diagnostician — deterministic, no
// API key required), navigates the served SPA, and asserts on what the
// user sees plus what the backend records.
//
// First run on this machine:
//   npm run test:e2e:install        # downloads Chromium (~200 MB)
// Then:
//   npm run test:e2e
//
// Inside Bodh:
//   make e2e                         # equivalent, from repo root
//
// Set BODH_E2E_REUSE_SERVER=1 to use an already-running cmd/care instead
// of spawning a fresh one; useful when iterating on tests locally.
export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false, // shared bus state — keep deterministic
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: [['html', { open: 'never' }], ['line']],
  use: {
    baseURL: 'http://localhost:8088',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    command: 'make care',
    url: 'http://localhost:8088/healthz',
    timeout: 120_000,
    reuseExistingServer: !!process.env.BODH_E2E_REUSE_SERVER,
    stdout: 'pipe',
    stderr: 'pipe',
    cwd: '../../..',
  },
});

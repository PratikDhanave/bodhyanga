import { test, expect, type Page } from '@playwright/test';

// Proves the platform is event-driven end to end: with the HITL reviews page
// open, running a case (which produces a review_request at the end of the
// diagnostic chain) makes a new pending review appear **without any manual
// reload** — the review_submitted event is pushed over the WebSocket and the
// page re-reads itself. This is the acceptance test for "pure event driven".

async function login(page: Page): Promise<void> {
  await page.goto('/ui/login');
  await page.locator('#email').fill('clinician@hospital.org');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(/\/ui\/?$/);
}

test('HITL reviews page updates live when a case produces a review (no reload)', async ({ page }) => {
  await login(page);

  // Open the reviews page — mounting subscribes it to the live event stream.
  await page.locator('aside').getByRole('link', { name: 'HITL reviews', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'HITL reviews', level: 1 })).toBeVisible();

  const rows = page.locator('table tbody tr');
  const before = await rows.count();

  // Run a case out-of-band (no navigation, no reload of the reviews page). The
  // rule diagnostician completes in <1s and the reasoning verifier emits a
  // review_request → Queue.Submit → WS review_submitted.
  const token = await page.evaluate(() => localStorage.getItem('bodhyanga_jwt'));
  const res = await page.request.post('/api/runs', {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
    data: { fixture: 'demo-case-001' },
  });
  expect(res.ok()).toBeTruthy();

  // Without touching the page, the pending-review count must grow.
  await expect
    .poll(async () => rows.count(), { timeout: 15_000, intervals: [250, 500, 1000] })
    .toBeGreaterThan(before);
});

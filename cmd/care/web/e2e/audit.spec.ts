import { test, expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// audit.spec.ts — AuditLog page E2E tests.
//
// Tests cover:
//  1. The audit log page loads and shows an empty-state message when no
//     events have been recorded yet (fresh server start with no cases run).
//  2. Filtering by case_id sends the correct query parameter and the table
//     reflects the filter (either events matching that case or the empty
//     state for an unknown case_id).
//
// Note: the AuditLog component auto-loads on mount, so we wait for the
// either the table or the empty-state text to appear before asserting.
// ---------------------------------------------------------------------------

// Shared auth helper.
async function login(page: import('@playwright/test').Page): Promise<void> {
  await page.goto('/ui/login');
  await page.locator('#email').fill('clinician@hospital.org');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(/\/ui\/?$/);
}

// ---------------------------------------------------------------------------
// Test: audit log page loads with empty state message
// ---------------------------------------------------------------------------
test('audit log page loads with empty state message', async ({ page }) => {
  await login(page);
  await page.goto('/ui/audit');

  await expect(page.getByRole('heading', { name: 'Audit Log', level: 1 })).toBeVisible();

  // The page auto-fetches on mount.  Wait for either:
  //   (a) at least one row in the tbody (events exist from prior runs), or
  //   (b) the "No audit events found." empty-state cell.
  // We accept both because Playwright tests share in-process state.
  const emptyStateCell = page.locator('td', { hasText: 'No audit events found.' });
  const firstRow = page.locator('table tbody tr').first();

  await expect(emptyStateCell.or(firstRow).first()).toBeVisible({ timeout: 10_000 });

  // The filter form must always be rendered regardless of data state.
  await expect(page.locator('input[placeholder*="a3f1b9c2"]')).toBeVisible();
});

// ---------------------------------------------------------------------------
// Test: audit log filters by case_id
// ---------------------------------------------------------------------------
test('audit log filters by case_id', async ({ page }) => {
  await login(page);
  await page.goto('/ui/audit');
  await expect(page.getByRole('heading', { name: 'Audit Log', level: 1 })).toBeVisible();

  // Wait for the initial load to settle.
  const table = page.locator('table');
  await expect(table).toBeVisible({ timeout: 10_000 });

  // Enter a case_id that is very unlikely to match any audit event — an
  // unknown case causes the backend to return an empty page, which puts the
  // component into the "No audit events found." state.
  const caseIdInput = page.locator('input[placeholder*="a3f1b9c2"]');
  await expect(caseIdInput).toBeVisible();
  await caseIdInput.fill('00000000');

  // Click Apply to trigger the filtered fetch.
  await page.getByRole('button', { name: 'Apply' }).click();

  // The table must show the empty state for this unknown case_id.
  const emptyCell = page.locator('td', { hasText: 'No audit events found.' });
  await expect(emptyCell).toBeVisible({ timeout: 10_000 });

  // The filter input must still hold the value we typed.
  await expect(caseIdInput).toHaveValue('00000000');

  // Clicking Clear must reset the input.
  await page.getByRole('button', { name: 'Clear' }).click();
  await expect(caseIdInput).toHaveValue('');
});

// ---------------------------------------------------------------------------
// Test: recorded events render a valid timestamp and agent (regression)
//
// Guards the field-name mismatch bug where AuditLog read `ts`/`agent` while the
// API emits `at`/`agent_id`, so every populated row showed "Invalid Date" and a
// blank agent. The empty-state tests above could not catch it (no rows to
// render), so this test first runs a case to record events, then asserts the
// first row's timestamp and agent columns are populated.
// ---------------------------------------------------------------------------
test('audit rows render a valid timestamp and agent', async ({ page }) => {
  await login(page);

  // Populate audit events by running a case through the case runner.
  await page.goto('/ui/runs');
  await page.getByRole('button', { name: /Run case/i }).click();
  await page.waitForURL(/\/ui\/runs\/.+/, { timeout: 15_000 }).catch(() => {});
  await page.waitForTimeout(1500);

  await page.goto('/ui/audit');
  await expect(page.getByRole('heading', { name: 'Audit Log', level: 1 })).toBeVisible();

  const firstRow = page.locator('table tbody tr').first();
  await expect(firstRow).toBeVisible({ timeout: 10_000 });

  // Never render "Invalid Date" (the symptom of the ts/at mismatch).
  await expect(page.getByText('Invalid Date')).toHaveCount(0);

  // Timestamp column contains a real date (has digits), agent column is non-empty.
  const ts = (await firstRow.locator('td').first().innerText()).trim();
  expect(ts).toMatch(/\d/);
  const agent = (await firstRow.locator('td').nth(1).innerText()).trim();
  expect(agent).toMatch(/\w/);
});

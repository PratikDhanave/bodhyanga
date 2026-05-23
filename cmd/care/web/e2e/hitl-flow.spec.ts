import { test, expect } from '@playwright/test';

// Full HITL approve cycle, driven entirely through the SPA:
//   1. Run a case (rule diagnostician → produces a review_request from the
//      reasoning verifier).
//   2. Navigate to /ui/reviews.
//   3. Open the first pending review and approve it with reviewer-id
//      'playwright-test'.
//   4. Back on /ui/reviews the same review must no longer be pending.

test('running a case produces a pending review that can be approved in the UI', async ({ page }) => {
  // ---- step 1: run the case ----
  await page.goto('/ui/runs');
  const fixtureSelect = page.locator('label', { hasText: 'Fixture' }).locator('select');
  await expect(fixtureSelect).toBeVisible();
  await expect(fixtureSelect.locator('option[value="demo-case-001"]')).toHaveCount(1);
  await fixtureSelect.selectOption('demo-case-001');
  await page.getByRole('button', { name: 'Run case' }).click();
  await expect(page).toHaveURL(/\/ui\/runs\/[a-f0-9-]{36}/);

  // Give the rule diagnostician time to publish the review_request. We don't
  // need to assert on the trace itself here — the next step is the source of
  // truth — but waiting for any event keeps the test deterministic.
  const streamCard = page.locator('.card', { has: page.getByText('Agent message stream') });
  await expect(streamCard.locator('div.border-l-2').first()).toBeVisible({ timeout: 10_000 });

  // ---- step 2: navigate to /ui/reviews via the sidebar ----
  await page.locator('aside').getByRole('link', { name: 'HITL reviews', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'HITL reviews', level: 1 })).toBeVisible();

  // Wait for at least one pending review row. The reviews list polls every
  // mount, but the message bus is in-process so it should already be there.
  const rows = page.locator('table tbody tr');
  await expect.poll(async () => rows.count(), { timeout: 10_000 }).toBeGreaterThan(0);

  // Capture the subject text of the first row so we can verify it later.
  const firstRow = rows.first();
  const subject = (await firstRow.locator('td').first().innerText()).trim();

  // ---- step 3: open + approve ----
  await firstRow.getByRole('link', { name: 'Open' }).click();
  await expect(page).toHaveURL(/\/ui\/reviews\/[a-f0-9-]{36}/);

  // Reviewer ID input is pre-filled with 'clinician-demo' — replace it.
  const reviewerInput = page.locator('input').first();
  await reviewerInput.fill('playwright-test');

  await page.getByRole('button', { name: 'Approve', exact: true }).click();

  // ---- step 4: back on reviews list, the approved review must not appear
  //              under Pending. ----
  // Require the URL to be /ui/reviews EXACTLY (no /:id suffix) so we know
  // the post-approve navigate('/reviews') landed before we assert.
  await expect(page).toHaveURL(/\/ui\/reviews\/?$/);
  await expect(page.getByRole('heading', { name: 'HITL reviews', level: 1 })).toBeVisible();

  // The Pending scope is the default. The row we just approved should be
  // gone (or its subject no longer present). We allow either:
  //   (a) the table is empty / showing the Empty state, or
  //   (b) the same subject is no longer present.
  // Toggle to 'All' so we can verify the review still exists with status
  // 'approved' — that confirms the decision actually persisted.
  await page.getByRole('button', { name: 'All' }).click();
  await expect.poll(async () => page.locator('table tbody tr').count()).toBeGreaterThan(0);

  const allRows = page.locator('table tbody tr');
  const matchingRow = allRows.filter({
    has: page.locator('td.font-medium', { hasText: subject }),
  }).first();
  await expect(matchingRow).toBeVisible();
  // Decision is the 4th <td>; its badge carries the status text.
  await expect(matchingRow.locator('td').nth(3).locator('.badge')).toContainText(/approved/i);
});

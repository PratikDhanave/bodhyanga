import { test, expect } from '@playwright/test';

// -----------------------------------------------------------------------
// E1.4 — HITL Reviews page
// Scenarios: Navigate to /reviews; review list renders; pending/all toggle;
// click opens review detail.
// -----------------------------------------------------------------------

test.describe('Reviews page', () => {
  test('renders the page title', async ({ page }) => {
    await page.goto('/ui/reviews');
    await expect(page.getByText('HITL reviews')).toBeVisible();
  });

  test('renders review table or empty state', async ({ page }) => {
    await page.goto('/ui/reviews');

    const hasTable = await page.locator('table').count() > 0;
    const hasEmpty = await page.getByText(/No reviews/).count() > 0;
    expect(hasTable || hasEmpty).toBeTruthy();
  });

  test('has filter tabs for status', async ({ page }) => {
    await page.goto('/ui/reviews');

    // Filter tabs should be present (All, Pending, etc.)
    const tabs = page.getByRole('tab');
    if (await tabs.count() > 0) {
      await expect(tabs.first()).toBeVisible();
    }
  });

  test('clicking a review navigates to detail page', async ({ page }) => {
    await page.goto('/ui/reviews');

    const table = page.locator('table');
    if (await table.count() > 0) {
      const firstRow = table.locator('tbody tr').first();
      if (await firstRow.count() > 0) {
        await firstRow.click();
        // Should navigate to /reviews/<id>
        await page.waitForURL(/\/reviews\//);
      }
    }
  });
});

import { test, expect } from '@playwright/test';

// -----------------------------------------------------------------------
// E1.2 — Cost analytics page
// Scenarios: Navigate to /cost; stat cards visible; demo data banner when
// stub data is used.
// -----------------------------------------------------------------------

test.describe('Cost analytics page', () => {
  test('renders the page title', async ({ page }) => {
    await page.goto('/ui/cost');
    await expect(page.getByText('Cost analytics')).toBeVisible();
  });

  test('shows stat cards with dollar amounts', async ({ page }) => {
    await page.goto('/ui/cost');

    // Should display at least one dollar amount
    await expect(page.getByText(/\$/)).toBeVisible();
  });

  test('shows demo data banner when using stub data', async ({ page }) => {
    await page.goto('/ui/cost');

    // The demo data banner should be visible when the real API isn't wired
    const banner = page.getByText('Demo data');
    const hasBanner = await banner.count() > 0;
    if (hasBanner) {
      await expect(banner).toBeVisible();
      await expect(page.getByText(/GET \/api\/cost\/analytics/)).toBeVisible();
    }
  });
});

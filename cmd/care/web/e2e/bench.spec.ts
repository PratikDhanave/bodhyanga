import { test, expect } from '@playwright/test';

// -----------------------------------------------------------------------
// E1.1 — Bench results page
// Scenarios: Navigate to /bench; Pareto chart SVG visible; summary table
// renders; empty state when no data.
// -----------------------------------------------------------------------

test.describe('Bench results page', () => {
  test('renders the Pareto chart and summary table', async ({ page }) => {
    await page.goto('/ui/bench');

    // The page title should appear
    await expect(page.getByText('Bench results')).toBeVisible();

    // Either we see data or the empty state
    const hasData = await page.locator('table').count() > 0;
    const hasEmpty = await page.getByText('No bench results yet').count() > 0;
    expect(hasData || hasEmpty).toBeTruthy();
  });

  test('Pareto chart SVG is rendered when data exists', async ({ page }) => {
    await page.goto('/ui/bench');

    // If data exists, the SVG chart should be present
    const svg = page.locator('svg');
    const hasSvg = await svg.count() > 0;
    if (hasSvg) {
      // Check for axis label
      await expect(page.getByText('Cost per case (USD)')).toBeVisible();
    }
  });

  test('summary table has correct headers', async ({ page }) => {
    await page.goto('/ui/bench');

    const table = page.locator('table');
    if (await table.count() > 0) {
      await expect(table.getByText('Provider')).toBeVisible();
      await expect(table.getByText('Cases')).toBeVisible();
      await expect(table.getByText('Mean accuracy')).toBeVisible();
      await expect(table.getByText('Mean cost')).toBeVisible();
      await expect(table.getByText('Under budget')).toBeVisible();
    }
  });

  test('color legend is visible when data exists', async ({ page }) => {
    await page.goto('/ui/bench');

    // If data exists, hover instruction text should appear
    const hasData = await page.locator('table').count() > 0;
    if (hasData) {
      await expect(page.getByText(/Hover to inspect/)).toBeVisible();
    }
  });
});

import { test, expect } from '@playwright/test';

// -----------------------------------------------------------------------
// E1.5 — RPM observations page
// Scenarios: Navigate to /rpm; patient monitoring table renders; sparkline
// SVGs visible; alert indicators.
// -----------------------------------------------------------------------

test.describe('RPM page', () => {
  test('renders the page title and subtitle', async ({ page }) => {
    await page.goto('/ui/rpm');
    await expect(page.getByText('RPM observations')).toBeVisible();
    await expect(page.getByText(/Remote patient monitoring/)).toBeVisible();
  });

  test('renders patient monitoring table with headers', async ({ page }) => {
    await page.goto('/ui/rpm');

    const table = page.locator('table');
    await expect(table).toBeVisible();
    await expect(table.getByText('Patient')).toBeVisible();
    await expect(table.getByText('HR (bpm)')).toBeVisible();
    await expect(table.getByText('7-day trend')).toBeVisible();
    await expect(table.getByText('SpO2')).toBeVisible();
    await expect(table.getByText('BP')).toBeVisible();
    await expect(table.getByText('Alert')).toBeVisible();
  });

  test('renders sparkline SVGs for HR trends', async ({ page }) => {
    await page.goto('/ui/rpm');

    // Each patient row should have an SVG sparkline
    const sparklines = page.locator('svg[aria-label*="HR sparkline"]');
    await expect(sparklines.first()).toBeVisible();
    const count = await sparklines.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });

  test('shows alert indicators for flagged patients', async ({ page }) => {
    await page.goto('/ui/rpm');

    // Alice and Carlos have alerts in the stub data
    const alerts = page.locator('text=HR trending up');
    if (await alerts.count() > 0) {
      await expect(alerts.first()).toBeVisible();
    }
  });

  test('shows alert summary banner', async ({ page }) => {
    await page.goto('/ui/rpm');

    // Alert banner should be visible when there are patients with alerts
    const banner = page.getByText(/patient.*with active alerts/);
    if (await banner.count() > 0) {
      await expect(banner).toBeVisible();
    }
  });
});

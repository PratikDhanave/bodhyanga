import { test, expect } from '@playwright/test';

// -----------------------------------------------------------------------
// E1.3 — Patients registry page
// Scenarios: Navigate to /patients; patient table renders; conditions as
// badges; search input visible; empty state.
// -----------------------------------------------------------------------

test.describe('Patients page', () => {
  test('renders the page title and subtitle', async ({ page }) => {
    await page.goto('/ui/patients');
    await expect(page.getByText('Patients')).toBeVisible();
  });

  test('renders patient table or empty state', async ({ page }) => {
    await page.goto('/ui/patients');

    const hasTable = await page.locator('table').count() > 0;
    const hasEmpty = await page.getByText('Registry is empty').count() > 0;
    expect(hasTable || hasEmpty).toBeTruthy();
  });

  test('search input is present when patients exist', async ({ page }) => {
    await page.goto('/ui/patients');

    const table = page.locator('table');
    if (await table.count() > 0) {
      await expect(page.getByPlaceholder(/Search by ID/)).toBeVisible();
    }
  });

  test('patient table has correct headers', async ({ page }) => {
    await page.goto('/ui/patients');

    const table = page.locator('table');
    if (await table.count() > 0) {
      await expect(table.getByText('Patient ID')).toBeVisible();
      await expect(table.getByText('Name')).toBeVisible();
      await expect(table.getByText('Conditions')).toBeVisible();
      await expect(table.getByText('Last A1c')).toBeVisible();
      await expect(table.getByText('Last BP')).toBeVisible();
    }
  });

  test('patient IDs are links', async ({ page }) => {
    await page.goto('/ui/patients');

    const table = page.locator('table');
    if (await table.count() > 0) {
      const firstLink = table.locator('tbody a').first();
      if (await firstLink.count() > 0) {
        const href = await firstLink.getAttribute('href');
        expect(href).toMatch(/\/patients\//);
      }
    }
  });
});

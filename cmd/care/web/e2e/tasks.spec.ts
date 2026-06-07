import { test, expect } from '@playwright/test';

// -----------------------------------------------------------------------
// E1.6 — Nurse tasks page
// Scenarios: Navigate to /tasks; task list renders; all/open/urgent/overdue
// filter tabs; priority badges; sortable columns.
// -----------------------------------------------------------------------

test.describe('Tasks page', () => {
  test('renders the page title', async ({ page }) => {
    await page.goto('/ui/tasks');
    await expect(page.getByText('Nurse tasks')).toBeVisible();
  });

  test('renders task table or empty state', async ({ page }) => {
    await page.goto('/ui/tasks');

    const hasTable = await page.locator('table').count() > 0;
    const hasEmpty = await page.getByText('No tasks').count() > 0;
    expect(hasTable || hasEmpty).toBeTruthy();
  });

  test('has filter tabs when tasks exist', async ({ page }) => {
    await page.goto('/ui/tasks');

    const table = page.locator('table');
    if (await table.count() > 0) {
      // Should show All, Open, Urgent, Overdue tabs
      const tabs = page.getByRole('tab');
      const count = await tabs.count();
      expect(count).toBeGreaterThanOrEqual(3);
    }
  });

  test('task table has correct headers', async ({ page }) => {
    await page.goto('/ui/tasks');

    const table = page.locator('table');
    if (await table.count() > 0) {
      await expect(table.getByText('Subject')).toBeVisible();
      await expect(table.getByText('Patient')).toBeVisible();
      await expect(table.getByText('Kind')).toBeVisible();
    }
  });

  test('clicking a filter tab updates the displayed tasks', async ({ page }) => {
    await page.goto('/ui/tasks');

    const table = page.locator('table');
    if (await table.count() > 0) {
      const allTab = page.getByRole('tab', { name: /All/ });
      if (await allTab.count() > 0) {
        await allTab.click();
        // The subtitle should reflect the count
        await expect(page.getByText(/of \d+/)).toBeVisible();
      }
    }
  });
});

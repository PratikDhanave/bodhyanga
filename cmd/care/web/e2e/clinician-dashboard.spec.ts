import { test, expect, type Page } from '@playwright/test';

async function login(page: Page): Promise<void> {
  await page.goto('/ui/login');
  await page.locator('#email').fill('clinician');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(/\/ui\/?$/);
}

test.describe('Clinician Dashboard', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await page.goto('/ui/dashboard');
  });

  test('page loads with heading', async ({ page }) => {
    await expect(
      page.getByRole('heading', { name: /clinician|dashboard/i })
    ).toBeVisible();
  });

  test('shows summary stat cards', async ({ page }) => {
    // Should have at least one stat card with a number
    const cards = page.locator('[class*="card"], [class*="stat"], [class*="summary"]');
    await expect(cards.first()).toBeVisible({ timeout: 10_000 });
  });

  test('shows recent cases table or list', async ({ page }) => {
    // Look for a table or list of cases
    const table = page.locator('table, [role="table"], [class*="case"]');
    await expect(table.first()).toBeVisible({ timeout: 10_000 });
  });

  test('HITL review queue widget is visible', async ({ page }) => {
    // Should show pending reviews section
    const reviewSection = page.locator('text=/review|hitl|pending/i');
    await expect(reviewSection.first()).toBeVisible({ timeout: 10_000 });
  });
});

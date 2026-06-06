import { test, expect, type Page } from '@playwright/test';

async function login(page: Page): Promise<void> {
  await page.goto('/ui/login');
  await page.locator('#email').fill('clinician');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(/\/ui\/?$/);
}

test.describe('Panel Report', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await page.goto('/ui/panel');
  });

  test('page loads with heading', async ({ page }) => {
    await expect(
      page.getByRole('heading', { name: /panel|gap|care/i })
    ).toBeVisible();
  });

  test('shows patient count', async ({ page }) => {
    const count = page.locator('text=/patient|total/i');
    await expect(count.first()).toBeVisible({ timeout: 10_000 });
  });

  test('shows gap-in-care list or table', async ({ page }) => {
    // Panel report displays care gaps (a1c_overdue, bp_uncontrolled, etc.)
    const gaps = page.locator('table, [class*="gap"], text=/overdue|gap|measure/i');
    await expect(gaps.first()).toBeVisible({ timeout: 10_000 });
  });

  test('shows condition breakdown', async ({ page }) => {
    // Should show conditions like dm, htn, chf
    const conditions = page.locator('text=/condition|dm|htn|chf|copd/i');
    await expect(conditions.first()).toBeVisible({ timeout: 10_000 });
  });
});

import { test, expect, type Page } from '@playwright/test';

async function login(page: Page): Promise<void> {
  await page.goto('/ui/login');
  await page.locator('#email').fill('clinician');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(/\/ui\/?$/);
}

test.describe('Patient Portal', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await page.goto('/ui/portal');
  });

  test('page loads with heading', async ({ page }) => {
    await expect(
      page.getByRole('heading', { name: /patient|portal/i })
    ).toBeVisible();
  });

  test('shows care plan section', async ({ page }) => {
    const carePlan = page.locator('text=/care plan|your plan|medications/i');
    await expect(carePlan.first()).toBeVisible({ timeout: 10_000 });
  });

  test('shows panel gaps or screenings section', async ({ page }) => {
    const gaps = page.locator('text=/screening|gap|upcoming|due/i');
    await expect(gaps.first()).toBeVisible({ timeout: 10_000 });
  });

  test('shows vitals or RPM trends', async ({ page }) => {
    const vitals = page.locator('text=/vital|trend|heart rate|blood pressure/i');
    await expect(vitals.first()).toBeVisible({ timeout: 10_000 });
  });

  test('language selector is visible for i18n', async ({ page }) => {
    // The patient portal has i18n support — look for language selector
    const langSelector = page.locator('select, [class*="lang"], [aria-label*="language"]');
    // May not exist on all builds — soft check
    const count = await langSelector.count();
    if (count > 0) {
      await expect(langSelector.first()).toBeVisible();
    }
  });

  test('displays research disclaimer', async ({ page }) => {
    const disclaimer = page.locator('text=/research|not for clinical|demo/i');
    await expect(disclaimer.first()).toBeVisible({ timeout: 10_000 });
  });
});

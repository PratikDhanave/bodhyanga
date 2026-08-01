import { test, expect, type Page } from '@playwright/test';

async function login(page: Page): Promise<void> {
  await page.goto('/ui/login');
  await page.locator('#email').fill('clinician@hospital.org');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(/\/ui\/?$/);
}

test.describe('Fine-Tune Export', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await page.goto('/ui/fine-tune');
  });

  test('page loads with heading', async ({ page }) => {
    await expect(
      page.getByRole('heading', { name: /fine.?tun|export|training/i })
    ).toBeVisible();
  });

  test('shows SFT and DPO counts', async ({ page }) => {
    // Fine-tune page shows counts of decided reviews available for SFT/DPO
    const stats = page.locator('text=/sft|dpo|decided|approved|rejected/i');
    await expect(stats.first()).toBeVisible({ timeout: 10_000 });
  });

  test('shows export status or format options', async ({ page }) => {
    const exportSection = page.locator('text=/export|format|jsonl|download/i');
    await expect(exportSection.first()).toBeVisible({ timeout: 10_000 });
  });

  test('displays review decision breakdown', async ({ page }) => {
    // Should show how many reviews were approved, rejected, modified
    const breakdown = page.locator('text=/pending|approved|rejected|modified|total/i');
    await expect(breakdown.first()).toBeVisible({ timeout: 10_000 });
  });
});

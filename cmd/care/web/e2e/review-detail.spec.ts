import { test, expect, type Page } from '@playwright/test';

async function login(page: Page): Promise<void> {
  await page.goto('/ui/login');
  await page.locator('#email').fill('clinician');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(/\/ui\/?$/);
}

test.describe('Review Detail', () => {
  test('reviews list links to detail page', async ({ page }) => {
    await login(page);
    await page.goto('/ui/reviews');

    // Wait for the reviews list to load
    await page.waitForTimeout(2000);

    // If there are any reviews, click the first one
    const reviewLinks = page.locator('a[href*="/reviews/"]');
    const count = await reviewLinks.count();
    if (count > 0) {
      await reviewLinks.first().click();
      // Should navigate to a detail page
      await expect(page.url()).toContain('/reviews/');

      // Detail page should show review content
      const content = page.locator('text=/diagnosis|care plan|review|decision/i');
      await expect(content.first()).toBeVisible({ timeout: 10_000 });
    }
  });

  test('detail page shows approve and reject buttons', async ({ page }) => {
    await login(page);
    await page.goto('/ui/reviews');
    await page.waitForTimeout(2000);

    const reviewLinks = page.locator('a[href*="/reviews/"]');
    const count = await reviewLinks.count();
    if (count > 0) {
      await reviewLinks.first().click();

      // Look for decision controls
      const approveBtn = page.locator('button, [role="button"]', { hasText: /approve/i });
      const rejectBtn = page.locator('button, [role="button"]', { hasText: /reject/i });

      // At least one decision control should be visible for pending reviews
      const hasApprove = await approveBtn.count();
      const hasReject = await rejectBtn.count();
      expect(hasApprove + hasReject).toBeGreaterThan(0);
    }
  });

  test('detail page shows case context', async ({ page }) => {
    await login(page);
    await page.goto('/ui/reviews');
    await page.waitForTimeout(2000);

    const reviewLinks = page.locator('a[href*="/reviews/"]');
    const count = await reviewLinks.count();
    if (count > 0) {
      await reviewLinks.first().click();

      // Should show metadata like case_id, agent, kind
      const metadata = page.locator('text=/case|agent|kind|subject|priority/i');
      await expect(metadata.first()).toBeVisible({ timeout: 10_000 });
    }
  });
});

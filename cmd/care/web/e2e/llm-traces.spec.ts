import { test, expect } from '@playwright/test';

// cmd/care started with -diagnostician=rule never emits LLM traces (no
// observability.SetLLMOTelHook invocations originate from the rule
// diagnostician). The /api/llm-traces endpoint therefore returns [] and
// the SPA shows the Empty state card.

async function login(page: import('@playwright/test').Page): Promise<void> {
  await page.goto('/ui/login');
  await page.locator('#email').fill('clinician@hospital.org');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(/\/ui\/?$/);
}

test('LLM traces page shows empty state under rule diagnostician', async ({ page }) => {
  await login(page);
  await page.goto('/ui/llm-traces');

  await expect(page.getByRole('heading', { name: 'LLM traces', level: 1 })).toBeVisible();
  await expect(page.getByText('No LLM calls recorded yet')).toBeVisible();
});

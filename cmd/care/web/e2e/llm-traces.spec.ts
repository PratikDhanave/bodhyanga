import { test, expect } from '@playwright/test';

// cmd/care started with -diagnostician=rule never emits LLM traces (no
// observability.SetLLMOTelHook invocations originate from the rule
// diagnostician). The /api/llm-traces endpoint therefore returns [] and
// the SPA shows the Empty state card.
test('LLM traces page shows empty state under rule diagnostician', async ({ page }) => {
  await page.goto('/ui/llm-traces');

  await expect(page.getByRole('heading', { name: 'LLM traces', level: 1 })).toBeVisible();
  await expect(page.getByText('No LLM calls recorded yet')).toBeVisible();
});

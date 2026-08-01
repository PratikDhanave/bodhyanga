import { test, expect } from '@playwright/test';

// cmd/care/main.go registers exactly 19 agents on startup (see the slice
// passed to reg.Register: intake, rpm_monitor, diagnostic_supervisor,
// questioner, test_planner, cost_guardian, diagnostician,
// reasoning_verifier, cdm_planner, tcm_coordinator, panel_manager,
// pre_visit_prep, inbox_triage, refill_manager, prior_auth, engagement,
// readmission_tracker, human_review).

async function login(page: import('@playwright/test').Page): Promise<void> {
  await page.goto('/ui/login');
  await page.locator('#email').fill('clinician@hospital.org');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(/\/ui\/?$/);
}

test('agents page lists all 19 registered agents with their IDs', async ({ page }) => {
  await login(page);
  await page.goto('/ui/agents');

  // Wait for the table to render (Empty state would not have a <table>).
  const table = page.locator('table');
  await expect(table).toBeVisible();

  // Exactly 19 data rows in <tbody>.
  const rows = table.locator('tbody tr');
  await expect(rows).toHaveCount(19);

  // Spot-check a representative subset of the IDs in the first column.
  for (const id of [
    'intake',
    'diagnostic_supervisor',
    'diagnostician',
    'reasoning_verifier',
    'human_review',
  ]) {
    await expect(table.locator('tbody td.font-mono', { hasText: new RegExp(`^${id}$`) })).toBeVisible();
  }
});

import { test, expect } from '@playwright/test';

// cmd/care/main.go registers exactly 18 agents on startup (see the slice
// passed to reg.Register: intake, rpm_monitor, diagnostic_supervisor,
// questioner, test_planner, cost_guardian, diagnostician,
// reasoning_verifier, cdm_planner, tcm_coordinator, panel_manager,
// pre_visit_prep, inbox_triage, refill_manager, prior_auth, engagement,
// readmission_tracker, human_review).
test('agents page lists all 18 registered agents with their IDs', async ({ page }) => {
  await page.goto('/ui/agents');

  // Wait for the table to render (Empty state would not have a <table>).
  const table = page.locator('table');
  await expect(table).toBeVisible();

  // Exactly 18 data rows in <tbody>.
  const rows = table.locator('tbody tr');
  await expect(rows).toHaveCount(18);

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

import { test, expect } from '@playwright/test';

// End-to-end: select the demo-case-001 fixture in the case runner, POST it,
// land on /ui/runs/<id>, and watch the SSE stream populate the trace card.
// The rule-based diagnostician runs deterministically in <1s, so we expect
// the full event chain (presentation → questioner → test plan → diagnostic
// proposal → ... → diagnosis / review_request) within seconds.

test('submitting a fixture starts a run and streams events', async ({ page }) => {
  await page.goto('/ui/runs');

  // Fixture dropdown lives inside the "From fixture" card. The card renders
  // a Loader until /api/fixtures resolves; only then is the <select> mounted.
  // The select is inside a <label> whose first child span is "Fixture".
  const fixtureSelect = page.locator('label', { hasText: 'Fixture' }).locator('select');
  await expect(fixtureSelect).toBeVisible();
  // Wait until at least the demo-case-001 option is in the DOM.
  await expect(fixtureSelect.locator('option[value="demo-case-001"]')).toHaveCount(1);

  await fixtureSelect.selectOption('demo-case-001');

  await page.getByRole('button', { name: 'Run case' }).click();

  // Navigation to /ui/runs/<uuid>.
  await expect(page).toHaveURL(/\/ui\/runs\/[a-f0-9-]{36}/);

  // The Run trace page header card is labeled "Agent message stream".
  const streamCard = page.locator('.card', { has: page.getByText('Agent message stream') });
  await expect(streamCard).toBeVisible();

  // Each event is rendered as a div with `border-l-2 border-bodh-200 pl-3`.
  // Wait for at least 8 of them.
  const eventRows = streamCard.locator('div.border-l-2');
  await expect.poll(
    async () => eventRows.count(),
    { timeout: 15_000, intervals: [250, 500, 1000] },
  ).toBeGreaterThanOrEqual(8);

  // Spot-check that we saw a 'presentation' message (intake input) and a
  // 'diagnosis_proposal' message (from the diagnostician agent).
  await expect(streamCard.getByText('presentation', { exact: true }).first()).toBeVisible();
  await expect(streamCard.getByText('diagnosis_proposal', { exact: true }).first()).toBeVisible();
});

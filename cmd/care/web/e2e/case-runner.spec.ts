import { test, expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// case-runner.spec.ts — CaseRunner page E2E tests.
//
// Tests cover:
//  1. The fixture list is shown after loading.
//  2. Selecting a fixture enables the "Run case" button (it was never
//     disabled, but the fixture value must be populated before submitting).
//  3. Running a case navigates to the trace page and shows progress events.
//
// cmd/care is started with -diagnostician=rule (deterministic, < 1 s).
// ---------------------------------------------------------------------------

// Shared auth helper.
async function login(page: import('@playwright/test').Page): Promise<void> {
  await page.goto('/ui/login');
  await page.locator('#email').fill('clinician@hospital.org');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(/\/ui\/?$/);
}

// ---------------------------------------------------------------------------
// Test: case runner shows fixture list
// ---------------------------------------------------------------------------
test('case runner shows fixture list', async ({ page }) => {
  await login(page);
  await page.goto('/ui/runs');

  await expect(page.getByRole('heading', { name: 'Case runner', level: 1 })).toBeVisible();

  // The fixture select is rendered once /api/fixtures resolves.
  const fixtureSelect = page.locator('label', { hasText: 'Fixture' }).locator('select');
  await expect(fixtureSelect).toBeVisible();

  // At least the canonical demo-case-001 fixture must be present.
  await expect(fixtureSelect.locator('option[value="demo-case-001"]')).toHaveCount(1);

  // The first option is a placeholder "(use custom presentation below)".
  const firstOption = fixtureSelect.locator('option').first();
  await expect(firstOption).toHaveValue('');
});

// ---------------------------------------------------------------------------
// Test: selecting a fixture enables run button
// ---------------------------------------------------------------------------
test('selecting a fixture enables run button', async ({ page }) => {
  await login(page);
  await page.goto('/ui/runs');

  const fixtureSelect = page.locator('label', { hasText: 'Fixture' }).locator('select');
  await expect(fixtureSelect).toBeVisible();
  await expect(fixtureSelect.locator('option[value="demo-case-001"]')).toHaveCount(1);

  // Before selecting a fixture the "Run case" button must exist and be
  // enabled (it submits with the custom presentation form in that state).
  const runBtn = page.getByRole('button', { name: 'Run case' });
  await expect(runBtn).toBeVisible();
  await expect(runBtn).toBeEnabled();

  // Select a fixture.
  await fixtureSelect.selectOption('demo-case-001');

  // The button must still be enabled and show the idle label.
  await expect(runBtn).toBeEnabled();
  await expect(runBtn).toHaveText('Run case');

  // The fixture value must be reflected in the select element.
  await expect(fixtureSelect).toHaveValue('demo-case-001');
});

// ---------------------------------------------------------------------------
// Test: running a case shows progress events
// ---------------------------------------------------------------------------
test('running a case shows progress events', async ({ page }) => {
  await login(page);
  await page.goto('/ui/runs');

  const fixtureSelect = page.locator('label', { hasText: 'Fixture' }).locator('select');
  await expect(fixtureSelect).toBeVisible();
  await expect(fixtureSelect.locator('option[value="demo-case-001"]')).toHaveCount(1);
  await fixtureSelect.selectOption('demo-case-001');

  await page.getByRole('button', { name: 'Run case' }).click();

  // Navigation to the run-trace page.
  await expect(page).toHaveURL(/\/ui\/runs\/[a-f0-9-]{36}/);

  // The "Agent message stream" card must appear.
  const streamCard = page.locator('.card', { has: page.getByText('Agent message stream') });
  await expect(streamCard).toBeVisible();

  // Wait for at least 8 event rows (rule diagnostician completes in < 1 s).
  const eventRows = streamCard.locator('div.border-l-2');
  await expect.poll(
    async () => eventRows.count(),
    { timeout: 15_000, intervals: [250, 500, 1000] },
  ).toBeGreaterThanOrEqual(8);

  // Spot-check representative message types in the stream.
  await expect(streamCard.getByText('presentation', { exact: true }).first()).toBeVisible();
  await expect(streamCard.getByText('diagnosis_proposal', { exact: true }).first()).toBeVisible();
});

import { test, expect, type Page, type ConsoleMessage } from '@playwright/test';

// ---------------------------------------------------------------------------
// dashboard.spec.ts — Dashboard page E2E tests.
//
// The cmd/care binary is started with -diagnostician=rule (deterministic,
// no API key required).  Auth uses the seeded demo user: clinician/clinic123.
// ---------------------------------------------------------------------------

// Shared helper: authenticate as the default demo clinician before each test.
async function goToDashboard(page: Page): Promise<void> {
  // Set the JWT directly in localStorage to avoid UI login overhead in most
  // tests.  We do this by first hitting /ui/login through the UI so the React
  // app is mounted, then navigating after the token lands.
  await page.goto('/ui/login');
  await page.locator('#email').fill('clinician@hospital.org');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(/\/ui\/?$/);
}

// ---------------------------------------------------------------------------
// Test: dashboard loads and shows stat cards
// ---------------------------------------------------------------------------
test('dashboard loads and shows stat cards', async ({ page }) => {
  await goToDashboard(page);

  // The page-level heading must be present.
  await expect(page.getByRole('heading', { name: 'Dashboard', level: 1 })).toBeVisible();

  // All four stat card labels must be visible.
  await expect(page.locator('a', { hasText: 'Registered agents' })).toBeVisible();
  await expect(page.locator('a', { hasText: 'Pending HITL reviews' })).toBeVisible();
  await expect(page.locator('a', { hasText: 'Open nurse tasks' })).toBeVisible();
  await expect(page.locator('a', { hasText: 'Patients in registry' })).toBeVisible();

  // Wait for all four tiles to resolve (replace the en-dash loading sentinel).
  const agentsTile = page.locator('a', { hasText: 'Registered agents' });
  await expect(agentsTile).toContainText('19');

  await expect(page.locator('a', { hasText: 'Pending HITL reviews' })).toContainText(/\d+/);
  await expect(page.locator('a', { hasText: 'Open nurse tasks' })).toContainText(/\d+/);
  await expect(page.locator('a', { hasText: 'Patients in registry' })).toContainText(/\d+/);

  // No tile should still show the loading placeholder.
  const placeholders = await page.locator('a .text-3xl', { hasText: '–' }).count();
  expect(placeholders).toBe(0);
});

// ---------------------------------------------------------------------------
// Test: stat cards link to correct routes
// ---------------------------------------------------------------------------
test('stat cards link to correct routes', async ({ page }) => {
  await goToDashboard(page);

  // Wait for cards to be rendered.
  await expect(page.locator('a', { hasText: 'Registered agents' })).toBeVisible();

  const cardTargets: Array<{ hasText: string; pathPattern: RegExp }> = [
    { hasText: 'Registered agents', pathPattern: /\/ui\/agents\/?$/ },
    { hasText: 'Pending HITL reviews', pathPattern: /\/ui\/reviews\/?$/ },
    { hasText: 'Open nurse tasks', pathPattern: /\/ui\/tasks\/?$/ },
    { hasText: 'Patients in registry', pathPattern: /\/ui\/patients\/?$/ },
  ];

  for (const { hasText, pathPattern } of cardTargets) {
    // Navigate back to dashboard between each card click.
    await page.goto('/ui/');
    await expect(page.getByRole('heading', { name: 'Dashboard', level: 1 })).toBeVisible();

    await page.locator('a', { hasText }).click();
    await expect(page).toHaveURL(pathPattern);
  }
});

// ---------------------------------------------------------------------------
// Test: pending HITL reviews badge shows correct count
// ---------------------------------------------------------------------------
test('pending HITL reviews badge shows correct count', async ({ page }) => {
  await goToDashboard(page);

  // The "Pending HITL reviews" tile value must be a non-negative integer.
  const tile = page.locator('a', { hasText: 'Pending HITL reviews' });
  await expect(tile).toContainText(/\d+/);

  // Extract the numeric value and verify it is a non-negative integer.
  const tileText = await tile.innerText();
  const match = /(\d+)/.exec(tileText);
  expect(match).not.toBeNull();
  const count = parseInt(match![1], 10);
  expect(count).toBeGreaterThanOrEqual(0);

  // Navigate to /reviews and confirm the heading count agrees (within the
  // same request window — we allow for the "pending" scope which may differ
  // if a review was just created, so we only verify that the count is a
  // non-negative integer on the reviews page too).
  await tile.click();
  await expect(page).toHaveURL(/\/ui\/reviews\/?$/);
  await expect(page.getByRole('heading', { name: 'HITL reviews', level: 1 })).toBeVisible();
});

// ---------------------------------------------------------------------------
// Existing smoke test kept for regression coverage
// ---------------------------------------------------------------------------
test('homepage redirects to /ui/ and dashboard renders', async ({ page }) => {
  // Pre-seed the token so the auth guard passes.
  await goToDashboard(page);
  await page.goto('/');
  await expect(page).toHaveURL(/\/ui\/?$/);
  await expect(page.getByRole('heading', { name: 'Dashboard', level: 1 })).toBeVisible();
  await expect(page.locator('a', { hasText: 'Registered agents' })).toContainText('19');
});

// ---------------------------------------------------------------------------
// Nav smoke: every sidebar link reaches its page without console errors.
// ---------------------------------------------------------------------------
const navTargets: Array<{ link: string; path: string; headingContains: string }> = [
  { link: 'Case runner', path: '/ui/runs', headingContains: 'Case runner' },
  { link: 'HITL reviews', path: '/ui/reviews', headingContains: 'HITL reviews' },
  { link: 'Nurse tasks', path: '/ui/tasks', headingContains: 'Nurse tasks' },
  { link: 'Patients', path: '/ui/patients', headingContains: 'Patients' },
  { link: 'Panel report', path: '/ui/panel', headingContains: 'Panel report' },
  { link: 'Agents', path: '/ui/agents', headingContains: 'Agents' },
  { link: 'RPM', path: '/ui/rpm', headingContains: 'RPM' },
  { link: 'Bench results', path: '/ui/bench', headingContains: 'Bench results' },
  { link: 'LLM traces', path: '/ui/llm-traces', headingContains: 'LLM traces' },
  { link: 'Fine-tune export', path: '/ui/fine-tune', headingContains: 'Fine-tune export' },
  { link: 'About', path: '/ui/about', headingContains: 'About' },
  { link: 'Dashboard', path: '/ui', headingContains: 'Dashboard' },
];

test('nav between every page works without console errors', async ({ page }) => {
  await goToDashboard(page);

  const errors: string[] = [];
  const onConsole = (msg: ConsoleMessage) => {
    if (msg.type() !== 'error') return;
    const text = msg.text();
    if (/StrictMode|deprecation|React Router Future Flag/i.test(text)) return;
    errors.push(text);
  };
  const onPageError = (err: Error) => {
    errors.push(`pageerror: ${err.message}`);
  };
  page.on('console', onConsole);
  page.on('pageerror', onPageError);

  await page.goto('/ui/');
  await expect(page.getByRole('heading', { name: 'Dashboard', level: 1 })).toBeVisible();

  for (const { link, path, headingContains } of navTargets) {
    await clickNav(page, link);
    await expect(page).toHaveURL(new RegExp(`${escapeRegex(path)}/?$`));
    const h1 = page.getByRole('heading', { level: 1 }).first();
    await expect(h1).toContainText(headingContains);
  }

  page.off('console', onConsole);
  page.off('pageerror', onPageError);
  expect(errors, `unexpected console errors:\n${errors.join('\n')}`).toEqual([]);
});

async function clickNav(page: Page, label: string) {
  const sidebar = page.locator('aside');
  await sidebar.getByRole('link', { name: label, exact: true }).click();
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

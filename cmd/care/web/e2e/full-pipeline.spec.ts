import { test, expect, type Page, type ConsoleMessage } from '@playwright/test';
import { loginAs } from './helpers/api';

// ---------------------------------------------------------------------------
// E2.2 — Full MARA pipeline: cross-cutting navigation and integration tests
//
// These tests verify that the entire SPA works as a cohesive unit: every
// major page loads without crashing, sidebar navigation performs client-side
// transitions (no full reload), layout chrome is consistent, unknown routes
// redirect gracefully, and the health endpoint is reachable.
//
// Auth: uses seeded demo credentials clinician / clinic123.
// ---------------------------------------------------------------------------

// Authenticate once before each test.
async function login(page: Page): Promise<void> {
  await loginAs(page, 'clinician', 'clinic123');
}

// ---------------------------------------------------------------------------
// 1. Navigation smoke — visit each major page and confirm it loads.
// ---------------------------------------------------------------------------
test.describe('Navigation smoke', () => {
  const majorPages: Array<{ path: string; description: string }> = [
    { path: '/ui/',           description: 'Dashboard' },
    { path: '/ui/patients',   description: 'Patients' },
    { path: '/ui/tasks',      description: 'Tasks' },
    { path: '/ui/reviews',    description: 'Reviews' },
    { path: '/ui/bench',      description: 'Bench' },
    { path: '/ui/rpm',        description: 'RPM' },
    { path: '/ui/llm-traces', description: 'LLM traces' },
    { path: '/ui/agents',     description: 'Agents' },
    { path: '/ui/audit',      description: 'Audit Log' },
    { path: '/ui/fine-tune',  description: 'Fine-tune' },
    { path: '/ui/about',      description: 'About' },
  ];

  for (const { path, description } of majorPages) {
    test(`${description} (${path}) loads without crashing`, async ({ page }) => {
      await login(page);
      await page.goto(path);
      await page.waitForLoadState('networkidle');

      // The page should render non-trivial content — not blank.
      const body = await page.textContent('body');
      expect(body?.length).toBeGreaterThan(0);

      // The sidebar brand must still be visible (layout did not blow up).
      await expect(page.locator('aside')).toBeVisible();

      // There should be at least one heading on the page.
      const headings = page.getByRole('heading');
      const headingCount = await headings.count();
      expect(headingCount).toBeGreaterThanOrEqual(1);
    });
  }
});

// ---------------------------------------------------------------------------
// 2. SPA nav links — clicking a sidebar link changes the URL without a full
//    page reload (the React app stays mounted).
// ---------------------------------------------------------------------------
test('sidebar link performs SPA navigation without full reload', async ({ page }) => {
  await login(page);
  await page.goto('/ui/');
  await expect(page.getByRole('heading', { name: 'Dashboard', level: 1 })).toBeVisible();

  // Inject a marker into the DOM that would disappear on a full page reload.
  await page.evaluate(() => {
    (window as unknown as Record<string, boolean>).__bodh_spa_marker = true;
  });

  // Click the "Patients" link in the sidebar.
  const sidebar = page.locator('aside');
  await sidebar.getByRole('link', { name: 'Patients', exact: true }).click();

  // URL must change to /ui/patients.
  await expect(page).toHaveURL(/\/ui\/patients\/?$/);

  // The SPA marker must survive — proving no full reload occurred.
  const markerSurvived = await page.evaluate(
    () => (window as unknown as Record<string, boolean>).__bodh_spa_marker === true,
  );
  expect(markerSurvived).toBe(true);

  // Navigate to another page via sidebar to double-check.
  await sidebar.getByRole('link', { name: 'Bench results', exact: true }).click();
  await expect(page).toHaveURL(/\/ui\/bench\/?$/);

  const stillAlive = await page.evaluate(
    () => (window as unknown as Record<string, boolean>).__bodh_spa_marker === true,
  );
  expect(stillAlive).toBe(true);
});

// ---------------------------------------------------------------------------
// 3. Layout consistency — the sidebar is visible on every authed page and
//    contains expected nav group labels and links.
// ---------------------------------------------------------------------------
test('layout sidebar is consistent across pages', async ({ page }) => {
  await login(page);

  const pagesToCheck = ['/ui/', '/ui/patients', '/ui/tasks', '/ui/reviews', '/ui/bench'];

  for (const path of pagesToCheck) {
    await page.goto(path);
    await page.waitForLoadState('networkidle');

    // Sidebar should be visible.
    const sidebar = page.locator('aside');
    await expect(sidebar).toBeVisible();

    // Brand text.
    await expect(sidebar.getByText('Bodh')).toBeVisible();
    await expect(sidebar.getByText('Clinician console')).toBeVisible();

    // Nav group labels.
    await expect(sidebar.getByText('Clinical')).toBeVisible();
    await expect(sidebar.getByText('Population')).toBeVisible();
    await expect(sidebar.getByText('Platform')).toBeVisible();
    await expect(sidebar.getByText('Compliance')).toBeVisible();

    // Core nav links are present.
    await expect(sidebar.getByRole('link', { name: 'Dashboard', exact: true })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Patients', exact: true })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Nurse tasks', exact: true })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'HITL reviews', exact: true })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Agents', exact: true })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'About', exact: true })).toBeVisible();

    // Footer disclaimer.
    await expect(sidebar.getByText('Research demo. Not for clinical use.')).toBeVisible();
  }
});

// ---------------------------------------------------------------------------
// 4. Catch-all redirect — unknown routes redirect to / (the dashboard).
// ---------------------------------------------------------------------------
test('unknown route redirects to dashboard', async ({ page }) => {
  await login(page);

  await page.goto('/ui/nonexistent-page-12345');
  // The SPA catch-all Route should redirect to /ui/.
  await expect(page).toHaveURL(/\/ui\/?$/);

  // Dashboard heading should be visible after redirect.
  await expect(page.getByRole('heading', { name: 'Dashboard', level: 1 })).toBeVisible();
});

// ---------------------------------------------------------------------------
// 5. Health endpoint — /healthz is reachable from the browser.
// ---------------------------------------------------------------------------
test('health endpoint responds with 200', async ({ page, baseURL }) => {
  const res = await page.request.get(`${baseURL}/healthz`);
  expect(res.status()).toBe(200);

  const body = await res.text();
  expect(body.length).toBeGreaterThan(0);
});

// ---------------------------------------------------------------------------
// 6. Dark mode toggle — clicking the toggle applies/removes the "dark" class
//    on <html> and persists the preference.
// ---------------------------------------------------------------------------
test('dark mode toggle applies and persists', async ({ page }) => {
  await login(page);
  await page.goto('/ui/');
  await expect(page.getByRole('heading', { name: 'Dashboard', level: 1 })).toBeVisible();

  // Read initial dark-mode state.
  const initiallyDark = await page.evaluate(() =>
    document.documentElement.classList.contains('dark'),
  );

  // Click the dark/light toggle button.
  const toggleBtn = page.getByRole('button', {
    name: initiallyDark ? /light mode/i : /dark mode/i,
  });
  await expect(toggleBtn).toBeVisible();
  await toggleBtn.click();

  // The <html> class must have flipped.
  const afterToggle = await page.evaluate(() =>
    document.documentElement.classList.contains('dark'),
  );
  expect(afterToggle).toBe(!initiallyDark);

  // localStorage should reflect the new preference.
  const stored = await page.evaluate(() => localStorage.getItem('bodh-dark'));
  expect(stored).toBe(String(!initiallyDark));

  // Toggle back and verify it reverts.
  const revertBtn = page.getByRole('button', {
    name: afterToggle ? /light mode/i : /dark mode/i,
  });
  await revertBtn.click();

  const afterRevert = await page.evaluate(() =>
    document.documentElement.classList.contains('dark'),
  );
  expect(afterRevert).toBe(initiallyDark);
});

// ---------------------------------------------------------------------------
// 7. Cross-page navigation produces no uncaught console errors.
// ---------------------------------------------------------------------------
test('navigating across all pages produces no console errors', async ({ page }) => {
  await login(page);

  const errors: string[] = [];
  const onConsole = (msg: ConsoleMessage) => {
    if (msg.type() !== 'error') return;
    const text = msg.text();
    // Ignore React StrictMode double-render warnings and Future Flag deprecations.
    if (/StrictMode|deprecation|React Router Future Flag/i.test(text)) return;
    errors.push(text);
  };
  const onPageError = (err: Error) => {
    errors.push(`pageerror: ${err.message}`);
  };
  page.on('console', onConsole);
  page.on('pageerror', onPageError);

  const routes = [
    '/ui/',
    '/ui/dashboard',
    '/ui/patients',
    '/ui/runs',
    '/ui/tasks',
    '/ui/reviews',
    '/ui/rpm',
    '/ui/panel',
    '/ui/agents',
    '/ui/bench',
    '/ui/llm-traces',
    '/ui/fine-tune',
    '/ui/audit',
    '/ui/about',
  ];

  for (const route of routes) {
    await page.goto(route);
    await page.waitForLoadState('networkidle');
  }

  page.off('console', onConsole);
  page.off('pageerror', onPageError);

  expect(errors, `Unexpected console errors:\n${errors.join('\n')}`).toEqual([]);
});

// ---------------------------------------------------------------------------
// 8. Active nav link highlighting — the sidebar highlights the current page.
// ---------------------------------------------------------------------------
test('sidebar highlights the active nav link', async ({ page }) => {
  await login(page);
  await page.goto('/ui/patients');
  await page.waitForLoadState('networkidle');

  const sidebar = page.locator('aside');
  const patientsLink = sidebar.getByRole('link', { name: 'Patients', exact: true });
  await expect(patientsLink).toBeVisible();

  // The active link should have the "nav-link-active" CSS class.
  const classes = await patientsLink.getAttribute('class');
  expect(classes).toContain('nav-link-active');

  // A non-active link should NOT have the active class.
  const aboutLink = sidebar.getByRole('link', { name: 'About', exact: true });
  const aboutClasses = await aboutLink.getAttribute('class');
  expect(aboutClasses).not.toContain('nav-link-active');
});

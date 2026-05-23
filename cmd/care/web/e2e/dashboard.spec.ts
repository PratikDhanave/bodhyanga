import { test, expect, type Page, type ConsoleMessage } from '@playwright/test';

// Initial-load smoke + cross-page navigation. cmd/care starts with an empty
// registry (no patients / tasks / reviews) but the in-process agent registry
// always has 18 entries — we use that as the "data really loaded" signal so
// the test does not race the dashboard's four parallel /api/* fetches.

test('homepage redirects to /ui/ and dashboard renders', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveURL(/\/ui\/?$/);

  // Heading.
  await expect(page.getByRole('heading', { name: 'Dashboard', level: 1 })).toBeVisible();

  // Wait for the four /api/* calls to resolve. The "Registered agents" tile
  // is the most reliable: it lands on 18 regardless of any other state.
  const agentsTile = page.locator('a', { hasText: 'Registered agents' });
  await expect(agentsTile).toContainText('18');

  // After agents resolves, the other three tiles should have flipped from
  // "–" (loading sentinel) to numeric values. With a freshly started server
  // the counts are 0 — they still render as "0", not "–". `\b\d+\b` would
  // not match because the Stat component concatenates label + value with no
  // whitespace ("reviews0"), so there's no word boundary between them.
  await expect(page.locator('a', { hasText: 'Pending HITL reviews' })).toContainText(/\d+/);
  await expect(page.locator('a', { hasText: 'Open nurse tasks' })).toContainText(/\d+/);
  await expect(page.locator('a', { hasText: 'Patients in registry' })).toContainText(/\d+/);

  // None of the tiles should still be on the en-dash placeholder.
  const placeholders = await page.locator('a .text-3xl', { hasText: '–' }).count();
  expect(placeholders).toBe(0);
});

// Each nav label, the URL path (under /ui) it lands on, and the substring
// the corresponding page <h1> should contain. RPM is the odd one — nav says
// "RPM", page heading is "RPM observations". Dashboard's NavLink has
// to="/" relative to basename "/ui", which navigates to "/ui" without the
// trailing slash — the regex builder below makes the trailing slash optional.
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
  const errors: string[] = [];
  const onConsole = (msg: ConsoleMessage) => {
    if (msg.type() !== 'error') return;
    const text = msg.text();
    // React StrictMode + dev mode emit benign warnings we don't want to
    // flag. The webServer here runs the embedded prod SPA, but be defensive.
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
    // Wait for client-side route to flip before asserting on the heading.
    // Trailing slash is optional (Dashboard's "/ui" vs others' "/ui/runs").
    await expect(page).toHaveURL(new RegExp(`${escapeRegex(path)}/?$`));
    const h1 = page.getByRole('heading', { level: 1 }).first();
    await expect(h1).toContainText(headingContains);
  }

  page.off('console', onConsole);
  page.off('pageerror', onPageError);
  expect(errors, `unexpected console errors:\n${errors.join('\n')}`).toEqual([]);
});

async function clickNav(page: Page, label: string) {
  // The sidebar uses NavLink — links are <a> tags with the label as text.
  // Scope to the <aside> sidebar so we don't accidentally click stat tiles
  // (e.g. the Dashboard "Registered agents" card whose label contains
  // "Agents") that share their text with nav labels.
  const sidebar = page.locator('aside');
  await sidebar.getByRole('link', { name: label, exact: true }).click();
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

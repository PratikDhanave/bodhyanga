import { test, expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// auth.spec.ts — Authentication flow E2E tests.
//
// Tests cover:
//  1. Unauthenticated users are redirected to /login.
//  2. Login with valid credentials succeeds and lands on the dashboard.
//  3. Login with invalid credentials shows an error message.
//  4. Logout clears the session and redirects to /login.
//
// Default seed credentials (NewCredentialStore):
//   clinician / clinic123   (role: clinician)
//   admin     / admin123    (role: admin)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Test: unauthenticated user redirected to /login
// ---------------------------------------------------------------------------
test('unauthenticated user redirected to /login', async ({ page }) => {
  // Clear any stored token before navigating — important when tests share a
  // browser context across runs.
  await page.goto('/ui/login');
  await page.evaluate(() => localStorage.removeItem('bodh_jwt'));

  // Attempt to reach a protected page.
  await page.goto('/ui/');

  // The auth guard (AuthedLayout) should redirect to /ui/login.
  await expect(page).toHaveURL(/\/ui\/login\/?$/);

  // The Login page must be visible.
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
});

// ---------------------------------------------------------------------------
// Test: login with valid credentials succeeds
// ---------------------------------------------------------------------------
test('login with valid credentials succeeds', async ({ page }) => {
  // Navigate to the login page with no token in storage.
  await page.goto('/ui/login');
  await page.evaluate(() => localStorage.removeItem('bodh_jwt'));

  // Fill and submit the form.
  await page.locator('#email').fill('clinician@hospital.org');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();

  // Successful login redirects to the dashboard.
  await expect(page).toHaveURL(/\/ui\/?$/);
  await expect(page.getByRole('heading', { name: 'Dashboard', level: 1 })).toBeVisible();

  // The JWT must have been stored in localStorage.
  const token = await page.evaluate(() => localStorage.getItem('bodh_jwt'));
  expect(token).not.toBeNull();
  expect(token!.length).toBeGreaterThan(20);
});

// ---------------------------------------------------------------------------
// Test: login with invalid credentials shows error
// ---------------------------------------------------------------------------
test('login with invalid credentials shows error', async ({ page }) => {
  await page.goto('/ui/login');
  await page.evaluate(() => localStorage.removeItem('bodh_jwt'));

  await page.locator('#email').fill('clinician@hospital.org');
  await page.locator('#password').fill('wrongpassword');
  await page.getByRole('button', { name: 'Sign in' }).click();

  // The form must stay on /ui/login — no redirect.
  await expect(page).toHaveURL(/\/ui\/login\/?$/);

  // An error message must be displayed.
  await expect(page.getByText('Invalid credentials')).toBeVisible();

  // No token must have been stored.
  const token = await page.evaluate(() => localStorage.getItem('bodh_jwt'));
  expect(token).toBeNull();
});

// ---------------------------------------------------------------------------
// Test: logout clears session and redirects to /login
// ---------------------------------------------------------------------------
test('logout clears session and redirects to /login', async ({ page }) => {
  // First log in.
  await page.goto('/ui/login');
  await page.locator('#email').fill('clinician@hospital.org');
  await page.locator('#password').fill('clinic123');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/ui\/?$/);
  await expect(page.getByRole('heading', { name: 'Dashboard', level: 1 })).toBeVisible();

  // Locate and click the logout button / link.  The Layout component renders
  // a logout control in the header or sidebar.  We look for any element with
  // the accessible name "Logout" or "Sign out".
  const logoutBtn = page
    .getByRole('button', { name: /log.?out|sign.?out/i })
    .or(page.getByRole('link', { name: /log.?out|sign.?out/i }))
    .first();
  await expect(logoutBtn).toBeVisible();
  await logoutBtn.click();

  // After logout the app should navigate to /ui/login.
  await expect(page).toHaveURL(/\/ui\/login\/?$/);
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();

  // The JWT must have been removed from localStorage.
  const token = await page.evaluate(() => localStorage.getItem('bodh_jwt'));
  expect(token).toBeNull();

  // Navigating to a protected route must redirect back to /login.
  await page.goto('/ui/');
  await expect(page).toHaveURL(/\/ui\/login\/?$/);
});

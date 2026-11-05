import type { Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// loginAs — log in via the UI login form.
//
// Navigates to /ui/login, fills the email + password fields, submits the
// form, and waits until the dashboard heading is visible (i.e. the JWT has
// been stored and the redirect completed).
// ---------------------------------------------------------------------------
export async function loginAs(page: Page, email: string, password: string): Promise<void> {
  await page.goto('/ui/login');
  await page.locator('#email').fill(email);
  await page.locator('#password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  // After a successful login the app navigates to /ui/ (Dashboard).
  await page.waitForURL(/\/ui\/?$/);
}

// ---------------------------------------------------------------------------
// seedReview — POST a review_request directly to the API to seed test data.
//
// reviewData must include at minimum { subject, kind }.  A token obtained
// from loginAs must already be stored in localStorage on `page` — we extract
// it and forward it as a Bearer token.
//
// Returns the created review object (with id, status, …).
// ---------------------------------------------------------------------------
export async function seedReview(
  page: Page,
  baseURL: string,
  reviewData: {
    subject: string;
    kind?: string;
    priority?: string;
    detail?: string;
    payload?: string;
  },
): Promise<{ id: string; [key: string]: unknown }> {
  // Retrieve the JWT from the browser's localStorage so we can authenticate.
  const token = await page.evaluate(() => localStorage.getItem('bodhyanga_jwt'));

  const res = await page.request.post(`${baseURL}/api/reviews`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
    data: {
      subject: reviewData.subject,
      kind: reviewData.kind ?? 'diagnosis_review',
      priority: reviewData.priority ?? 'normal',
      detail: reviewData.detail ?? '',
      payload: reviewData.payload ?? '',
    },
  });

  if (!res.ok()) {
    const text = await res.text();
    throw new Error(`seedReview failed (${res.status()}): ${text}`);
  }

  return res.json() as Promise<{ id: string; [key: string]: unknown }>;
}

// ---------------------------------------------------------------------------
// clearTestData — DELETE /api/test/reset to wipe transient test state.
//
// The server only exposes this endpoint when started with -test-reset-enabled
// (or in E2E mode).  If the endpoint returns 404 / 405 the function silently
// swallows the error so tests that don't rely on a clean slate still pass.
// ---------------------------------------------------------------------------
export async function clearTestData(page: Page, baseURL: string): Promise<void> {
  const token = await page.evaluate(() => localStorage.getItem('bodhyanga_jwt'));
  try {
    await page.request.delete(`${baseURL}/api/test/reset`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
  } catch {
    // Endpoint may not exist — ignore errors gracefully.
  }
}

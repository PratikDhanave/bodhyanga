import { test, expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// E2.1 — API route reachability
//
// Cross-cutting spec that verifies backend API routes are reachable and
// return expected status codes and JSON-parsable responses.  These tests
// hit the HTTP API directly via page.request (no browser UI interaction)
// so they run fast and catch wiring / routing regressions early.
// ---------------------------------------------------------------------------

test.describe('API route reachability', () => {
  // -----------------------------------------------------------------------
  // Kubernetes-style probes
  // -----------------------------------------------------------------------

  test('GET /healthz returns 200', async ({ page }) => {
    const res = await page.request.get('/healthz');
    expect(res.status()).toBe(200);
  });

  test('GET /readyz returns 200', async ({ page }) => {
    const res = await page.request.get('/readyz');
    expect(res.status()).toBe(200);
  });

  // -----------------------------------------------------------------------
  // Data endpoints — verify routes are wired and return valid JSON
  // -----------------------------------------------------------------------

  test('GET /api/patients returns 200 and valid JSON', async ({ page }) => {
    const res = await page.request.get('/api/patients');
    expect(res.status()).toBe(200);

    // The body must be parsable JSON (array or object).
    const body = await res.json();
    expect(body).toBeDefined();
    expect(typeof body === 'object').toBeTruthy();
  });

  test('GET /api/tasks returns 200', async ({ page }) => {
    const res = await page.request.get('/api/tasks');
    expect(res.status()).toBe(200);
  });

  test('GET /api/reviews returns 200', async ({ page }) => {
    const res = await page.request.get('/api/reviews');
    expect(res.status()).toBe(200);
  });

  test('GET /api/agents returns 200', async ({ page }) => {
    const res = await page.request.get('/api/agents');
    expect(res.status()).toBe(200);
  });

  test('GET /api/llm-traces?limit=10 returns 200', async ({ page }) => {
    const res = await page.request.get('/api/llm-traces?limit=10');
    expect(res.status()).toBe(200);
  });

  test('GET /api/fixtures returns 200', async ({ page }) => {
    const res = await page.request.get('/api/fixtures');
    expect(res.status()).toBe(200);
  });

  // -----------------------------------------------------------------------
  // Auth — invalid credentials must not succeed
  // -----------------------------------------------------------------------

  test('POST /api/auth/login with invalid credentials returns non-200', async ({ page }) => {
    const res = await page.request.post('/api/auth/login', {
      data: { email: 'bogus_user', password: 'wrong_password' },
    });
    expect(res.status()).not.toBe(200);
    // Most implementations return 401; accept any non-success status.
    expect(res.status()).toBeGreaterThanOrEqual(400);
  });

  // -----------------------------------------------------------------------
  // Unknown route — should 404
  // -----------------------------------------------------------------------

  test('GET /api/nonexistent returns 404', async ({ page }) => {
    const res = await page.request.get('/api/nonexistent');
    expect(res.status()).toBe(404);
  });
});

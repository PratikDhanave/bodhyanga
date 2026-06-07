import { describe, it, expect, vi, beforeEach } from 'vitest';
import { setToken } from '../../hooks/useAuth';

// We need to import api after setup so localStorage is ready
let api: typeof import('../client').api;

beforeEach(async () => {
  localStorage.clear();
  vi.restoreAllMocks();
  // Dynamic import to get fresh module
  const mod = await import('../client');
  api = mod.api;
});

function routeFor(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input.toString();
}

describe('API client', () => {
  it('injects JWT Authorization header when token exists', async () => {
    setToken('test-jwt-token');
    const fetchSpy = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    vi.stubGlobal('fetch', fetchSpy);

    await api.agents();

    const [, init] = fetchSpy.mock.calls[0];
    expect(init.headers).toHaveProperty('Authorization', 'Bearer test-jwt-token');
  });

  it('does not inject Authorization header when no token', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    vi.stubGlobal('fetch', fetchSpy);

    await api.agents();

    const [, init] = fetchSpy.mock.calls[0];
    expect(init.headers).not.toHaveProperty('Authorization');
  });

  it('throws Error with status and body on non-OK response', async () => {
    setToken('token');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response('Bad request', { status: 400 }),
    ));

    await expect(api.agents()).rejects.toThrow('HTTP 400: Bad request');
  });

  it('returns undefined for 204 No Content', async () => {
    setToken('token');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(null, { status: 204 }),
    ));

    // Use decide as it returns a non-array type
    const result = await api.decide('r1', {
      reviewer_id: 'dr',
      decision: 'approved',
    });
    expect(result).toBeUndefined();
  });

  it('calls correct endpoint for reviews with pending filter', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    vi.stubGlobal('fetch', fetchSpy);

    await api.reviews('pending');

    const url = routeFor(fetchSpy.mock.calls[0][0] as RequestInfo | URL);
    expect(url).toBe('/api/reviews?status=pending');
  });

  it('calls correct endpoint for reviews without filter', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    vi.stubGlobal('fetch', fetchSpy);

    await api.reviews();

    const url = routeFor(fetchSpy.mock.calls[0][0] as RequestInfo | URL);
    expect(url).toBe('/api/reviews');
  });

  it('sends POST with JSON body for decide', async () => {
    setToken('token');
    const fetchSpy = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ id: 'r1', status: 'approved' }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    vi.stubGlobal('fetch', fetchSpy);

    await api.decide('r1', {
      reviewer_id: 'dr-smith',
      decision: 'approved',
      reason: 'Confirmed diagnosis',
    });

    const [url, init] = fetchSpy.mock.calls[0];
    expect(routeFor(url as RequestInfo | URL)).toBe('/api/reviews/r1/decide');
    expect(init.method).toBe('POST');
    const body = JSON.parse(init.body);
    expect(body).toEqual({
      reviewer_id: 'dr-smith',
      decision: 'approved',
      reason: 'Confirmed diagnosis',
    });
  });

  it('calls llmTraces with default limit', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    vi.stubGlobal('fetch', fetchSpy);

    await api.llmTraces();

    const url = routeFor(fetchSpy.mock.calls[0][0] as RequestInfo | URL);
    expect(url).toBe('/api/llm-traces?limit=100');
  });

  it('calls llmTraces with custom limit', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    vi.stubGlobal('fetch', fetchSpy);

    await api.llmTraces(50);

    const url = routeFor(fetchSpy.mock.calls[0][0] as RequestInfo | URL);
    expect(url).toBe('/api/llm-traces?limit=50');
  });
});

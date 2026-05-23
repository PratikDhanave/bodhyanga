// Vitest test setup — applied to every test file before it runs.
//
// Adds:
// - jest-dom matchers (toBeInTheDocument, toHaveTextContent, etc.)
// - a global fetch mock helper installed automatically; tests override it
// - a minimal EventSource stub so SSE-driven components can be tested
//   without a real network endpoint
import '@testing-library/jest-dom/vitest';
import { afterEach, vi } from 'vitest';
import { cleanup } from '@testing-library/react';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

// Minimal EventSource stub. Tests can call `eventSourceFire(url, data)` to
// push a `MessageEvent` to any subscriber on that URL.
type Handler = (e: MessageEvent) => void;
const sourcesByURL = new Map<string, Set<EventSource & { __handlers: Set<Handler> }>>();

class StubEventSource {
  static instances: StubEventSource[] = [];
  url: string;
  readyState = 1;
  onmessage: Handler | null = null;
  onerror: ((e: Event) => void) | null = null;
  onopen: ((e: Event) => void) | null = null;
  __handlers: Set<Handler> = new Set();

  constructor(url: string) {
    this.url = url;
    StubEventSource.instances.push(this);
    const set = sourcesByURL.get(url) ?? new Set();
    set.add(this as unknown as EventSource & { __handlers: Set<Handler> });
    sourcesByURL.set(url, set);
  }

  addEventListener(_event: string, fn: Handler) {
    this.__handlers.add(fn);
  }

  close() {
    this.readyState = 2;
    const set = sourcesByURL.get(this.url);
    if (set) set.delete(this as unknown as EventSource & { __handlers: Set<Handler> });
  }
}

(globalThis as unknown as { EventSource: typeof StubEventSource }).EventSource = StubEventSource;

export function eventSourceFire(url: string, data: unknown) {
  const event = new MessageEvent('message', { data: JSON.stringify(data) });
  const sources = sourcesByURL.get(url);
  if (!sources) return;
  for (const s of sources) {
    if (s.onmessage) s.onmessage(event);
    for (const h of s.__handlers) h(event);
  }
}

export function lastEventSource(): StubEventSource | undefined {
  return StubEventSource.instances[StubEventSource.instances.length - 1];
}

export function resetEventSources() {
  StubEventSource.instances.length = 0;
  sourcesByURL.clear();
}

// Lightweight fetch mocker. Returns the spy so each test can configure
// per-URL responses.
export function mockFetch(handler: (input: RequestInfo | URL, init?: RequestInit) => unknown) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const result = await handler(input, init);
    if (result instanceof Response) return result;
    return new Response(JSON.stringify(result), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  vi.stubGlobal('fetch', fn);
  return fn;
}

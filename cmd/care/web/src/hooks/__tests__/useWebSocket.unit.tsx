import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { useWebSocket } from '../useWebSocket';

// Mock WebSocket
class MockWebSocket {
  static instances: MockWebSocket[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 0; // CONNECTING
  closed = false;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  close() {
    this.closed = true;
    this.readyState = 3; // CLOSED
  }

  // Test helpers
  simulateOpen() {
    this.readyState = 1;
    this.onopen?.();
  }

  simulateMessage(data: string) {
    this.onmessage?.({ data });
  }

  simulateClose() {
    this.onclose?.();
  }

  simulateError() {
    this.onerror?.();
  }
}

function TestComponent({ path }: { path: string }) {
  const { lastMessage, connected } = useWebSocket(path);
  return (
    <div>
      <span data-testid="connected">{String(connected)}</span>
      <span data-testid="message">{lastMessage ? JSON.stringify(lastMessage) : 'null'}</span>
    </div>
  );
}

describe('useWebSocket', () => {
  let originalWebSocket: typeof WebSocket;

  beforeEach(() => {
    MockWebSocket.instances = [];
    originalWebSocket = globalThis.WebSocket;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    globalThis.WebSocket = MockWebSocket as any;
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    globalThis.WebSocket = originalWebSocket;
    vi.useRealTimers();
  });

  it('starts disconnected', () => {
    render(<TestComponent path="/ws/test" />);
    expect(screen.getByTestId('connected')).toHaveTextContent('false');
    expect(screen.getByTestId('message')).toHaveTextContent('null');
  });

  it('creates WebSocket with correct URL', () => {
    render(<TestComponent path="/ws/events" />);
    expect(MockWebSocket.instances).toHaveLength(1);
    expect(MockWebSocket.instances[0].url).toContain('/ws/events');
  });

  it('shows connected on open', async () => {
    render(<TestComponent path="/ws/test" />);
    const ws = MockWebSocket.instances[0];

    await act(async () => {
      ws.simulateOpen();
    });

    expect(screen.getByTestId('connected')).toHaveTextContent('true');
  });

  it('receives JSON messages', async () => {
    render(<TestComponent path="/ws/test" />);
    const ws = MockWebSocket.instances[0];

    await act(async () => {
      ws.simulateOpen();
    });

    await act(async () => {
      ws.simulateMessage('{"type":"alert","value":42}');
    });

    expect(screen.getByTestId('message')).toHaveTextContent('{"type":"alert","value":42}');
  });

  it('handles non-JSON messages', async () => {
    render(<TestComponent path="/ws/test" />);
    const ws = MockWebSocket.instances[0];

    await act(async () => {
      ws.simulateOpen();
    });

    await act(async () => {
      ws.simulateMessage('plain text message');
    });

    expect(screen.getByTestId('message')).toHaveTextContent('"plain text message"');
  });

  it('shows disconnected on close', async () => {
    render(<TestComponent path="/ws/test" />);
    const ws = MockWebSocket.instances[0];

    await act(async () => {
      ws.simulateOpen();
    });
    expect(screen.getByTestId('connected')).toHaveTextContent('true');

    await act(async () => {
      ws.simulateClose();
    });
    expect(screen.getByTestId('connected')).toHaveTextContent('false');
  });

  it('closes WebSocket on error', async () => {
    render(<TestComponent path="/ws/test" />);
    const ws = MockWebSocket.instances[0];

    await act(async () => {
      ws.simulateError();
    });

    expect(ws.closed).toBe(true);
  });

  it('cleans up on unmount', async () => {
    const { unmount } = render(<TestComponent path="/ws/test" />);
    const ws = MockWebSocket.instances[0];

    unmount();
    expect(ws.closed).toBe(true);
  });

  it('reconnects with exponential backoff after close', async () => {
    render(<TestComponent path="/ws/test" />);
    const ws1 = MockWebSocket.instances[0];

    await act(async () => {
      ws1.simulateOpen();
    });

    // Simulate close — triggers reconnect timer
    await act(async () => {
      ws1.simulateClose();
    });

    // Advance timer past first backoff (1000ms)
    await act(async () => {
      vi.advanceTimersByTime(1100);
    });

    // A new WebSocket should have been created
    expect(MockWebSocket.instances.length).toBeGreaterThanOrEqual(2);
  });
});

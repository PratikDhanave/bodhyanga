import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { ConnectionStatus } from '../ConnectionStatus';

describe('ConnectionStatus', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows "Connected" when health check succeeds', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('ok', { status: 200 }),
    );
    render(<ConnectionStatus />);

    await waitFor(() => {
      expect(screen.getByText('Connected')).toBeInTheDocument();
    });
  });

  it('shows "Disconnected" when health check returns non-ok', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('', { status: 503 }),
    );
    render(<ConnectionStatus />);

    await waitFor(() => {
      expect(screen.getByText('Disconnected')).toBeInTheDocument();
    });
  });

  it('shows "Disconnected" when fetch throws', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('Network error'));
    render(<ConnectionStatus />);

    await waitFor(() => {
      expect(screen.getByText('Disconnected')).toBeInTheDocument();
    });
  });

  it('includes API status in title attribute', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('ok', { status: 200 }),
    );
    render(<ConnectionStatus />);

    await waitFor(() => {
      expect(screen.getByTitle('API: Connected')).toBeInTheDocument();
    });
  });

  it('renders status dot element', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('ok', { status: 200 }),
    );
    render(<ConnectionStatus />);

    await waitFor(() => {
      const container = screen.getByTitle('API: Connected');
      const dot = container.querySelector('span.rounded-full');
      expect(dot).toBeInTheDocument();
    });
  });
});

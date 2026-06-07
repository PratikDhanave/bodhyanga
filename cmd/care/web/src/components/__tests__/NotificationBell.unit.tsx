import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import NotificationBell from '../NotificationBell';
import { ToastProvider } from '../Toast';

// Mock useWebSocket
const mockUseWebSocket = vi.fn().mockReturnValue({ lastMessage: null, connected: false });
vi.mock('../../hooks/useWebSocket', () => ({
  useWebSocket: (...args: unknown[]) => mockUseWebSocket(...args),
}));

function renderBell() {
  return render(
    <MemoryRouter>
      <ToastProvider>
        <NotificationBell />
      </ToastProvider>
    </MemoryRouter>,
  );
}

describe('NotificationBell', () => {
  beforeEach(() => {
    mockUseWebSocket.mockReturnValue({ lastMessage: null, connected: false });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the bell button', () => {
    renderBell();
    expect(screen.getByRole('button', { name: 'Notifications' })).toBeInTheDocument();
  });

  it('shows disconnected indicator when not connected', () => {
    renderBell();
    expect(screen.getByTitle('Disconnected')).toBeInTheDocument();
  });

  it('shows connected indicator when connected', () => {
    mockUseWebSocket.mockReturnValue({ lastMessage: null, connected: true });
    renderBell();
    expect(screen.getByTitle('Connected')).toBeInTheDocument();
  });

  it('opens dropdown panel on click', async () => {
    const user = userEvent.setup();
    renderBell();

    await user.click(screen.getByRole('button', { name: 'Notifications' }));
    expect(screen.getByText('HITL Alerts')).toBeInTheDocument();
    expect(screen.getByText('No alerts yet')).toBeInTheDocument();
  });

  it('closes dropdown on second click', async () => {
    const user = userEvent.setup();
    renderBell();

    await user.click(screen.getByRole('button', { name: 'Notifications' }));
    expect(screen.getByText('HITL Alerts')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Notifications' }));
    expect(screen.queryByText('HITL Alerts')).not.toBeInTheDocument();
  });

  it('shows "View all" link to /reviews', async () => {
    const user = userEvent.setup();
    renderBell();

    await user.click(screen.getByRole('button', { name: 'Notifications' }));
    const viewAll = screen.getByText('View all');
    expect(viewAll).toBeInTheDocument();
    expect(viewAll.closest('a')).toHaveAttribute('href', '/reviews');
  });

  it('accumulates alerts when messages arrive', async () => {
    const { rerender } = render(
      <MemoryRouter>
        <ToastProvider>
          <NotificationBell />
        </ToastProvider>
      </MemoryRouter>,
    );

    // Simulate a new message arriving
    mockUseWebSocket.mockReturnValue({
      lastMessage: { id: 'alert-1', message: 'Lab review needed', timestamp: '2026-06-06T10:00:00Z' },
      connected: true,
    });

    await act(async () => {
      rerender(
        <MemoryRouter>
          <ToastProvider>
            <NotificationBell />
          </ToastProvider>
        </MemoryRouter>,
      );
    });

    // Should show unread badge
    expect(screen.getByText('1')).toBeInTheDocument();
  });
});

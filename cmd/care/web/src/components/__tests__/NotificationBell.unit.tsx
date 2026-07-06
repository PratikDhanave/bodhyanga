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

/** Simulate a WS message arriving by updating the mock and re-rendering. */
function simulateMessage(
  rerender: (ui: React.ReactElement) => void,
  msg: Record<string, unknown>,
) {
  mockUseWebSocket.mockReturnValue({
    lastMessage: msg,
    connected: true,
  });
  return act(async () => {
    rerender(
      <MemoryRouter>
        <ToastProvider>
          <NotificationBell />
        </ToastProvider>
      </MemoryRouter>,
    );
  });
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
    const { rerender } = renderBell();

    await simulateMessage(rerender, {
      id: 'alert-1',
      message: 'Lab review needed',
      timestamp: '2026-06-06T10:00:00Z',
      priority: 'normal',
    });

    // Should show unread badge
    expect(screen.getByText('1')).toBeInTheDocument();
  });

  // ---------- Priority-based toasting ----------

  it('toasts for urgent-priority messages', async () => {
    const { rerender } = renderBell();

    await simulateMessage(rerender, {
      id: 'u1',
      message: 'Critical lab result',
      timestamp: '2026-06-06T10:00:00Z',
      priority: 'urgent',
    });

    // Toast appears for urgent messages — look for the warning toast text
    expect(screen.getByText('New review needs attention')).toBeInTheDocument();
  });

  it('toasts for high-priority messages', async () => {
    const { rerender } = renderBell();

    await simulateMessage(rerender, {
      id: 'h1',
      message: 'High priority review',
      timestamp: '2026-06-06T10:00:00Z',
      priority: 'high',
    });

    expect(screen.getByText('New review needs attention')).toBeInTheDocument();
  });

  it('does NOT toast for normal-priority messages', async () => {
    const { rerender } = renderBell();

    await simulateMessage(rerender, {
      id: 'n1',
      message: 'Routine review',
      timestamp: '2026-06-06T10:00:00Z',
      priority: 'normal',
    });

    expect(screen.queryByText('New review needs attention')).not.toBeInTheDocument();
  });

  it('does NOT toast for low-priority messages', async () => {
    const { rerender } = renderBell();

    await simulateMessage(rerender, {
      id: 'l1',
      message: 'Low priority task',
      timestamp: '2026-06-06T10:00:00Z',
      priority: 'low',
    });

    expect(screen.queryByText('New review needs attention')).not.toBeInTheDocument();
  });

  // ---------- Badge color by priority ----------

  it('shows red badge when unread alerts include urgent priority', async () => {
    const { rerender } = renderBell();

    await simulateMessage(rerender, {
      id: 'u2',
      message: 'Urgent alert',
      timestamp: '2026-06-06T10:00:00Z',
      priority: 'urgent',
    });

    const badge = screen.getByText('1');
    expect(badge.className).toContain('bg-red-500');
  });

  it('shows amber badge when unread alerts are normal priority only', async () => {
    const { rerender } = renderBell();

    await simulateMessage(rerender, {
      id: 'n2',
      message: 'Normal alert',
      timestamp: '2026-06-06T10:00:00Z',
      priority: 'normal',
    });

    const badge = screen.getByText('1');
    expect(badge.className).toContain('bg-amber-500');
  });

  // ---------- DND controls ----------

  it('shows DND duration buttons in dropdown', async () => {
    const user = userEvent.setup();
    renderBell();

    await user.click(screen.getByRole('button', { name: 'Notifications' }));

    expect(screen.getByText('Do not disturb:')).toBeInTheDocument();
    expect(screen.getByText('15 min')).toBeInTheDocument();
    expect(screen.getByText('30 min')).toBeInTheDocument();
    expect(screen.getByText('1 hour')).toBeInTheDocument();
  });

  it('enables DND when duration button is clicked', async () => {
    const user = userEvent.setup();
    renderBell();

    await user.click(screen.getByRole('button', { name: 'Notifications' }));
    await user.click(screen.getByText('15 min'));

    // Should now show "DND active" and "Turn off" instead of duration buttons
    expect(screen.getByText('DND active')).toBeInTheDocument();
    expect(screen.getByText('Turn off')).toBeInTheDocument();
    expect(screen.queryByText('15 min')).not.toBeInTheDocument();
  });

  it('disables DND when "Turn off" is clicked', async () => {
    const user = userEvent.setup();
    renderBell();

    await user.click(screen.getByRole('button', { name: 'Notifications' }));
    await user.click(screen.getByText('30 min'));

    expect(screen.getByText('DND active')).toBeInTheDocument();

    await user.click(screen.getByText('Turn off'));

    // Should go back to showing duration buttons
    expect(screen.queryByText('DND active')).not.toBeInTheDocument();
    expect(screen.getByText('Do not disturb:')).toBeInTheDocument();
    expect(screen.getByText('15 min')).toBeInTheDocument();
  });

  it('suppresses toasts when DND is active', async () => {
    const user = userEvent.setup();
    const { rerender } = renderBell();

    // Enable DND
    await user.click(screen.getByRole('button', { name: 'Notifications' }));
    await user.click(screen.getByText('1 hour'));

    // Close the dropdown
    await user.click(screen.getByRole('button', { name: 'Notifications' }));

    // Simulate an urgent message arriving
    await simulateMessage(rerender, {
      id: 'dnd-test',
      message: 'Urgent while DND',
      timestamp: '2026-06-06T10:00:00Z',
      priority: 'urgent',
    });

    // Alert is accumulated (badge shows) but NO toast
    expect(screen.queryByText('New review needs attention')).not.toBeInTheDocument();
    // The unread count badge should still appear
    expect(screen.getByText('1')).toBeInTheDocument();
  });

  it('shows DND overlay on bell icon when active', async () => {
    const user = userEvent.setup();
    renderBell();

    await user.click(screen.getByRole('button', { name: 'Notifications' }));
    await user.click(screen.getByText('15 min'));

    // Close dropdown to see bell
    await user.click(screen.getByRole('button', { name: 'Notifications' }));

    // DND indicator should be visible on the bell
    expect(screen.getByText('DND')).toBeInTheDocument();
  });

  it('shows red dot indicator next to high-priority alerts in dropdown', async () => {
    const user = userEvent.setup();
    const { rerender } = renderBell();

    await simulateMessage(rerender, {
      id: 'hp1',
      message: 'High priority alert in list',
      timestamp: '2026-06-06T10:00:00Z',
      priority: 'high',
    });

    // Open dropdown to see alert list
    await user.click(screen.getByRole('button', { name: 'Notifications' }));

    expect(screen.getByText('High priority alert in list')).toBeInTheDocument();
    // The red dot indicator is a span with bg-red-500
    const listItem = screen.getByText('High priority alert in list').closest('div');
    const dot = listItem?.querySelector('.bg-red-500');
    expect(dot).toBeInTheDocument();
  });

  it('does not show red dot for normal-priority alerts in dropdown', async () => {
    const user = userEvent.setup();
    const { rerender } = renderBell();

    await simulateMessage(rerender, {
      id: 'np1',
      message: 'Normal priority alert in list',
      timestamp: '2026-06-06T10:00:00Z',
      priority: 'normal',
    });

    await user.click(screen.getByRole('button', { name: 'Notifications' }));

    expect(screen.getByText('Normal priority alert in list')).toBeInTheDocument();
    const listItem = screen.getByText('Normal priority alert in list').closest('div');
    const dot = listItem?.querySelector('.bg-red-500');
    expect(dot).not.toBeInTheDocument();
  });

  it('auto-expires DND after timeout', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
      renderBell();

      // Enable 15-minute DND
      await user.click(screen.getByRole('button', { name: 'Notifications' }));
      await user.click(screen.getByText('15 min'));

      expect(screen.getByText('DND active')).toBeInTheDocument();

      // Advance time past the DND duration
      await act(async () => {
        vi.advanceTimersByTime(15 * 60 * 1000 + 100);
      });

      // DND should have auto-expired — duration buttons should be back
      expect(screen.queryByText('DND active')).not.toBeInTheDocument();
      expect(screen.getByText('Do not disturb:')).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });
});

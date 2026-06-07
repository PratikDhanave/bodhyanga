import { describe, it, expect, vi } from 'vitest';
import { render, screen, act, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ToastProvider, useToast } from '../Toast';

// Helper to trigger a toast from inside the provider
function ToastTrigger({ message, type }: { message: string; type?: 'info' | 'success' | 'warning' | 'error' }) {
  const { showToast } = useToast();
  return <button onClick={() => showToast(message, type)}>Trigger</button>;
}

function renderWithToast(type?: 'info' | 'success' | 'warning' | 'error') {
  return render(
    <ToastProvider>
      <ToastTrigger message="Test toast" type={type} />
    </ToastProvider>,
  );
}

describe('Toast', () => {
  it('shows toast when showToast is called', async () => {
    const user = userEvent.setup();
    renderWithToast();

    await user.click(screen.getByRole('button', { name: 'Trigger' }));
    expect(screen.getByText('Test toast')).toBeInTheDocument();
  });

  it('renders info type with correct icon', async () => {
    const user = userEvent.setup();
    renderWithToast('info');

    await user.click(screen.getByRole('button', { name: 'Trigger' }));
    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.getByText('i')).toBeInTheDocument();
  });

  it('renders success type with checkmark icon', async () => {
    const user = userEvent.setup();
    renderWithToast('success');

    await user.click(screen.getByRole('button', { name: 'Trigger' }));
    expect(screen.getByText('✓')).toBeInTheDocument();
  });

  it('renders error type with X icon', async () => {
    const user = userEvent.setup();
    renderWithToast('error');

    await user.click(screen.getByRole('button', { name: 'Trigger' }));
    expect(screen.getByText('✕')).toBeInTheDocument();
  });

  it('renders warning type with exclamation icon', async () => {
    const user = userEvent.setup();
    renderWithToast('warning');

    await user.click(screen.getByRole('button', { name: 'Trigger' }));
    expect(screen.getByText('!')).toBeInTheDocument();
  });

  it('dismisses toast when dismiss button is clicked', async () => {
    const user = userEvent.setup();
    renderWithToast();

    await user.click(screen.getByRole('button', { name: 'Trigger' }));
    expect(screen.getByText('Test toast')).toBeInTheDocument();

    await user.click(screen.getByLabelText('Dismiss'));
    await waitFor(() => {
      expect(screen.queryByText('Test toast')).not.toBeInTheDocument();
    });
  });

  it('auto-dismisses after 5 seconds', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
      renderWithToast();

      await user.click(screen.getByRole('button', { name: 'Trigger' }));
      expect(screen.getByText('Test toast')).toBeInTheDocument();

      await act(async () => { vi.advanceTimersByTime(5100); });
      expect(screen.queryByText('Test toast')).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it('stacks multiple toasts', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });

      function MultiTrigger() {
        const { showToast } = useToast();
        return (
          <>
            <button onClick={() => showToast('First')}>T1</button>
            <button onClick={() => showToast('Second')}>T2</button>
          </>
        );
      }

      render(
        <ToastProvider>
          <MultiTrigger />
        </ToastProvider>,
      );

      await user.click(screen.getByRole('button', { name: 'T1' }));
      await user.click(screen.getByRole('button', { name: 'T2' }));

      expect(screen.getByText('First')).toBeInTheDocument();
      expect(screen.getByText('Second')).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });
});

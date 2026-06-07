import { describe, it, expect, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConfirmButton } from '../ConfirmButton';

describe('ConfirmButton', () => {
  it('shows initial label', () => {
    render(<ConfirmButton label="Delete" onConfirm={() => {}} />);
    expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument();
  });

  it('enters confirm mode on first click', async () => {
    const user = userEvent.setup();
    render(<ConfirmButton label="Delete" onConfirm={() => {}} />);

    await user.click(screen.getByRole('button', { name: 'Delete' }));
    expect(screen.getByRole('button', { name: 'Confirm Delete' })).toBeInTheDocument();
  });

  it('calls onConfirm on second click', async () => {
    const onConfirm = vi.fn();
    const user = userEvent.setup();
    render(<ConfirmButton label="Delete" onConfirm={onConfirm} />);

    await user.click(screen.getByRole('button', { name: 'Delete' }));
    await user.click(screen.getByRole('button', { name: 'Confirm Delete' }));
    expect(onConfirm).toHaveBeenCalledOnce();
  });

  it('uses custom confirmLabel when provided', async () => {
    const user = userEvent.setup();
    render(<ConfirmButton label="Remove" confirmLabel="Yes, remove it" onConfirm={() => {}} />);

    await user.click(screen.getByRole('button', { name: 'Remove' }));
    expect(screen.getByRole('button', { name: 'Yes, remove it' })).toBeInTheDocument();
  });

  it('auto-reverts after 5 seconds', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
      render(<ConfirmButton label="Delete" onConfirm={() => {}} />);

      await user.click(screen.getByRole('button', { name: 'Delete' }));
      expect(screen.getByText('Confirm Delete')).toBeInTheDocument();

      await act(async () => { vi.advanceTimersByTime(5100); });
      expect(screen.getByText('Delete')).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it('reverts when clicking outside', async () => {
    const user = userEvent.setup();
    render(
      <div>
        <ConfirmButton label="Delete" onConfirm={() => {}} />
        <span data-testid="outside">outside</span>
      </div>,
    );

    await user.click(screen.getByRole('button', { name: 'Delete' }));
    expect(screen.getByText('Confirm Delete')).toBeInTheDocument();

    await user.click(screen.getByTestId('outside'));
    expect(screen.getByText('Delete')).toBeInTheDocument();
  });

  it('applies danger variant styling', async () => {
    const user = userEvent.setup();
    render(<ConfirmButton label="Reject" variant="danger" onConfirm={() => {}} />);

    const btn = screen.getByRole('button', { name: 'Reject' });
    expect(btn.className).toContain('bg-red-600');

    await user.click(btn);
    expect(screen.getByRole('button', { name: 'Confirm Reject' }).className).toContain('ring-red');
  });
});

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Tooltip } from '../Tooltip';

describe('Tooltip', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders children', () => {
    render(
      <Tooltip content="Help text">
        <button>Hover me</button>
      </Tooltip>,
    );
    expect(screen.getByRole('button', { name: 'Hover me' })).toBeInTheDocument();
  });

  it('tooltip is hidden by default', () => {
    render(
      <Tooltip content="Help text">
        <span>Target</span>
      </Tooltip>,
    );
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('shows tooltip on mouse hover after delay', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(
      <Tooltip content="Helpful info">
        <span>Target</span>
      </Tooltip>,
    );

    // Hover over the trigger
    await user.hover(screen.getByText('Target'));

    // Advance past the 150ms delay
    await act(async () => {
      vi.advanceTimersByTime(200);
    });

    expect(screen.getByRole('tooltip')).toHaveTextContent('Helpful info');
  });

  it('hides tooltip on mouse leave', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(
      <Tooltip content="Helpful info">
        <span>Target</span>
      </Tooltip>,
    );

    await user.hover(screen.getByText('Target'));
    await act(async () => {
      vi.advanceTimersByTime(200);
    });
    expect(screen.getByRole('tooltip')).toBeInTheDocument();

    await user.unhover(screen.getByText('Target'));
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('applies correct position classes', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(
      <Tooltip content="Bottom tip" position="bottom">
        <span>Target</span>
      </Tooltip>,
    );

    await user.hover(screen.getByText('Target'));
    await act(async () => {
      vi.advanceTimersByTime(200);
    });

    const tooltip = screen.getByRole('tooltip');
    expect(tooltip.className).toContain('top-full');
  });

  it('cancels show timer on quick mouse leave', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(
      <Tooltip content="Should not show">
        <span>Target</span>
      </Tooltip>,
    );

    // Hover and immediately unhover (before 150ms)
    await user.hover(screen.getByText('Target'));
    await act(async () => {
      vi.advanceTimersByTime(50);
    });
    await user.unhover(screen.getByText('Target'));

    // Advance past the delay
    await act(async () => {
      vi.advanceTimersByTime(200);
    });

    // Tooltip should not be visible
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });
});

import { describe, it, expect, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SearchInput } from '../SearchInput';

describe('SearchInput', () => {
  it('renders with placeholder', () => {
    render(<SearchInput onSearch={() => {}} placeholder="Find patients…" />);
    expect(screen.getByPlaceholderText('Find patients…')).toBeInTheDocument();
  });

  it('calls onSearch after debounce', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const onSearch = vi.fn();
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
      render(<SearchInput onSearch={onSearch} debounce={200} />);

      await user.type(screen.getByRole('searchbox'), 'test');
      // Not yet called during typing
      expect(onSearch).not.toHaveBeenCalledWith('test');

      // Advance past debounce
      await act(async () => { vi.advanceTimersByTime(250); });
      expect(onSearch).toHaveBeenCalledWith('test');
    } finally {
      vi.useRealTimers();
    }
  });

  it('shows clear button when text is entered', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
      render(<SearchInput onSearch={() => {}} />);

      expect(screen.queryByLabelText('Clear search')).not.toBeInTheDocument();

      await user.type(screen.getByRole('searchbox'), 'hello');
      expect(screen.getByLabelText('Clear search')).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it('clears input when clear button is clicked', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const onSearch = vi.fn();
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
      render(<SearchInput onSearch={onSearch} />);

      await user.type(screen.getByRole('searchbox'), 'test');
      await user.click(screen.getByLabelText('Clear search'));

      expect(screen.getByRole('searchbox')).toHaveValue('');
      expect(onSearch).toHaveBeenCalledWith('');
    } finally {
      vi.useRealTimers();
    }
  });

  it('works in controlled mode', async () => {
    const onSearch = vi.fn();
    const user = userEvent.setup();
    render(<SearchInput onSearch={onSearch} value="controlled" />);

    expect(screen.getByRole('searchbox')).toHaveValue('controlled');

    // Typing triggers onSearch immediately in controlled mode
    await user.type(screen.getByRole('searchbox'), 'x');
    expect(onSearch).toHaveBeenCalled();
  });

  it('has accessible label matching placeholder', () => {
    render(<SearchInput onSearch={() => {}} placeholder="Search agents" />);
    expect(screen.getByLabelText('Search agents')).toBeInTheDocument();
  });
});

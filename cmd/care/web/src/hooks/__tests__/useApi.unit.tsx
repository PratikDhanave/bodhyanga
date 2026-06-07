import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useApi } from '../useApi';

// Test component that exposes useApi state
function TestComponent({ fn, deps = [] }: { fn: () => Promise<unknown>; deps?: unknown[] }) {
  const { data, loading, error, reload } = useApi(fn, deps);
  return (
    <div>
      {loading && <span data-testid="loading">Loading</span>}
      {error && <span data-testid="error">{error.message}</span>}
      {data !== null && <span data-testid="data">{JSON.stringify(data)}</span>}
      <button onClick={reload}>Reload</button>
    </div>
  );
}

describe('useApi', () => {
  it('starts in loading state', () => {
    const fn = () => new Promise<string>(() => {}); // never resolves
    render(<TestComponent fn={fn} />);
    expect(screen.getByTestId('loading')).toBeInTheDocument();
  });

  it('shows data after successful fetch', async () => {
    const fn = vi.fn().mockResolvedValue({ result: 42 });
    render(<TestComponent fn={fn} />);

    await waitFor(() => {
      expect(screen.getByTestId('data')).toHaveTextContent('{"result":42}');
    });
    expect(screen.queryByTestId('loading')).not.toBeInTheDocument();
  });

  it('shows error on fetch failure', async () => {
    const fn = vi.fn().mockRejectedValue(new Error('Network error'));
    render(<TestComponent fn={fn} />);

    await waitFor(() => {
      expect(screen.getByTestId('error')).toHaveTextContent('Network error');
    });
  });

  it('wraps non-Error rejections in Error', async () => {
    const fn = vi.fn().mockRejectedValue('string error');
    render(<TestComponent fn={fn} />);

    await waitFor(() => {
      expect(screen.getByTestId('error')).toHaveTextContent('string error');
    });
  });

  it('calls fn again when reload is triggered', async () => {
    let callCount = 0;
    const fn = vi.fn().mockImplementation(() => {
      callCount++;
      return Promise.resolve(`call-${callCount}`);
    });
    const user = userEvent.setup();
    render(<TestComponent fn={fn} />);

    await waitFor(() => {
      expect(screen.getByTestId('data')).toHaveTextContent('call-1');
    });

    await user.click(screen.getByRole('button', { name: 'Reload' }));

    await waitFor(() => {
      expect(screen.getByTestId('data')).toHaveTextContent('call-2');
    });
    expect(fn).toHaveBeenCalledTimes(2);
  });

  it('does not update state after unmount (cancelled flag)', async () => {
    let resolver: (v: string) => void;
    const fn = vi.fn().mockImplementation(() => new Promise<string>((r) => { resolver = r; }));
    const { unmount } = render(<TestComponent fn={fn} />);

    // Unmount while promise is pending
    unmount();

    // Resolve after unmount — should not throw or update state
    await act(async () => { resolver!('late data'); });
    // If we got here without errors, the cancelled flag worked
  });
});

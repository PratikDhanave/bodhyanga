import { describe, it, expect, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import ReviewDetail from '../ReviewDetail';
import { ToastProvider } from '../../components/Toast';
import { mockFetch } from '../../test/setup';

function routeFor(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input.toString();
}

function renderAt(id: string) {
  return render(
    <MemoryRouter initialEntries={[`/reviews/${id}`]}>
      <ToastProvider>
        <Routes>
          <Route path="/reviews/:id" element={<ReviewDetail />} />
          <Route path="/reviews" element={<div>Reviews list</div>} />
        </Routes>
      </ToastProvider>
    </MemoryRouter>,
  );
}

describe('ReviewDetail', () => {
  it('submits POST /api/reviews/{id}/decide with approve when Approve clicked and confirmed', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const fetchFn = mockFetch((input, init) => {
      const url = routeFor(input);
      const method = (init?.method || 'GET').toUpperCase();
      if (url.endsWith('/api/reviews/r1') && method === 'GET') {
        return {
          id: 'r1',
          kind: 'sign_off',
          status: 'pending',
          subject: 'Approve cap dx',
          submitted_at: '2026-01-01T00:00:00Z',
        };
      }
      if (url.endsWith('/api/reviews/r1/decide') && method === 'POST') {
        return {
          id: 'r1',
          kind: 'sign_off',
          status: 'approved',
          subject: 'Approve cap dx',
          submitted_at: '2026-01-01T00:00:00Z',
        };
      }
      return {};
    });

    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    renderAt('r1');

    // Wait for the form to render.
    const reviewerInput = await screen.findByDisplayValue('clinician-demo');
    await user.clear(reviewerInput);
    await user.type(reviewerInput, 'dr-foo');

    // Click Approve — opens confirmation modal
    await user.click(screen.getByRole('button', { name: 'Approve' }));

    // Confirmation modal should show
    expect(screen.getByText('Confirm Clinical Decision')).toBeInTheDocument();

    // Wait for the 2-second safety delay
    await act(async () => { vi.advanceTimersByTime(2100); });

    // Click confirm
    const confirmBtn = screen.getByRole('button', { name: /Confirm Approve/ });
    await user.click(confirmBtn);

    // Find the POST call.
    const postCall = fetchFn.mock.calls.find((c) => {
      const init = c[1] as RequestInit | undefined;
      return (init?.method || 'GET').toUpperCase() === 'POST';
    });
    expect(postCall).toBeDefined();
    const [postUrl, postInit] = postCall!;
    expect(routeFor(postUrl as RequestInfo | URL)).toBe('/api/reviews/r1/decide');
    const body = JSON.parse((postInit as RequestInit).body as string);
    expect(body).toEqual(
      expect.objectContaining({
        reviewer_id: 'dr-foo',
        decision: 'approved',
      }),
    );
    vi.useRealTimers();
  });

  it('disables modification button until textarea has content', async () => {
    mockFetch((input) => {
      const url = routeFor(input);
      if (url.endsWith('/api/reviews/r1')) {
        return {
          id: 'r1',
          kind: 'sign_off',
          status: 'pending',
          subject: 'Approve cap dx',
          submitted_at: '2026-01-01T00:00:00Z',
        };
      }
      return {};
    });

    const user = userEvent.setup();
    renderAt('r1');

    const modBtn = await screen.findByRole('button', {
      name: 'Approve with modifications',
    });
    expect(modBtn).toBeDisabled();

    // Find the modification textarea by its label text.
    const modLabel = await screen.findByText(/Modified payload/i);
    const modTextarea = modLabel.parentElement!.querySelector('textarea')!;
    // userEvent treats `{` as a key modifier; escape with `{{`.
    await user.type(modTextarea, '{{"diagnosis":"changed"}');

    expect(modBtn).toBeEnabled();
  });
});

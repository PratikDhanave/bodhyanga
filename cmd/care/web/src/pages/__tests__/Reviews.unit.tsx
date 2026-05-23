import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import Reviews from '../Reviews';
import { mockFetch } from '../../test/setup';

function routeFor(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input.toString();
}

function pendingReview(id: string) {
  return {
    id,
    kind: 'sign_off',
    status: 'pending' as const,
    subject: `Pending review ${id}`,
    submitted_at: '2026-01-01T00:00:00Z',
    priority: 'normal',
  };
}

function decidedReview(id: string) {
  return {
    id,
    kind: 'sign_off',
    status: 'approved' as const,
    subject: `Decided review ${id}`,
    submitted_at: '2026-01-01T00:00:00Z',
    priority: 'normal',
  };
}

describe('Reviews', () => {
  it('pending scope is default and lists pending reviews', async () => {
    mockFetch((input) => {
      const url = routeFor(input);
      if (url.endsWith('/api/reviews?status=pending')) {
        return [pendingReview('p1'), pendingReview('p2')];
      }
      if (url.endsWith('/api/reviews')) {
        return [
          pendingReview('p1'),
          pendingReview('p2'),
          decidedReview('d1'),
          decidedReview('d2'),
          decidedReview('d3'),
        ];
      }
      return [];
    });

    render(
      <MemoryRouter>
        <Reviews />
      </MemoryRouter>,
    );

    expect(await screen.findByText('Pending review p1')).toBeInTheDocument();
    expect(await screen.findByText('Pending review p2')).toBeInTheDocument();
    expect(screen.queryByText('Decided review d1')).not.toBeInTheDocument();
  });

  it('clicking "All" switches scope and refetches', async () => {
    const fetchFn = mockFetch((input) => {
      const url = routeFor(input);
      if (url.endsWith('/api/reviews?status=pending')) {
        return [pendingReview('p1'), pendingReview('p2')];
      }
      if (url.endsWith('/api/reviews')) {
        return [
          pendingReview('p1'),
          pendingReview('p2'),
          decidedReview('d1'),
          decidedReview('d2'),
          decidedReview('d3'),
        ];
      }
      return [];
    });

    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <Reviews />
      </MemoryRouter>,
    );

    // Wait for the initial pending load to render.
    await screen.findByText('Pending review p1');

    await user.click(screen.getByRole('button', { name: 'All' }));

    // Expect 5 rows now (the all-scope payload).
    expect(await screen.findByText('Decided review d1')).toBeInTheDocument();
    expect(await screen.findByText('Decided review d2')).toBeInTheDocument();
    expect(await screen.findByText('Decided review d3')).toBeInTheDocument();

    const calls = fetchFn.mock.calls.map((c) => routeFor(c[0] as RequestInfo | URL));
    expect(calls).toContain('/api/reviews');
    expect(calls.filter((u) => u === '/api/reviews').length).toBeGreaterThanOrEqual(1);
  });
});

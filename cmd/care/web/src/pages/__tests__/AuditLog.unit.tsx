import { describe, it, expect } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import AuditLog from '../AuditLog';
import { mockFetch } from '../../test/setup';
import I18nProvider from '../../i18n/I18nProvider';

const EVENTS = [
  { id: 'a1', ts: '2026-06-06T08:00:00Z', agent: 'diagnostician', kind: 'diagnosis', case_id: 'ab12cd34', decision: 'approved', reason: 'CHF confirmed' },
  { id: 'a2', ts: '2026-06-06T09:00:00Z', agent: 'intake', kind: 'intake_complete', case_id: 'ef56gh78' },
  { id: 'a3', ts: '2026-06-06T10:00:00Z', agent: 'care_planner', kind: 'care_plan', decision: 'rejected', reason: 'Missing labs' },
];

function routeFor(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input.toString();
}

function renderAuditLog() {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <AuditLog />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe('AuditLog', () => {
  it('loads and displays audit events on mount', async () => {
    mockFetch((input) => {
      const url = routeFor(input);
      if (url.includes('/api/audit')) return { events: EVENTS };
      return {};
    });

    renderAuditLog();
    const table = await screen.findByRole('table', { name: 'Audit log entries' });
    const view = within(table);
    expect(view.getByText('diagnostician')).toBeInTheDocument();
    expect(view.getByText('intake')).toBeInTheDocument();
    expect(view.getByText('care_planner')).toBeInTheDocument();
  });

  it('shows decision badges with correct style', async () => {
    mockFetch(() => ({ events: EVENTS }));
    renderAuditLog();

    const table = await screen.findByRole('table', { name: 'Audit log entries' });
    const view = within(table);
    const approved = view.getByText('approved');
    expect(approved.className).toContain('badge-approved');

    const rejected = view.getByText('rejected');
    expect(rejected.className).toContain('badge-rejected');
  });

  it('shows empty state when no events match', async () => {
    mockFetch(() => ({ events: [] }));
    renderAuditLog();

    await waitFor(() => {
      expect(screen.getByText('No audit events found.')).toBeInTheDocument();
    });
  });

  it('shows error state and retry button', async () => {
    mockFetch(() => { throw new Error('Server error'); });
    renderAuditLog();

    expect(await screen.findByText('Something went wrong.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });

  it('renders filter inputs for Case ID, Kind, and Agent', async () => {
    mockFetch(() => ({ events: EVENTS }));
    renderAuditLog();

    const table = await screen.findByRole('table', { name: 'Audit log entries' });
    await within(table).findByText('diagnostician');
    expect(screen.getByPlaceholderText('e.g. a3f1b9c2')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('e.g. diagnosis, review_request')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('e.g. diagnostician')).toBeInTheDocument();
  });

  it('handles plain array response format', async () => {
    // The endpoint may return a plain array instead of {events: [...]}
    mockFetch(() => EVENTS);
    renderAuditLog();

    const table = await screen.findByRole('table', { name: 'Audit log entries' });
    expect(await within(table).findByText('diagnostician')).toBeInTheDocument();
  });

  it('shows Load more button when cursor exists', async () => {
    mockFetch(() => ({
      events: Array.from({ length: 50 }, (_, i) => ({
        id: `e${i}`,
        ts: '2026-06-06T08:00:00Z',
        agent: `agent-${i}`,
        kind: 'test',
      })),
      next_cursor: 'cursor123',
    }));
    renderAuditLog();

    expect(await screen.findByRole('button', { name: 'Load more' })).toBeInTheDocument();
  });

  it('clears filters when Clear button is clicked', async () => {
    mockFetch(() => ({ events: EVENTS }));
    const user = userEvent.setup();
    renderAuditLog();

    const table = await screen.findByRole('table', { name: 'Audit log entries' });
    await within(table).findByText('diagnostician');

    // Type a filter
    const caseInput = screen.getByPlaceholderText('e.g. a3f1b9c2');
    await user.type(caseInput, 'ab12cd34');

    // Click Clear
    await user.click(screen.getByRole('button', { name: 'Clear' }));
    expect(caseInput).toHaveValue('');
  });
});

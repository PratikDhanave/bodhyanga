import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Dashboard from '../Dashboard';
import { mockFetch } from '../../test/setup';
import I18nProvider from '../../i18n/I18nProvider';

function routeFor(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input.toString();
}

describe('Dashboard', () => {
  it('renders four stat tiles with counts from /api/*', async () => {
    mockFetch((input) => {
      const url = routeFor(input);
      if (url.endsWith('/api/agents')) {
        return [
          { id: 'a1', name: 'A1', capabilities: [] },
          { id: 'a2', name: 'A2', capabilities: [] },
          { id: 'a3', name: 'A3', capabilities: [] },
        ];
      }
      if (url.endsWith('/api/tasks')) {
        return [
          { id: 't1', kind: 'k', patient_id: 'p1', priority: 'normal', status: 'open', subject: 'S1', due_at: '', created_at: '' },
          { id: 't2', kind: 'k', patient_id: 'p2', priority: 'normal', status: 'open', subject: 'S2', due_at: '', created_at: '' },
          { id: 't3', kind: 'k', patient_id: 'p3', priority: 'normal', status: 'done', subject: 'S3', due_at: '', created_at: '' },
        ];
      }
      if (url.endsWith('/api/reviews?status=pending')) {
        return [
          {
            id: 'r1',
            kind: 'sign_off',
            status: 'pending',
            subject: 'Sign off diagnosis: CAP',
            submitted_at: '2026-01-01T00:00:00Z',
          },
        ];
      }
      if (url.endsWith('/api/patients')) {
        return [
          { patient_id: 'p1' },
          { patient_id: 'p2' },
          { patient_id: 'p3' },
          { patient_id: 'p4' },
          { patient_id: 'p5' },
        ];
      }
      return [];
    });

    render(
      <I18nProvider>
        <MemoryRouter>
          <Dashboard />
        </MemoryRouter>
      </I18nProvider>,
    );

    // Stat tiles render label + value inside a Link container.
    const agentsTile = (await screen.findByText('Registered agents')).closest('a');
    expect(agentsTile).toHaveTextContent('3');

    const reviewsTile = (await screen.findByText('Pending HITL reviews')).closest('a');
    expect(reviewsTile).toHaveTextContent('1');

    const tasksTile = (await screen.findByText('Open nurse tasks')).closest('a');
    expect(tasksTile).toHaveTextContent('2');

    const patientsTile = (await screen.findByText('Patients in registry')).closest('a');
    expect(patientsTile).toHaveTextContent('5');
  });

  it('shows empty-state hints when lists are empty', async () => {
    mockFetch((input) => {
      const url = routeFor(input);
      if (url.endsWith('/api/agents')) return [];
      if (url.endsWith('/api/tasks')) return [];
      if (url.endsWith('/api/reviews?status=pending')) return [];
      if (url.endsWith('/api/patients')) return [];
      return [];
    });

    render(
      <I18nProvider>
        <MemoryRouter>
          <Dashboard />
        </MemoryRouter>
      </I18nProvider>,
    );

    expect(await screen.findByText('No pending reviews.')).toBeInTheDocument();
    expect(await screen.findByText('No open tasks.')).toBeInTheDocument();
  });

  it('shows pending review with the right status badge', async () => {
    mockFetch((input) => {
      const url = routeFor(input);
      if (url.endsWith('/api/agents')) return [];
      if (url.endsWith('/api/tasks')) return [];
      if (url.endsWith('/api/reviews?status=pending')) {
        return [
          {
            id: 'r1',
            kind: 'sign_off',
            status: 'pending',
            subject: 'Sign off diagnosis: CAP',
            submitted_at: '2026-01-01T00:00:00Z',
          },
        ];
      }
      if (url.endsWith('/api/patients')) return [];
      return [];
    });

    render(
      <I18nProvider>
        <MemoryRouter>
          <Dashboard />
        </MemoryRouter>
      </I18nProvider>,
    );

    expect(await screen.findByText('Sign off diagnosis: CAP')).toBeInTheDocument();
    // The badge text "pending" should appear inside the badge span.
    const badge = await screen.findByText('pending');
    expect(badge).toHaveClass('badge-pending');
  });
});

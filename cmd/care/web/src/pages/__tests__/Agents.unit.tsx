import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Agents from '../Agents';
import { mockFetch } from '../../test/setup';
import I18nProvider from '../../i18n/I18nProvider';

describe('Agents', () => {
  it('renders one row per agent with capabilities as badges', async () => {
    mockFetch(() => [
      { id: 'intake', name: 'Intake', capabilities: ['ingest', 'triage'] },
      { id: 'diag', name: 'Diag', capabilities: ['dx'] },
    ]);

    render(
      <I18nProvider>
        <MemoryRouter>
          <Agents />
        </MemoryRouter>
      </I18nProvider>,
    );

    const table = await screen.findByRole('table', { name: 'Registered agents' });
    const view = within(table);
    expect(view.getByText('intake')).toBeInTheDocument();
    expect(view.getByText('diag')).toBeInTheDocument();
    expect(view.getByText('ingest')).toBeInTheDocument();
    expect(view.getByText('triage')).toBeInTheDocument();
    expect(view.getByText('dx')).toBeInTheDocument();
  });

  it('shows empty state when no agents', async () => {
    mockFetch(() => []);

    render(
      <I18nProvider>
        <MemoryRouter>
          <Agents />
        </MemoryRouter>
      </I18nProvider>,
    );

    expect(await screen.findByText('No agents registered')).toBeInTheDocument();
  });

  it('shows error retry button on fetch failure', async () => {
    mockFetch(
      () =>
        new Response('boom', {
          status: 500,
          headers: { 'Content-Type': 'text/plain' },
        }),
    );

    render(
      <I18nProvider>
        <MemoryRouter>
          <Agents />
        </MemoryRouter>
      </I18nProvider>,
    );

    expect(await screen.findByText('Something went wrong.')).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });
});

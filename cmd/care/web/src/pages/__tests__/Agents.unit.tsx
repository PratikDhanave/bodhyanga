import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Agents from '../Agents';
import { mockFetch } from '../../test/setup';

describe('Agents', () => {
  it('renders one row per agent with capabilities as badges', async () => {
    mockFetch(() => [
      { id: 'intake', name: 'Intake', capabilities: ['ingest', 'triage'] },
      { id: 'diag', name: 'Diag', capabilities: ['dx'] },
    ]);

    render(
      <MemoryRouter>
        <Agents />
      </MemoryRouter>,
    );

    expect(await screen.findByText('intake')).toBeInTheDocument();
    expect(await screen.findByText('diag')).toBeInTheDocument();
    expect(await screen.findByText('ingest')).toBeInTheDocument();
    expect(await screen.findByText('triage')).toBeInTheDocument();
    expect(await screen.findByText('dx')).toBeInTheDocument();
  });

  it('shows empty state when no agents', async () => {
    mockFetch(() => []);

    render(
      <MemoryRouter>
        <Agents />
      </MemoryRouter>,
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
      <MemoryRouter>
        <Agents />
      </MemoryRouter>,
    );

    expect(await screen.findByText('Something went wrong.')).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });
});

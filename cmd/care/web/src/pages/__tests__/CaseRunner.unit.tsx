import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import CaseRunner from '../CaseRunner';
import { mockFetch } from '../../test/setup';

function routeFor(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input.toString();
}

const fixture = {
  case_id: 'fx-cap-1',
  condition: 'cap',
  chief_complaint: 'Cough',
  hpi: 'hpi',
  gold_diagnosis: 'Community-acquired pneumonia',
  max_cost_usd: 500,
  file: 'cap.yaml',
};

describe('CaseRunner', () => {
  it('submits a custom presentation when no fixture selected', async () => {
    const fetchFn = mockFetch((input, init) => {
      const url = routeFor(input);
      const method = (init?.method || 'GET').toUpperCase();
      if (url.endsWith('/api/fixtures') && method === 'GET') return [fixture];
      if (url.endsWith('/api/runs') && method === 'POST') {
        return {
          run_id: 'run-1',
          case_id: 'custom',
          diagnostician: 'rule',
          started_at: '2026-01-01T00:00:00Z',
        };
      }
      return {};
    });

    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <CaseRunner />
      </MemoryRouter>,
    );

    // Wait for fixtures to load (select renders only then).
    await screen.findByRole('combobox', { name: /Fixture/i });

    await user.click(screen.getByRole('button', { name: 'Run case' }));

    const postCall = fetchFn.mock.calls.find((c) => {
      const init = c[1] as RequestInit | undefined;
      return (
        (init?.method || 'GET').toUpperCase() === 'POST' &&
        routeFor(c[0] as RequestInfo | URL) === '/api/runs'
      );
    });
    expect(postCall).toBeDefined();
    expect(routeFor(postCall![0] as RequestInfo | URL)).toBe('/api/runs');
    const body = JSON.parse((postCall![1] as RequestInit).body as string);
    expect(body).toEqual(
      expect.objectContaining({
        diagnostician: 'rule',
        presentation: expect.objectContaining({
          chief_complaint: 'Cough and fever',
          condition: 'cap',
        }),
      }),
    );
    expect(body.fixture).toBeUndefined();
  });

  it('submits with the fixture key when one is selected', async () => {
    const fetchFn = mockFetch((input, init) => {
      const url = routeFor(input);
      const method = (init?.method || 'GET').toUpperCase();
      if (url.endsWith('/api/fixtures') && method === 'GET') return [fixture];
      if (url.endsWith('/api/runs') && method === 'POST') {
        return {
          run_id: 'run-2',
          case_id: fixture.case_id,
          diagnostician: 'rule',
          started_at: '2026-01-01T00:00:00Z',
        };
      }
      return {};
    });

    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <CaseRunner />
      </MemoryRouter>,
    );

    const select = await screen.findByRole('combobox', { name: /Fixture/i });
    await user.selectOptions(select, fixture.case_id);
    await user.click(screen.getByRole('button', { name: 'Run case' }));

    const postCall = fetchFn.mock.calls.find((c) => {
      const init = c[1] as RequestInit | undefined;
      return (
        (init?.method || 'GET').toUpperCase() === 'POST' &&
        routeFor(c[0] as RequestInfo | URL) === '/api/runs'
      );
    });
    expect(postCall).toBeDefined();
    const body = JSON.parse((postCall![1] as RequestInit).body as string);
    expect(body).toEqual({
      fixture: fixture.case_id,
      diagnostician: 'rule',
    });
    expect(body.presentation).toBeUndefined();
  });
});

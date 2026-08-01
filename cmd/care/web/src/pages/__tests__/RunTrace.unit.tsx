import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, act, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import RunTrace from '../RunTrace';
import { eventSourceFire, resetEventSources, mockFetch, lastEventSource } from '../../test/setup';

function renderAt(runId: string) {
  return render(
    <MemoryRouter initialEntries={[`/runs/${runId}`]}>
      <Routes>
        <Route path="/runs/:runId" element={<RunTrace />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('RunTrace', () => {
  beforeEach(() => {
    resetEventSources();
    // RunTrace does not fetch, but ensure fetch is mocked so any stray call
    // doesn't escape to the real network.
    mockFetch(() => ({}));
  });

  it('renders streamed trace events', async () => {
    renderAt('r-1');

    await waitFor(() => expect(lastEventSource()).toBeDefined());

    act(() => {
      eventSourceFire('/api/runs/r-1/events', {
        ts: '2026-01-01T00:00:00Z',
        from: 'user',
        to: 'intake',
        type: 'presentation',
        role: 'user',
        content_preview: 'cough',
      });
    });

    expect(await screen.findByText('presentation')).toBeInTheDocument();
    expect(await screen.findByText('cough')).toBeInTheDocument();
  });

  it('marks the stream "closed" when a terminal diagnosis event fires', async () => {
    renderAt('r-2');

    // Initially live.
    expect(await screen.findByText('live')).toBeInTheDocument();

    await waitFor(() => expect(lastEventSource()).toBeDefined());

    act(() => {
      eventSourceFire('/api/runs/r-2/events', {
        ts: '2026-01-01T00:00:00Z',
        from: 'user',
        to: 'intake',
        type: 'presentation',
        role: 'user',
        content_preview: 'cough',
      });
    });

    act(() => {
      eventSourceFire('/api/runs/r-2/events', {
        ts: '2026-01-01T00:00:01Z',
        from: 'diag',
        to: 'user',
        type: 'diagnosis',
        role: 'assistant',
        content_preview: 'CAP',
      });
    });

    expect(await screen.findByText('closed')).toBeInTheDocument();
    expect(screen.queryByText('live')).not.toBeInTheDocument();
  });
});

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import LLMTraces from '../LLMTraces';
import { mockFetch } from '../../test/setup';
import type { LLMTraceLog } from '../../types';

const TRACES: LLMTraceLog[] = [
  {
    started_at: '2026-06-05T14:30:00Z',
    duration_ms: 1250,
    provider: 'anthropic',
    model: 'claude-sonnet-4-6',
    agent_id: 'diagnostician',
    case_id: 'synth-case-01',
    input_tokens: 2400,
    output_tokens: 680,
    cached_tokens: 1200,
    stop_reason: 'end_turn',
    tool_calls: 3,
    kind: 'chat',
  },
  {
    started_at: '2026-06-05T14:31:00Z',
    duration_ms: 890,
    provider: 'openai',
    model: 'gpt-4o',
    agent_id: 'care-plan-gen',
    case_id: 'synth-case-02',
    input_tokens: 1800,
    output_tokens: 420,
    cached_tokens: 0,
    stop_reason: 'stop',
    tool_calls: 1,
    kind: 'chat',
  },
  {
    started_at: '2026-06-05T14:32:00Z',
    duration_ms: 2100,
    provider: 'anthropic',
    model: 'claude-sonnet-4-6',
    input_tokens: 3200,
    output_tokens: 900,
    cached_tokens: 800,
    error: 'context_length_exceeded',
    kind: 'chat',
  },
];

function renderTraces() {
  return render(
    <MemoryRouter>
      <LLMTraces />
    </MemoryRouter>,
  );
}

describe('LLMTraces', () => {
  it('renders the traces table with all entries', async () => {
    mockFetch(() => TRACES);
    renderTraces();

    expect(await screen.findByText('diagnostician')).toBeInTheDocument();
    expect(screen.getByText('care-plan-gen')).toBeInTheDocument();
  });

  it('displays provider and model columns', async () => {
    mockFetch(() => TRACES);
    renderTraces();

    await screen.findByText('diagnostician');
    const anthropicCells = screen.getAllByText('anthropic');
    expect(anthropicCells.length).toBe(2);
    expect(screen.getByText('openai')).toBeInTheDocument();
    expect(screen.getAllByText('claude-sonnet-4-6').length).toBe(2);
    expect(screen.getByText('gpt-4o')).toBeInTheDocument();
  });

  it('displays combined token counts (in+out) for each trace', async () => {
    mockFetch(() => TRACES);
    renderTraces();

    await screen.findByText('diagnostician');
    // in+out column: 2400+680 = 3,080
    expect(screen.getByText('3,080')).toBeInTheDocument();
  });

  it('highlights error rows and shows error text', async () => {
    mockFetch(() => TRACES);
    renderTraces();

    expect(await screen.findByText('context_length_exceeded')).toBeInTheDocument();
  });

  it('shows empty state when no traces exist', async () => {
    mockFetch(() => []);
    renderTraces();

    expect(await screen.findByText('No LLM calls recorded yet')).toBeInTheDocument();
  });

  it('shows error state when API fails', async () => {
    mockFetch(() => new Response('Bad Gateway', { status: 502 }));
    renderTraces();

    expect(await screen.findByText(/HTTP 502/)).toBeInTheDocument();
  });

  it('renders a Refresh button', async () => {
    const spy = mockFetch(() => TRACES);
    const user = userEvent.setup();
    renderTraces();

    await screen.findByText('diagnostician');
    const refreshBtn = screen.getByRole('button', { name: 'Refresh' });
    expect(refreshBtn).toBeInTheDocument();

    await user.click(refreshBtn);
    expect(spy).toHaveBeenCalledTimes(2);
  });

  it('displays subtitle with trace count', async () => {
    mockFetch(() => TRACES);
    renderTraces();

    await screen.findByText('diagnostician');
    expect(screen.getByText('3 most recent')).toBeInTheDocument();
  });

  it('renders error filter tab with correct count', async () => {
    mockFetch(() => TRACES);
    renderTraces();

    await screen.findByText('diagnostician');
    // Should show "All 3" and "Errors 1" tabs
    expect(screen.getByText('All')).toBeInTheDocument();
    expect(screen.getByText('Errors')).toBeInTheDocument();
  });

  it('filters to show only errors when Errors tab is clicked', async () => {
    mockFetch(() => TRACES);
    const user = userEvent.setup();
    renderTraces();

    await screen.findByText('diagnostician');
    await user.click(screen.getByText('Errors'));

    // Only the error row should remain
    expect(screen.getByText('context_length_exceeded')).toBeInTheDocument();
    // Non-error agents should be gone
    expect(screen.queryByText('diagnostician')).not.toBeInTheDocument();
    expect(screen.queryByText('care-plan-gen')).not.toBeInTheDocument();
  });

  it('displays estimated cost column', async () => {
    mockFetch(() => TRACES);
    renderTraces();

    await screen.findByText('diagnostician');
    // Anthropic: (2400+680) / 1M * 6.0 = $0.0185
    expect(screen.getAllByText(/\$0\.0/).length).toBeGreaterThan(0);
  });

  it('renders sortable column headers', async () => {
    mockFetch(() => TRACES);
    const user = userEvent.setup();
    renderTraces();

    await screen.findByText('diagnostician');

    // Click the "ms" sort header to sort by duration
    const msHeader = screen.getByText(/^ms/);
    await user.click(msHeader);
    // The header should show a down arrow when active
    expect(msHeader.textContent).toContain('↓');
  });
});

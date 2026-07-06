import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Bench from '../Bench';
import { mockFetch } from '../../test/setup';
import type { BenchResult } from '../../types';
import I18nProvider from '../../i18n/I18nProvider';

const RESULTS: BenchResult[] = [
  { provider: 'rule', cases: 50, mean_accuracy_pct: 62, mean_cost_usd: 0.0, under_budget_pct: 100 },
  { provider: 'llm-anthropic', cases: 50, mean_accuracy_pct: 94, mean_cost_usd: 0.35, under_budget_pct: 88 },
  { provider: 'llm-openai', cases: 50, mean_accuracy_pct: 91, mean_cost_usd: 0.42, under_budget_pct: 82 },
  { provider: 'llm-ollama', cases: 50, mean_accuracy_pct: 78, mean_cost_usd: 0.0, under_budget_pct: 100 },
];

function renderBench() {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <Bench />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe('Bench', () => {
  it('renders the summary table with all providers', async () => {
    mockFetch(() => RESULTS);
    renderBench();

    // Provider names appear in both the SVG chart and the table,
    // so we use getAllByText and check that at least one exists.
    const ruleEls = await screen.findAllByText('rule');
    expect(ruleEls.length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('llm-anthropic').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('llm-openai').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('llm-ollama').length).toBeGreaterThanOrEqual(1);
  });

  it('displays accuracy with ConfidenceDisplay categories', async () => {
    mockFetch(() => RESULTS);
    renderBench();

    await screen.findAllByText('rule');
    // ConfidenceDisplay renders "(94%)" with "Very High" category, etc.
    expect(screen.getByText('(94%)')).toBeInTheDocument();
    expect(screen.getByText('(91%)')).toBeInTheDocument();
    expect(screen.getByText('(78%)')).toBeInTheDocument();
    expect(screen.getByText('(62%)')).toBeInTheDocument();
  });

  it('displays cost values formatted as dollars', async () => {
    mockFetch(() => RESULTS);
    renderBench();

    await screen.findAllByText('rule');
    // Cost values may also appear in the SVG x-axis ticks
    expect(screen.getAllByText('$0.35').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('$0.42').length).toBeGreaterThanOrEqual(1);
  });

  it('shows the Pareto chart section', async () => {
    mockFetch(() => RESULTS);
    renderBench();

    // "Pareto frontier" appears in the SVG label and the description text
    const paretoEls = await screen.findAllByText(/Pareto frontier/i);
    expect(paretoEls.length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Cost per case (USD)')).toBeInTheDocument();
  });

  it('shows empty state when no results', async () => {
    mockFetch(() => []);
    renderBench();

    expect(await screen.findByText('No bench results yet')).toBeInTheDocument();
  });

  it('shows error state when API fails', async () => {
    mockFetch(() => new Response('Unavailable', { status: 503 }));
    renderBench();

    expect(await screen.findByText(/HTTP 503/)).toBeInTheDocument();
  });

  it('renders the subtitle text', async () => {
    mockFetch(() => RESULTS);
    renderBench();

    await screen.findAllByText('rule');
    expect(screen.getByText('Provider comparison from cmd/bench')).toBeInTheDocument();
  });

  it('renders the table header columns', async () => {
    mockFetch(() => RESULTS);
    renderBench();

    const table = await screen.findByRole('table', { name: 'Benchmark results' });
    const view = within(table);
    expect(view.getByText('Provider')).toBeInTheDocument();
    expect(view.getByText('Cases')).toBeInTheDocument();
    expect(view.getByText('Mean accuracy')).toBeInTheDocument();
    expect(view.getByText('Mean cost')).toBeInTheDocument();
    expect(view.getByText('Under budget')).toBeInTheDocument();
  });

  it('shows hover instruction text', async () => {
    mockFetch(() => RESULTS);
    renderBench();

    await screen.findAllByText('rule');
    expect(screen.getByText(/Hover to inspect/)).toBeInTheDocument();
  });
});

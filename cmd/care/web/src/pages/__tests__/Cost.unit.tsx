import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

// Cost.tsx calls api.costAnalytics() which is not yet defined in the api
// client (pre-existing TS issue). We mock the entire api module so the
// component can render.

const costAnalyticsMock = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    costAnalytics: (...args: unknown[]) => costAnalyticsMock(...args),
  },
}));

// Import after mock is set up (vi.mock is hoisted automatically)
import Cost from '../Cost';

const COST_DATA = {
  today_usd: 12.50,
  month_usd: 185.30,
  cost_per_case_usd: 0.245,
  savings_vs_cloud_usd: 22.10,
  providers: [
    { provider: 'anthropic', spend_usd: 95.0, calls: 1200, tokens: 1800000 },
    { provider: 'openai', spend_usd: 65.3, calls: 800, tokens: 1100000 },
    { provider: 'ollama', spend_usd: 0, calls: 450, tokens: 600000 },
  ],
  daily: [
    { date: '2026-06-01', spend_usd: 15.2 },
    { date: '2026-06-02', spend_usd: 18.4 },
    { date: '2026-06-03', spend_usd: 12.1 },
  ],
  top_agents: [
    { agent: 'diagnostician-llm', calls: 600, tokens: 900000, cost_usd: 45.0 },
    { agent: 'care-plan-gen', calls: 300, tokens: 400000, cost_usd: 18.5 },
  ],
  daily_budget_usd: 10,
};

function renderCost() {
  return render(
    <MemoryRouter>
      <Cost />
    </MemoryRouter>,
  );
}

describe('Cost', () => {
  beforeEach(() => {
    costAnalyticsMock.mockReset();
  });

  it('renders with stub data when API returns nothing', async () => {
    costAnalyticsMock.mockResolvedValue(null);
    renderCost();

    // Stub data always renders the page title and summary cards
    expect(await screen.findByText('Cost analytics')).toBeInTheDocument();
    expect(screen.getByText('Spend today')).toBeInTheDocument();
    expect(screen.getByText('This month')).toBeInTheDocument();
    expect(screen.getByText('Cost per case')).toBeInTheDocument();
    expect(screen.getByText('Savings vs cloud')).toBeInTheDocument();
  });

  it('renders summary cards with API data', async () => {
    costAnalyticsMock.mockResolvedValue(COST_DATA);
    renderCost();

    expect(await screen.findByText('$12.50')).toBeInTheDocument();
    expect(screen.getByText('$185.30')).toBeInTheDocument();
    expect(screen.getByText('$0.245')).toBeInTheDocument();
    expect(screen.getByText('$22.10')).toBeInTheDocument();
  });

  it('renders spend-by-provider bars', async () => {
    costAnalyticsMock.mockResolvedValue(COST_DATA);
    renderCost();

    expect(await screen.findByText('Spend by provider')).toBeInTheDocument();
    expect(screen.getByText('anthropic')).toBeInTheDocument();
    expect(screen.getByText('openai')).toBeInTheDocument();
    expect(screen.getByText('ollama')).toBeInTheDocument();
  });

  it('renders the top agents table', async () => {
    costAnalyticsMock.mockResolvedValue(COST_DATA);
    renderCost();

    expect(await screen.findByText('Top agents by spend')).toBeInTheDocument();
    expect(screen.getByText('diagnostician-llm')).toBeInTheDocument();
    expect(screen.getByText('care-plan-gen')).toBeInTheDocument();
    expect(screen.getByText('$45.00')).toBeInTheDocument();
    expect(screen.getByText('$18.50')).toBeInTheDocument();
  });

  it('shows budget exceeded alert when today exceeds budget', async () => {
    costAnalyticsMock.mockResolvedValue(COST_DATA);
    renderCost();

    expect(await screen.findByText('Daily budget exceeded')).toBeInTheDocument();
    expect(screen.getByText(/Spent \$12.50 today/)).toBeInTheDocument();
  });

  it('does not show budget alert when under budget', async () => {
    const underBudget = { ...COST_DATA, today_usd: 5.0 };
    costAnalyticsMock.mockResolvedValue(underBudget);
    renderCost();

    await screen.findByText('$5.00');
    expect(screen.queryByText('Daily budget exceeded')).not.toBeInTheDocument();
  });

  it('shows daily sparkline section', async () => {
    costAnalyticsMock.mockResolvedValue(COST_DATA);
    renderCost();

    expect(await screen.findByText(/Daily spend/)).toBeInTheDocument();
  });

  it('shows error state when API fails', async () => {
    costAnalyticsMock.mockRejectedValue(new Error('HTTP 500: Internal Server Error'));
    renderCost();

    // Cost always falls back to stub data, but the error box also renders
    expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
  });
});

import { useMemo } from 'react';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import CardTable from '../components/CardTable';
import type { CardColumn } from '../components/CardTable';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';
import type { CostAnalytics } from '../types/index';
import { useI18n } from '../i18n';
import { useTableKeyboardNav } from '../hooks/useTableKeyboardNav';

// Stub data used when the API returns nothing yet
const STUB: CostAnalytics = {
  today_usd: 1.42,
  month_usd: 28.76,
  cost_per_case_usd: 0.18,
  savings_vs_cloud_usd: 4.31,
  providers: [
    { provider: 'anthropic', spend_usd: 14.5, calls: 320, tokens: 480000 },
    { provider: 'openai', spend_usd: 9.2, calls: 210, tokens: 310000 },
    { provider: 'ollama', spend_usd: 0, calls: 87, tokens: 95000 },
  ],
  daily: [
    { date: '2026-05-25', spend_usd: 3.1 },
    { date: '2026-05-26', spend_usd: 2.8 },
    { date: '2026-05-27', spend_usd: 4.2 },
    { date: '2026-05-28', spend_usd: 3.6 },
    { date: '2026-05-29', spend_usd: 5.0 },
    { date: '2026-05-30', spend_usd: 4.7 },
    { date: '2026-05-31', spend_usd: 1.42 },
  ],
  top_agents: [
    { agent: 'diagnostician-llm-anthropic', calls: 210, tokens: 320000, cost_usd: 9.6 },
    { agent: 'care-plan-generator', calls: 85, tokens: 110000, cost_usd: 3.3 },
    { agent: 'test-planner', calls: 65, tokens: 78000, cost_usd: 2.34 },
    { agent: 'intake-agent', calls: 47, tokens: 42000, cost_usd: 1.26 },
  ],
  daily_budget_usd: 10,
};

const COST_COLUMNS: CardColumn[] = [
  { key: 'agent', label: 'Agent', showInCard: true, primary: true },
  { key: 'calls', label: 'Calls', showInCard: true },
  { key: 'tokens', label: 'Tokens', showInCard: true },
  { key: 'cost_usd', label: 'Cost', showInCard: true },
];

const PROVIDER_COLORS: Record<string, string> = {
  anthropic: '#6366f1',
  openai: '#10b981',
  ollama: '#94a3b8',
};

// ---- Spend-by-provider horizontal bar chart (pure CSS) ----
function ProviderBars({ providers }: { providers: CostAnalytics['providers'] }) {
  const max = Math.max(...providers.map((p) => p.spend_usd), 0.01);
  return (
    <div className="space-y-3">
      {providers.map((p) => {
        const pct = (p.spend_usd / max) * 100;
        const color = PROVIDER_COLORS[p.provider] ?? '#64748b';
        return (
          <div key={p.provider}>
            <div className="flex justify-between text-xs text-bodh-500 mb-1">
              <span className="font-mono capitalize">{p.provider}</span>
              <span>
                {p.spend_usd === 0 ? 'free' : `$${p.spend_usd.toFixed(2)}`}
                {' · '}
                {p.calls} calls
              </span>
            </div>
            <div className="h-3 bg-bodh-100 rounded-full overflow-hidden">
              <div
                className="h-full rounded-full transition-all duration-500"
                style={{ width: `${pct}%`, backgroundColor: color }}
              />
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ---- Daily spend sparkline (SVG polyline, last 7 days) ----
function DailySparkline({ daily }: { daily: CostAnalytics['daily'] }) {
  const W = 300;
  const H = 60;
  const PAD = 4;

  const points = useMemo(() => {
    if (daily.length === 0) return '';
    const vals = daily.map((d) => d.spend_usd);
    const minV = Math.min(...vals);
    const maxV = Math.max(...vals, minV + 0.01);
    const stepX = (W - PAD * 2) / Math.max(daily.length - 1, 1);
    return daily
      .map((d, i) => {
        const x = PAD + i * stepX;
        const y = PAD + ((maxV - d.spend_usd) / (maxV - minV)) * (H - PAD * 2);
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      })
      .join(' ');
  }, [daily]);

  const maxSpend = Math.max(...daily.map((d) => d.spend_usd), 0.01);
  const lastDay = daily[daily.length - 1];

  return (
    <div>
      <svg viewBox={`0 0 ${W} ${H}`} className="w-full" style={{ maxWidth: W }}>
        <polyline
          points={points}
          fill="none"
          stroke="#6366f1"
          strokeWidth="2"
          strokeLinejoin="round"
          strokeLinecap="round"
        />
        {/* area fill */}
        <polyline
          points={`${PAD},${H - PAD} ${points} ${W - PAD},${H - PAD}`}
          fill="#6366f1"
          fillOpacity="0.08"
          stroke="none"
        />
      </svg>
      <div className="flex justify-between text-xs text-bodh-400 mt-1">
        <span>{daily[0]?.date?.slice(5)}</span>
        <span className="text-bodh-600 font-medium">
          peak ${maxSpend.toFixed(2)} · today ${lastDay?.spend_usd.toFixed(2)}
        </span>
        <span>{lastDay?.date?.slice(5)}</span>
      </div>
    </div>
  );
}

// ---- Budget alert banner ----
function BudgetAlert({ today, budget, t }: { today: number; budget: number; t: (key: string) => string }) {
  if (today <= budget) return null;
  return (
    <div className="rounded-lg border border-amber-300 bg-amber-50 px-4 py-3 flex items-start gap-3">
      <span className="text-amber-600 text-lg leading-none">!</span>
      <div>
        <div className="font-semibold text-amber-800 text-sm">{t('cost.budgetExceeded')}</div>
        <div className="text-xs text-amber-700 mt-0.5">
          Spent ${today.toFixed(2)} today — budget is ${budget.toFixed(2)}. Consider routing
          low-complexity cases to Ollama (free, local).
        </div>
      </div>
    </div>
  );
}

// ---- Summary card ----
function SummaryCard({
  label,
  value,
  sub,
  highlight,
}: {
  label: string;
  value: string;
  sub?: string;
  highlight?: boolean;
}) {
  return (
    <div className={`card ${highlight ? 'border-green-300 bg-green-50' : ''}`}>
      <div className="card-body">
        <div className="text-xs uppercase tracking-wide text-bodh-400">{label}</div>
        <div className="text-2xl font-semibold mt-2">{value}</div>
        {sub && <div className="text-xs text-bodh-400 mt-1">{sub}</div>}
      </div>
    </div>
  );
}

export default function Cost() {
  const { t } = useI18n();
  const today = new Date();
  const from = new Date(today);
  from.setDate(from.getDate() - 6);
  const fmt = (d: Date) => d.toISOString().slice(0, 10);

  const { data: raw, loading, error, reload } = useApi(
    () => api.costAnalytics('default', fmt(from), fmt(today)),
    [],
  );

  // Fall back to stub so the page is always useful
  const data: CostAnalytics = raw ?? STUB;
  const { getRowProps } = useTableKeyboardNav(data.top_agents.length);

  return (
    <Page title={t('cost.title')} subtitle={t('cost.subtitle')}>
      {/* Synthetic data safeguard notice */}
      <div className="rounded-lg border border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-900/20 px-4 py-2 text-sm text-amber-800 dark:text-amber-300 flex items-center gap-2" role="status">
        <span className="font-bold">⚠</span>
        <span>Synthetic data — not connected to live billing systems</span>
      </div>

      {loading && <Loader what="cost data" />}
      {error && <ErrorBox error={error} retry={reload} />}

      {!raw && (
        <div className="rounded-lg border border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-900/20 px-4 py-2 text-sm text-blue-700 dark:text-blue-300 flex items-center gap-2">
          <span className="font-medium">{t('cost.demoData')}</span>
          <span className="text-blue-500 dark:text-blue-400">— wire <code className="font-mono text-xs bg-blue-100 dark:bg-blue-800 px-1 rounded">POST /api/cost/analytics</code> for real cost tracking</span>
        </div>
      )}

      <BudgetAlert today={data.today_usd} budget={data.daily_budget_usd} t={t} />

      {/* Summary cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <SummaryCard
          label={t('cost.spendToday')}
          value={`$${data.today_usd.toFixed(2)}`}
          sub={`Budget $${data.daily_budget_usd.toFixed(2)}`}
        />
        <SummaryCard
          label={t('cost.thisMonth')}
          value={`$${data.month_usd.toFixed(2)}`}
          sub={t('cost.thisMonth_sub')}
        />
        <SummaryCard
          label={t('cost.costPerCase')}
          value={`$${data.cost_per_case_usd.toFixed(3)}`}
          sub={t('cost.costPerCase_sub')}
        />
        <SummaryCard
          label={t('cost.savingsVsCloud')}
          value={`$${data.savings_vs_cloud_usd.toFixed(2)}`}
          sub={t('cost.savingsVsCloud_sub')}
          highlight
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Spend by provider */}
        <div className="card">
          <div className="card-header">{t('cost.spendByProvider')}</div>
          <div className="card-body">
            <ProviderBars providers={data.providers} />
          </div>
        </div>

        {/* Daily sparkline */}
        <div className="card">
          <div className="card-header">{t('cost.dailySpend')}</div>
          <div className="card-body">
            <DailySparkline daily={data.daily} />
          </div>
        </div>
      </div>

      {/* Top agents table */}
      <CardTable columns={COST_COLUMNS} data={data.top_agents as unknown as Record<string, unknown>[]} keyField="agent">
        <div className="card overflow-hidden">
          <div className="card-header">{t('cost.topAgents')}</div>
          <table className="min-w-full text-sm" aria-label="Cost breakdown by provider">
            <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
              <tr>
                <th className="px-4 py-2 text-left">{t('cost.col_agent')}</th>
                <th className="px-4 py-2 text-right">{t('cost.col_calls')}</th>
                <th className="px-4 py-2 text-right">{t('cost.col_tokens')}</th>
                <th className="px-4 py-2 text-right">{t('cost.col_cost')}</th>
              </tr>
            </thead>
            <tbody>
              {data.top_agents.map((a, i) => {
                const rp = getRowProps(i);
                return (
                <tr key={a.agent} {...rp} className={`border-t border-bodh-100 ${rp.className}`}>
                  <td className="px-4 py-2 font-mono text-xs">{a.agent}</td>
                  <td className="px-4 py-2 text-right">{a.calls.toLocaleString()}</td>
                  <td className="px-4 py-2 text-right">{a.tokens.toLocaleString()}</td>
                  <td className="px-4 py-2 text-right">${a.cost_usd.toFixed(2)}</td>
                </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </CardTable>
    </Page>
  );
}

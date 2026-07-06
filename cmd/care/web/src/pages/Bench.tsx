import { useMemo, useState } from 'react';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import ConfidenceDisplay from '../components/ConfidenceDisplay';
import CardTable from '../components/CardTable';
import type { CardColumn } from '../components/CardTable';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';
import type { BenchResult } from '../types/index';
import { useI18n } from '../i18n';
import { useTableKeyboardNav } from '../hooks/useTableKeyboardNav';

// ---- Pareto frontier helper ----
// A point P dominates Q if P has lower cost AND higher accuracy.
// The Pareto frontier is the set of non-dominated points, sorted by cost.
function paretoFrontier(points: BenchResult[]): BenchResult[] {
  const frontier: BenchResult[] = [];
  for (const p of points) {
    const dominated = points.some(
      (q) =>
        q !== p &&
        q.mean_cost_usd <= p.mean_cost_usd &&
        q.mean_accuracy_pct >= p.mean_accuracy_pct &&
        (q.mean_cost_usd < p.mean_cost_usd || q.mean_accuracy_pct > p.mean_accuracy_pct),
    );
    if (!dominated) frontier.push(p);
  }
  return frontier.slice().sort((a, b) => a.mean_cost_usd - b.mean_cost_usd);
}

// Colours per provider name (first word)
const PROVIDER_COLORS: Record<string, string> = {
  rule: '#94a3b8',
  'llm-anthropic': '#6366f1',
  'llm-openai': '#10b981',
  'llm-ollama': '#f59e0b',
};
function colorFor(provider: string): string {
  return PROVIDER_COLORS[provider] ?? '#64748b';
}

// ---- Color legend ----
function ColorLegend({ data }: { data: BenchResult[] }) {
  const providers = [...new Set(data.map((d) => d.provider))];
  return (
    <div className="flex flex-wrap gap-3 mt-2 text-xs">
      {providers.map((p) => (
        <div key={p} className="flex items-center gap-1.5">
          <span
            className="inline-block w-3 h-3 rounded-full border border-white dark:border-gray-700"
            style={{ backgroundColor: colorFor(p) }}
          />
          <span className="font-mono text-bodh-500 dark:text-gray-400">{p}</span>
        </div>
      ))}
    </div>
  );
}

// ---- Pareto scatter plot (pure SVG) with hover tooltip ----
function ParetoChart({ data, hovered, onHover, t }: { data: BenchResult[]; hovered: string | null; onHover: (p: string | null) => void; t: (key: string) => string }) {
  const W = 480;
  const H = 280;
  const PAD = { top: 20, right: 20, bottom: 40, left: 48 };

  const plotW = W - PAD.left - PAD.right;
  const plotH = H - PAD.top - PAD.bottom;

  const maxCost = Math.max(...data.map((d) => d.mean_cost_usd), 0.01);
  const maxAcc = 100;

  const toX = (cost: number) => PAD.left + (cost / maxCost) * plotW;
  const toY = (acc: number) => PAD.top + plotH - (acc / maxAcc) * plotH;

  const frontier = useMemo(() => paretoFrontier(data), [data]);
  const frontierPoints = frontier.map((p) => `${toX(p.mean_cost_usd)},${toY(p.mean_accuracy_pct)}`).join(' ');

  // axis tick helpers
  const xTicks = [0, 0.25, 0.5, 0.75, 1.0].map((f) => ({
    val: f * maxCost,
    x: PAD.left + f * plotW,
  }));
  const yTicks = [0, 25, 50, 75, 100].map((v) => ({
    val: v,
    y: toY(v),
  }));

  return (
    <div className="overflow-x-auto">
      <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', maxWidth: W, minWidth: 300 }} role="img" aria-label="Pareto frontier scatter plot of accuracy vs cost">
        {/* grid lines */}
        {yTicks.map((t) => (
          <line
            key={t.val}
            x1={PAD.left}
            y1={t.y}
            x2={W - PAD.right}
            y2={t.y}
            stroke="#e2e8f0"
            strokeWidth="1"
          />
        ))}

        {/* Pareto frontier line */}
        {frontier.length > 1 && (
          <polyline
            points={frontierPoints}
            fill="none"
            stroke="#6366f1"
            strokeWidth="1.5"
            strokeDasharray="4 3"
            opacity="0.6"
          />
        )}

        {/* scatter points */}
        {data.map((d) => {
          const x = toX(d.mean_cost_usd);
          const y = toY(d.mean_accuracy_pct);
          const color = colorFor(d.provider);
          const onFrontier = frontier.includes(d);
          const isHovered = hovered === d.provider;
          const dimmed = hovered !== null && !isHovered;
          return (
            <g
              key={d.provider}
              onMouseEnter={() => onHover(d.provider)}
              onMouseLeave={() => onHover(null)}
              className="cursor-pointer"
            >
              <circle
                cx={x}
                cy={y}
                r={isHovered ? 9 : onFrontier ? 7 : 5}
                fill={color}
                stroke={isHovered ? '#1e293b' : 'white'}
                strokeWidth={isHovered ? 2 : 1.5}
                opacity={dimmed ? 0.25 : onFrontier ? 1 : 0.6}
              />
              <text
                x={x}
                y={y - 12}
                fontSize="9"
                textAnchor="middle"
                fill="#475569"
                fontFamily="monospace"
                opacity={dimmed ? 0.25 : 1}
              >
                {d.provider}
              </text>
              {/* Tooltip on hover */}
              {isHovered && (
                <foreignObject x={x + 12} y={y - 40} width="140" height="50">
                  <div className="bg-gray-900 text-white text-[10px] rounded px-2 py-1 shadow-lg leading-tight">
                    <div className="font-semibold">{d.provider}</div>
                    <div>{t('bench.tooltip_accuracy')}: {d.mean_accuracy_pct.toFixed(1)}%</div>
                    <div>{t('bench.tooltip_cost')}: ${d.mean_cost_usd.toFixed(3)}</div>
                  </div>
                </foreignObject>
              )}
            </g>
          );
        })}

        {/* X axis */}
        <line x1={PAD.left} y1={H - PAD.bottom} x2={W - PAD.right} y2={H - PAD.bottom} stroke="#cbd5e1" strokeWidth="1" />
        {xTicks.map((t) => (
          <text key={t.val} x={t.x} y={H - PAD.bottom + 14} fontSize="9" textAnchor="middle" fill="#94a3b8">
            ${t.val.toFixed(2)}
          </text>
        ))}
        <text x={PAD.left + plotW / 2} y={H - 4} fontSize="10" textAnchor="middle" fill="#64748b">
          {t('bench.axis_costPerCase')}
        </text>

        {/* Y axis */}
        <line x1={PAD.left} y1={PAD.top} x2={PAD.left} y2={H - PAD.bottom} stroke="#cbd5e1" strokeWidth="1" />
        {yTicks.map((t) => (
          <text key={t.val} x={PAD.left - 6} y={t.y + 3} fontSize="9" textAnchor="end" fill="#94a3b8">
            {t.val}%
          </text>
        ))}

        {/* Pareto frontier label */}
        {frontier.length > 1 && (
          <text
            x={toX(frontier[0].mean_cost_usd) + 6}
            y={toY(frontier[0].mean_accuracy_pct) - 14}
            fontSize="8"
            fill="#6366f1"
            opacity="0.8"
          >
            {t('bench.pareto_label')}
          </text>
        )}
      </svg>
    </div>
  );
}

const BENCH_COLUMNS: CardColumn[] = [
  { key: 'provider', label: 'Provider', showInCard: true, primary: true },
  { key: 'cases', label: 'Cases', showInCard: true },
  { key: 'mean_accuracy_pct', label: 'Mean Accuracy', showInCard: true },
  { key: 'mean_cost_usd', label: 'Mean Cost', showInCard: true },
  { key: 'under_budget_pct', label: 'Under Budget', showInCard: true },
];

export default function Bench() {
  const { t } = useI18n();
  const { data, loading, error, reload } = useApi(() => api.bench(), []);
  const [hovered, setHovered] = useState<string | null>(null);
  const { getRowProps } = useTableKeyboardNav(data?.length ?? 0);

  if (loading) return <Page title={t('bench.title')}><Loader what="bench" /></Page>;
  if (error) return <Page title={t('bench.title')}><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return (
      <Page title={t('bench.title')}>
        <Empty
          title={t('bench.noResults')}
          hint={t('bench.noResults_hint')}
        />
      </Page>
    );
  }

  return (
    <Page title={t('bench.title')} subtitle={t('bench.subtitle')}>
      {/* Pareto chart */}
      <div className="card">
        <div className="card-header dark:border-gray-700 dark:text-gray-100">{t('bench.chart_title')}</div>
        <div className="card-body">
          <ParetoChart data={data} hovered={hovered} onHover={setHovered} t={t} />
          <ColorLegend data={data} />
          <p className="text-xs text-bodh-400 dark:text-gray-500 mt-2">
            {t('bench.chart_hint')}
          </p>
        </div>
      </div>

      {/* Summary table */}
      <CardTable columns={BENCH_COLUMNS} data={data as unknown as Record<string, unknown>[]} keyField="provider">
        <div className="card overflow-hidden">
          <table className="min-w-full text-sm" aria-label="Benchmark results">
            <thead className="bg-bodh-50 dark:bg-gray-800 text-bodh-400 dark:text-gray-500 text-xs uppercase tracking-wide">
              <tr>
                <th className="px-4 py-2 text-left">{t('bench.col_provider')}</th>
                <th className="px-4 py-2 text-right">{t('bench.col_cases')}</th>
                <th className="px-4 py-2 text-right">{t('bench.col_meanAccuracy')}</th>
                <th className="px-4 py-2 text-right">{t('bench.col_meanCost')}</th>
                <th className="px-4 py-2 text-right">{t('bench.col_underBudget')}</th>
              </tr>
            </thead>
            <tbody>
              {data.map((r, i) => {
                const rp = getRowProps(i);
                return (
                <tr
                  key={r.provider}
                  {...rp}
                  className={`border-t border-bodh-100 dark:border-gray-700 cursor-pointer transition-colors ${
                    hovered === r.provider
                      ? 'bg-bodh-50 dark:bg-gray-800'
                      : hovered !== null
                        ? 'opacity-40'
                        : ''
                  } ${rp.className}`}
                  onMouseEnter={() => setHovered(r.provider)}
                  onMouseLeave={() => setHovered(null)}
                >
                  <td className="px-4 py-2 font-mono dark:text-gray-100">
                    <span
                      className="inline-block w-2.5 h-2.5 rounded-full mr-2"
                      style={{ backgroundColor: colorFor(r.provider) }}
                    />
                    {r.provider}
                  </td>
                  <td className="px-4 py-2 text-right dark:text-gray-300">{r.cases}</td>
                  <td className="px-4 py-2 text-right dark:text-gray-300"><ConfidenceDisplay value={r.mean_accuracy_pct / 100} size="sm" showTooltip={false} /></td>
                  <td className="px-4 py-2 text-right dark:text-gray-300">${r.mean_cost_usd.toFixed(2)}</td>
                  <td className="px-4 py-2 text-right dark:text-gray-300">{r.under_budget_pct.toFixed(0)}%</td>
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

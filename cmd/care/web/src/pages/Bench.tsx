import { useMemo } from 'react';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { useLiveResource } from '../hooks/useLiveResource';
import { api } from '../api/client';
import type { BenchResult } from '../types/index';

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

// ---- Pareto scatter plot (pure SVG) ----
function ParetoChart({ data }: { data: BenchResult[] }) {
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
      <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', maxWidth: W, minWidth: 300 }}>
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
          return (
            <g key={d.provider}>
              <circle
                cx={x}
                cy={y}
                r={onFrontier ? 7 : 5}
                fill={color}
                stroke="white"
                strokeWidth="1.5"
                opacity={onFrontier ? 1 : 0.6}
              />
              <text
                x={x}
                y={y - 10}
                fontSize="9"
                textAnchor="middle"
                fill="#475569"
                fontFamily="monospace"
              >
                {d.provider}
              </text>
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
          Cost per case (USD)
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
            Pareto frontier
          </text>
        )}
      </svg>
    </div>
  );
}

export default function Bench() {
  const { data, loading, error, reload } = useLiveResource(() => api.bench(), [], []);
  if (loading) return <Page title="Bench results"><Loader what="bench" /></Page>;
  if (error) return <Page title="Bench results"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return (
      <Page title="Bench results">
        <Empty
          title="No bench results yet"
          hint="Run cmd/bench -providers=rule,llm-anthropic,llm-openai,llm-ollama -json > bench.json and POST it to /api/bench/results."
        />
      </Page>
    );
  }

  return (
    <Page title="Bench results" subtitle="Provider comparison from cmd/bench">
      {/* Pareto chart */}
      <div className="card">
        <div className="card-header">Cost vs accuracy — Pareto frontier</div>
        <div className="card-body">
          <ParetoChart data={data} />
          <p className="text-xs text-bodh-400 mt-2">
            Dashed line connects non-dominated providers. Filled circles are on the frontier.
          </p>
        </div>
      </div>

      {/* Summary table */}
      <div className="card overflow-hidden">
        <table className="min-w-full text-sm">
          <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
            <tr>
              <th className="px-4 py-2 text-left">Provider</th>
              <th className="px-4 py-2 text-right">Cases</th>
              <th className="px-4 py-2 text-right">Mean accuracy</th>
              <th className="px-4 py-2 text-right">Mean cost</th>
              <th className="px-4 py-2 text-right">Under budget</th>
            </tr>
          </thead>
          <tbody>
            {data.map((r) => (
              <tr key={r.provider} className="border-t border-bodh-100">
                <td className="px-4 py-2 font-mono">{r.provider}</td>
                <td className="px-4 py-2 text-right">{r.cases}</td>
                <td className="px-4 py-2 text-right">{r.mean_accuracy_pct.toFixed(0)}%</td>
                <td className="px-4 py-2 text-right">${r.mean_cost_usd.toFixed(2)}</td>
                <td className="px-4 py-2 text-right">{r.under_budget_pct.toFixed(0)}%</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Page>
  );
}

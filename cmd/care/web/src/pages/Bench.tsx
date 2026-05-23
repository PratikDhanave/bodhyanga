import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';

export default function Bench() {
  const { data, loading, error, reload } = useApi(() => api.bench(), []);
  if (loading) return <Page title="Bench results"><Loader what="bench" /></Page>;
  if (error) return <Page title="Bench results"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return <Page title="Bench results"><Empty title="No bench results yet" hint="Run cmd/bench -providers=rule,llm-anthropic,llm-openai,llm-ollama -json > bench.json and POST it to /api/bench/results." /></Page>;
  }

  return (
    <Page title="Bench results" subtitle="Provider comparison from cmd/bench">
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

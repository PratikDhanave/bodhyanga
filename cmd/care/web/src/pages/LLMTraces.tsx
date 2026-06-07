import { useMemo, useState } from 'react';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { Tabs } from '../components/Tabs';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';
import type { LLMTraceLog } from '../types';

// Rough per-token cost estimates by provider (USD per 1M tokens, blended in/out)
const COST_PER_M_TOKENS: Record<string, number> = {
  anthropic: 6.0,
  openai: 5.0,
  ollama: 0,
};

function estimateCost(t: LLMTraceLog): number {
  const rate = COST_PER_M_TOKENS[t.provider] ?? 3.0;
  return ((t.input_tokens + t.output_tokens) / 1_000_000) * rate;
}

type SortKey = 'time' | 'duration' | 'tokens' | 'cost';
const SORT_DIR: Record<SortKey, (a: LLMTraceLog, b: LLMTraceLog) => number> = {
  time: (a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime(),
  duration: (a, b) => b.duration_ms - a.duration_ms,
  tokens: (a, b) => (b.input_tokens + b.output_tokens) - (a.input_tokens + a.output_tokens),
  cost: (a, b) => estimateCost(b) - estimateCost(a),
};

export default function LLMTraces() {
  const { data, loading, error, reload } = useApi(() => api.llmTraces(100), []);
  const [filter, setFilter] = useState<'all' | 'errors'>('all');
  const [sortBy, setSortBy] = useState<SortKey>('time');

  const filtered = useMemo(() => {
    if (!data) return [];
    const list = filter === 'errors' ? data.filter((t) => !!t.error) : data;
    return [...list].sort(SORT_DIR[sortBy]);
  }, [data, filter, sortBy]);

  const errorCount = data?.filter((t) => !!t.error).length ?? 0;

  if (loading) return <Page title="LLM traces"><Loader what="llm traces" /></Page>;
  if (error) return <Page title="LLM traces"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return <Page title="LLM traces"><Empty title="No LLM calls recorded yet" hint="Set -diagnostician=llm-anthropic and run a case to populate." /></Page>;
  }

  function sortHeader(label: string, key: SortKey, align: 'left' | 'right' = 'left') {
    const active = sortBy === key;
    return (
      <th
        className={`px-4 py-2 text-${align} cursor-pointer hover:text-bodh-600 dark:hover:text-gray-200 select-none ${active ? 'text-bodh-700 dark:text-gray-200 underline' : ''}`}
        onClick={() => setSortBy(key)}
      >
        {label} {active && '↓'}
      </th>
    );
  }

  return (
    <Page
      title="LLM traces"
      subtitle={`${data.length} most recent`}
      actions={<button onClick={reload} className="btn-secondary">Refresh</button>}
    >
      <div className="flex items-center gap-3 mb-2">
        <Tabs
          tabs={[
            { key: 'all', label: 'All', count: data.length },
            { key: 'errors', label: 'Errors', count: errorCount },
          ]}
          active={filter}
          onChange={(k) => setFilter(k as 'all' | 'errors')}
          size="sm"
        />
      </div>

      <div className="card overflow-hidden">
        <table className="min-w-full text-sm">
          <thead className="bg-bodh-50 dark:bg-gray-800 text-bodh-400 dark:text-gray-500 text-xs uppercase tracking-wide">
            <tr>
              {sortHeader('Time', 'time')}
              <th className="px-4 py-2 text-left">Provider</th>
              <th className="px-4 py-2 text-left">Model</th>
              <th className="px-4 py-2 text-left">Agent</th>
              {sortHeader('in+out', 'tokens', 'right')}
              <th className="px-4 py-2 text-right">cached</th>
              <th className="px-4 py-2 text-right">tools</th>
              {sortHeader('ms', 'duration', 'right')}
              {sortHeader('est $', 'cost', 'right')}
              <th className="px-4 py-2 text-left">stop</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((t, i) => {
              const cost = estimateCost(t);
              return (
                <tr key={`${t.started_at}-${i}`} className={`border-t border-bodh-100 dark:border-gray-700 ${t.error ? 'bg-rose-50 dark:bg-rose-900/20' : ''}`}>
                  <td className="px-4 py-2 text-xs text-bodh-400 dark:text-gray-500">{new Date(t.started_at).toLocaleTimeString()}</td>
                  <td className="px-4 py-2 font-mono text-xs">{t.provider}</td>
                  <td className="px-4 py-2 font-mono text-xs">{t.model}</td>
                  <td className="px-4 py-2 font-mono text-xs">{t.agent_id ?? '–'}</td>
                  <td className="px-4 py-2 text-right">{(t.input_tokens + t.output_tokens).toLocaleString()}</td>
                  <td className="px-4 py-2 text-right">{t.cached_tokens}</td>
                  <td className="px-4 py-2 text-right">{t.tool_calls ?? 0}</td>
                  <td className="px-4 py-2 text-right">{t.duration_ms}</td>
                  <td className="px-4 py-2 text-right font-mono text-xs text-bodh-400 dark:text-gray-500">
                    {cost > 0 ? `$${cost.toFixed(4)}` : 'free'}
                  </td>
                  <td className="px-4 py-2 text-xs text-bodh-400 dark:text-gray-500">{t.stop_reason ?? t.error ?? '–'}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </Page>
  );
}

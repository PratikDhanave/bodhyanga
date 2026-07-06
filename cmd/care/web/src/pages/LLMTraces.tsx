import { useMemo, useState } from 'react';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { Tabs } from '../components/Tabs';
import CardTable from '../components/CardTable';
import type { CardColumn } from '../components/CardTable';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';
import { useI18n } from '../i18n';
import { useTableKeyboardNav } from '../hooks/useTableKeyboardNav';
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

const COLUMNS: CardColumn[] = [
  { key: 'started_at', label: 'Time', showInCard: true, primary: true },
  { key: 'provider', label: 'Provider', showInCard: true },
  { key: 'model', label: 'Model', showInCard: true },
  { key: 'agent_id', label: 'Agent', showInCard: true },
  { key: 'duration_ms', label: 'Duration (ms)', showInCard: true },
  { key: 'stop_reason', label: 'Stop Reason', showInCard: false },
];

type SortKey = 'time' | 'duration' | 'tokens' | 'cost';
const SORT_DIR: Record<SortKey, (a: LLMTraceLog, b: LLMTraceLog) => number> = {
  time: (a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime(),
  duration: (a, b) => b.duration_ms - a.duration_ms,
  tokens: (a, b) => (b.input_tokens + b.output_tokens) - (a.input_tokens + a.output_tokens),
  cost: (a, b) => estimateCost(b) - estimateCost(a),
};

export default function LLMTraces() {
  const { t } = useI18n();
  const { data, loading, error, reload } = useApi(() => api.llmTraces(100), []);
  const [filter, setFilter] = useState<'all' | 'errors'>('all');
  const [sortBy, setSortBy] = useState<SortKey>('time');

  const filtered = useMemo(() => {
    if (!data) return [];
    const list = filter === 'errors' ? data.filter((trace) => !!trace.error) : data;
    return [...list].sort(SORT_DIR[sortBy]);
  }, [data, filter, sortBy]);

  const errorCount = data?.filter((trace) => !!trace.error).length ?? 0;
  const { getRowProps } = useTableKeyboardNav(filtered.length);

  if (loading) return <Page title={t('llmTraces.title')}><Loader what="llm traces" /></Page>;
  if (error) return <Page title={t('llmTraces.title')}><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return <Page title={t('llmTraces.title')}><Empty title={t('llmTraces.noTraces')} hint={t('llmTraces.noTraces_hint')} /></Page>;
  }

  function sortHeader(label: string, key: SortKey, align: 'left' | 'right' = 'left', extraClass = '') {
    const active = sortBy === key;
    return (
      <th
        className={`px-4 py-2 text-${align} cursor-pointer hover:text-bodh-600 dark:hover:text-gray-200 select-none ${active ? 'text-bodh-700 dark:text-gray-200 underline' : ''} ${extraClass}`}
        onClick={() => setSortBy(key)}
      >
        {label} {active && '↓'}
      </th>
    );
  }

  return (
    <Page
      title={t('llmTraces.title')}
      subtitle={`${data.length} most recent`}
      actions={<button onClick={reload} className="btn-secondary">{t('llmTraces.refresh')}</button>}
    >
      <div className="flex items-center gap-3 mb-2">
        <Tabs
          tabs={[
            { key: 'all', label: t('llmTraces.tab_all'), count: data.length },
            { key: 'errors', label: t('llmTraces.tab_errors'), count: errorCount },
          ]}
          active={filter}
          onChange={(k) => setFilter(k as 'all' | 'errors')}
          size="sm"
        />
      </div>

      <CardTable columns={COLUMNS} data={filtered as unknown as Record<string, unknown>[]} keyField="started_at">
        <div className="card overflow-hidden">
          <table className="min-w-full text-sm" aria-label="LLM call traces">
            <thead className="bg-bodh-50 dark:bg-gray-800 text-bodh-400 dark:text-gray-500 text-xs uppercase tracking-wide">
              <tr>
                {sortHeader(t('llmTraces.col_time'), 'time')}
                <th className="px-4 py-2 text-left">{t('llmTraces.col_provider')}</th>
                <th className="px-4 py-2 text-left hidden lg:table-cell">{t('llmTraces.col_model')}</th>
                <th className="px-4 py-2 text-left">{t('llmTraces.col_agent')}</th>
                {sortHeader(t('llmTraces.col_tokens'), 'tokens', 'right', 'hidden md:table-cell')}
                <th className="px-4 py-2 text-right">{t('llmTraces.col_cached')}</th>
                <th className="px-4 py-2 text-right">{t('llmTraces.col_tools')}</th>
                {sortHeader(t('llmTraces.col_duration'), 'duration', 'right', 'hidden md:table-cell')}
                {sortHeader(t('llmTraces.col_estCost'), 'cost', 'right', 'hidden lg:table-cell')}
                <th className="px-4 py-2 text-left">{t('llmTraces.col_stop')}</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((trace, i) => {
                const cost = estimateCost(trace);
                const rp = getRowProps(i);
                return (
                  <tr key={`${trace.started_at}-${i}`} {...rp} className={`border-t border-bodh-100 dark:border-gray-700 ${trace.error ? 'bg-rose-50 dark:bg-rose-900/20' : ''} ${rp.className}`}>
                    <td className="px-4 py-2 text-xs text-bodh-400 dark:text-gray-500">{new Date(trace.started_at).toLocaleTimeString()}</td>
                    <td className="px-4 py-2 font-mono text-xs">{trace.provider}</td>
                    <td className="px-4 py-2 font-mono text-xs hidden lg:table-cell">{trace.model}</td>
                    <td className="px-4 py-2 font-mono text-xs">{trace.agent_id ?? '–'}</td>
                    <td className="px-4 py-2 text-right hidden md:table-cell">{(trace.input_tokens + trace.output_tokens).toLocaleString()}</td>
                    <td className="px-4 py-2 text-right">{trace.cached_tokens}</td>
                    <td className="px-4 py-2 text-right">{trace.tool_calls ?? 0}</td>
                    <td className="px-4 py-2 text-right hidden md:table-cell">{trace.duration_ms}</td>
                    <td className="px-4 py-2 text-right font-mono text-xs text-bodh-400 dark:text-gray-500 hidden lg:table-cell">
                      {cost > 0 ? `$${cost.toFixed(4)}` : t('llmTraces.free')}
                    </td>
                    <td className="px-4 py-2 text-xs text-bodh-400 dark:text-gray-500">{trace.stop_reason ?? trace.error ?? '–'}</td>
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

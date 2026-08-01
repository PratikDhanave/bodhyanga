import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { useLiveResource } from '../hooks/useLiveResource';
import { api } from '../api/client';

export default function LLMTraces() {
  const { data, loading, error, reload } = useLiveResource(() => api.llmTraces(100), [], ['diagnosis','diagnosis_proposal']);
  if (loading) return <Page title="LLM traces"><Loader what="llm traces" /></Page>;
  if (error) return <Page title="LLM traces"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return <Page title="LLM traces"><Empty title="No LLM calls recorded yet" hint="Set -diagnostician=llm-anthropic and run a case to populate." /></Page>;
  }
  return (
    <Page title="LLM traces" subtitle={`${data.length} most recent`} actions={<button onClick={reload} className="btn-secondary">Refresh</button>}>
      <div className="card overflow-hidden">
        <table className="min-w-full text-sm">
          <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
            <tr>
              <th className="px-4 py-2 text-left">Time</th>
              <th className="px-4 py-2 text-left">Provider</th>
              <th className="px-4 py-2 text-left">Model</th>
              <th className="px-4 py-2 text-left">Agent</th>
              <th className="px-4 py-2 text-right">in</th>
              <th className="px-4 py-2 text-right">out</th>
              <th className="px-4 py-2 text-right">cached</th>
              <th className="px-4 py-2 text-right">tools</th>
              <th className="px-4 py-2 text-right">ms</th>
              <th className="px-4 py-2 text-left">stop</th>
            </tr>
          </thead>
          <tbody>
            {data.map((t, i) => (
              <tr key={`${t.started_at}-${i}`} className={`border-t border-bodh-100 ${t.error ? 'bg-rose-50' : ''}`}>
                <td className="px-4 py-2 text-xs text-bodh-400">{new Date(t.started_at).toLocaleTimeString()}</td>
                <td className="px-4 py-2 font-mono text-xs">{t.provider}</td>
                <td className="px-4 py-2 font-mono text-xs">{t.model}</td>
                <td className="px-4 py-2 font-mono text-xs">{t.agent_id ?? '–'}</td>
                <td className="px-4 py-2 text-right">{t.input_tokens}</td>
                <td className="px-4 py-2 text-right">{t.output_tokens}</td>
                <td className="px-4 py-2 text-right">{t.cached_tokens}</td>
                <td className="px-4 py-2 text-right">{t.tool_calls ?? 0}</td>
                <td className="px-4 py-2 text-right">{t.duration_ms}</td>
                <td className="px-4 py-2 text-xs text-bodh-400">{t.stop_reason ?? t.error ?? '–'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Page>
  );
}

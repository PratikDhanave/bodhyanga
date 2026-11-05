import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import { useLiveResource } from '../hooks/useLiveResource';
import { api } from '../api/client';

export default function CaseRunner() {
  const navigate = useNavigate();
  const fixtures = useLiveResource(() => api.fixtures(), [], []);
  const [provider, setProvider] = useState('rule');
  const [selectedFixture, setSelectedFixture] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [chiefComplaint, setChiefComplaint] = useState('Cough and fever');
  const [hpi, setHPI] = useState('58-year-old presents with 5 days productive cough and fever to 38.9°C');
  const [vitals, setVitals] = useState('T 38.9°C, HR 98, RR 22, SpO2 94% on room air');
  const [condition, setCondition] = useState('cap');
  const [maxCostUSD, setMaxCostUSD] = useState(500);
  const [goldDiagnosis, setGoldDiagnosis] = useState('Community-acquired pneumonia');

  async function run() {
    setSubmitting(true);
    setError(null);
    try {
      const req = selectedFixture
        ? { fixture: selectedFixture, diagnostician: provider }
        : {
            diagnostician: provider,
            presentation: { chief_complaint: chiefComplaint, hpi, vitals, condition },
            max_cost_usd: maxCostUSD,
            gold_diagnosis: goldDiagnosis,
          };
      const res = await api.startRun(req);
      navigate(`/runs/${res.run_id}`);
    } catch (e) {
      setError(String(e));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Page title="Case runner" subtitle="Trigger a diagnostic flow and watch the agent trace live">
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div className="card">
          <div className="card-header">From fixture</div>
          <div className="card-body space-y-3 text-sm">
            {fixtures.loading && <Loader what="fixtures" />}
            {fixtures.error && <ErrorBox error={fixtures.error} retry={fixtures.reload} />}
            {fixtures.data && (
              <>
                <label className="block">
                  <span className="text-xs uppercase tracking-wide text-bodhyanga-400">Fixture</span>
                  <select value={selectedFixture} onChange={(e) => setSelectedFixture(e.target.value)} className="mt-1 w-full border border-bodhyanga-200 rounded px-2 py-1">
                    <option value="">(use custom presentation below)</option>
                    {fixtures.data.map((f) => (
                      <option key={f.case_id} value={f.case_id}>{f.case_id} · {f.condition} · {f.gold_diagnosis}</option>
                    ))}
                  </select>
                </label>
              </>
            )}
          </div>
        </div>
        <div className="card">
          <div className="card-header">Provider</div>
          <div className="card-body text-sm">
            <label className="block">
              <span className="text-xs uppercase tracking-wide text-bodhyanga-400">Diagnostician</span>
              <select value={provider} onChange={(e) => setProvider(e.target.value)} className="mt-1 w-full border border-bodhyanga-200 rounded px-2 py-1">
                <option value="rule">rule (deterministic)</option>
                <option value="llm-anthropic">llm-anthropic (Claude)</option>
                <option value="llm-openai">llm-openai (GPT)</option>
                <option value="llm-ollama">llm-ollama (local)</option>
              </select>
            </label>
            <p className="text-xs text-bodhyanga-400 mt-2">LLM providers require the matching API-key env var on the cmd/care server.</p>
          </div>
        </div>
      </div>

      {!selectedFixture && (
        <div className="card">
          <div className="card-header">Custom presentation</div>
          <div className="card-body grid grid-cols-1 lg:grid-cols-2 gap-3 text-sm">
            <label className="block">
              <span className="text-xs uppercase tracking-wide text-bodhyanga-400">Chief complaint</span>
              <input value={chiefComplaint} onChange={(e) => setChiefComplaint(e.target.value)} className="mt-1 w-full border border-bodhyanga-200 rounded px-2 py-1" />
            </label>
            <label className="block">
              <span className="text-xs uppercase tracking-wide text-bodhyanga-400">Condition tag</span>
              <input value={condition} onChange={(e) => setCondition(e.target.value)} className="mt-1 w-full border border-bodhyanga-200 rounded px-2 py-1" />
            </label>
            <label className="block lg:col-span-2">
              <span className="text-xs uppercase tracking-wide text-bodhyanga-400">HPI</span>
              <textarea value={hpi} onChange={(e) => setHPI(e.target.value)} className="mt-1 w-full border border-bodhyanga-200 rounded px-2 py-1" rows={3} />
            </label>
            <label className="block lg:col-span-2">
              <span className="text-xs uppercase tracking-wide text-bodhyanga-400">Vitals</span>
              <input value={vitals} onChange={(e) => setVitals(e.target.value)} className="mt-1 w-full border border-bodhyanga-200 rounded px-2 py-1" />
            </label>
            <label className="block">
              <span className="text-xs uppercase tracking-wide text-bodhyanga-400">Max diagnostic cost (USD)</span>
              <input type="number" value={maxCostUSD} onChange={(e) => setMaxCostUSD(Number(e.target.value))} className="mt-1 w-full border border-bodhyanga-200 rounded px-2 py-1" />
            </label>
            <label className="block">
              <span className="text-xs uppercase tracking-wide text-bodhyanga-400">Gold diagnosis (for scoring)</span>
              <input value={goldDiagnosis} onChange={(e) => setGoldDiagnosis(e.target.value)} className="mt-1 w-full border border-bodhyanga-200 rounded px-2 py-1" />
            </label>
          </div>
        </div>
      )}

      {error && <ErrorBox error={new Error(error)} />}

      <div className="flex justify-end">
        <button onClick={run} disabled={submitting} className="btn">
          {submitting ? 'Starting…' : 'Run case'}
        </button>
      </div>
    </Page>
  );
}

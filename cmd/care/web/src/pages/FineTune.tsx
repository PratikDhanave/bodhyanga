import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';

export default function FineTune() {
  const { data, loading, error, reload } = useApi(() => api.fineTuneStatus(), []);
  if (loading) return <Page title="Fine-tune export"><Loader what="fine-tune status" /></Page>;
  if (error) return <Page title="Fine-tune export"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data) return <Page title="Fine-tune export"><p>No data.</p></Page>;

  return (
    <Page title="Fine-tune export" subtitle="HITL decisions as training data">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Stat label="Decided reviews" value={data.decided_reviews} />
        <Stat label="Pending reviews" value={data.pending_reviews} />
        <Stat label="Projected SFT lines" value={data.sft_lines} emphasis />
        <Stat label="Projected DPO pairs" value={data.dpo_pairs} emphasis />
      </div>
      <div className="card">
        <div className="card-header">How to export</div>
        <div className="card-body space-y-2 text-sm">
          <p>The HITL review queue can be exported as fine-tuning data via the CLI:</p>
          <pre className="bg-bodh-50 rounded p-3 overflow-x-auto text-xs">{`# Save the current queue to a JSON file (DB-backed export comes next),
# then run:
go run ./cmd/export-hitl-feedback \\
  -in ./data/hitl-sample.json \\
  -out-dir /tmp/ft-out`}</pre>
          <p>Outputs <code>sft.jsonl</code> + <code>dpo.jsonl</code>. See <a className="text-bodh-600 underline" href="https://github.com/PratikDhanave/bodh/blob/main/docs/ai-features.md#fine-tuning-from-hitl-decisions">docs/ai-features.md</a>.</p>
        </div>
      </div>
    </Page>
  );
}

function Stat({ label, value, emphasis }: { label: string; value: number; emphasis?: boolean }) {
  return (
    <div className={`card ${emphasis ? 'border-amber-300 bg-amber-50' : ''}`}>
      <div className="card-body">
        <div className="text-xs uppercase tracking-wide text-bodh-400">{label}</div>
        <div className="text-3xl font-semibold mt-2">{value}</div>
      </div>
    </div>
  );
}

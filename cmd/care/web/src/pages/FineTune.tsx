import { useState } from 'react';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import { Tooltip } from '../components/Tooltip';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';

const CLI_COMMAND = `go run ./cmd/export-hitl-feedback \\
  -in ./data/hitl-sample.json \\
  -out-dir /tmp/ft-out`;

export default function FineTune() {
  const { data, loading, error, reload } = useApi(() => api.fineTuneStatus(), []);
  const [copied, setCopied] = useState(false);

  if (loading) return <Page title="Fine-tune export"><Loader what="fine-tune status" /></Page>;
  if (error) return <Page title="Fine-tune export"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data) return <Page title="Fine-tune export"><p>No data.</p></Page>;

  async function copyCommand() {
    try {
      await navigator.clipboard.writeText(CLI_COMMAND);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard may not be available in tests/insecure contexts
    }
  }

  return (
    <Page title="Fine-tune export" subtitle="HITL decisions as training data">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Stat label="Decided reviews" value={data.decided_reviews} />
        <Stat label="Pending reviews" value={data.pending_reviews} />
        <Stat
          label="Projected SFT lines"
          value={data.sft_lines}
          emphasis
          tooltip="Supervised Fine-Tuning: each decided review becomes one training example"
        />
        <Stat
          label="Projected DPO pairs"
          value={data.dpo_pairs}
          emphasis
          tooltip="Direct Preference Optimization: pairs of chosen (approved) vs rejected responses"
        />
      </div>
      <div className="card">
        <div className="card-header flex items-center justify-between">
          <span>How to export</span>
          <button
            onClick={() => { void copyCommand(); }}
            className="btn-secondary text-xs"
            title="Copy command to clipboard"
          >
            {copied ? 'Copied!' : 'Copy command'}
          </button>
        </div>
        <div className="card-body space-y-2 text-sm">
          <p className="dark:text-gray-300">The HITL review queue can be exported as fine-tuning data via the CLI:</p>
          <pre className="bg-bodh-50 dark:bg-gray-800 dark:text-gray-300 rounded p-3 overflow-x-auto text-xs">{`# Save the current queue to a JSON file (DB-backed export comes next),
# then run:
${CLI_COMMAND}`}</pre>
          <p className="dark:text-gray-300">
            Outputs <code className="font-mono text-xs bg-bodh-100 dark:bg-gray-700 px-1 rounded">sft.jsonl</code> +{' '}
            <code className="font-mono text-xs bg-bodh-100 dark:bg-gray-700 px-1 rounded">dpo.jsonl</code>. See{' '}
            <a className="text-bodh-600 dark:text-bodh-300 underline" href="https://github.com/PratikDhanave/bodh/blob/main/docs/ai-features.md#fine-tuning-from-hitl-decisions">
              docs/ai-features.md
            </a>.
          </p>
        </div>
      </div>
    </Page>
  );
}

function Stat({ label, value, emphasis, tooltip }: { label: string; value: number; emphasis?: boolean; tooltip?: string }) {
  const content = (
    <div className={`card ${emphasis ? 'border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-900/20' : ''}`}>
      <div className="card-body">
        <div className="text-xs uppercase tracking-wide text-bodh-400 dark:text-gray-400">{label}</div>
        <div className="text-3xl font-semibold mt-2 text-bodh-800 dark:text-gray-100">{value}</div>
      </div>
    </div>
  );
  if (tooltip) {
    return <Tooltip content={tooltip}>{content}</Tooltip>;
  }
  return content;
}

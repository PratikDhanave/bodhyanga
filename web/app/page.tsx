"use client";

import Link from "next/link";
import { useState } from "react";
import { careApi, CareApiError, type SubmitHandle } from "@/lib/api";
import { PageHeader } from "@/components/PageHeader";
import { StatusPill } from "@/components/StatusPill";

// The pipeline IS a sequence, so numbered steps encode real order (not decoration).
const PIPELINE = [
  { n: "01", label: "Admit", note: "Governance gate — fail-closed" },
  { n: "02", label: "Diagnose", note: "Rule or LLM engine, cost-aware" },
  { n: "03", label: "Review", note: "Physician approves at the confidence gate" },
  { n: "04", label: "Care plan", note: "Terminal output + audit trail" },
];

function toList(s: string): string[] {
  return s.split(",").map((x) => x.trim()).filter(Boolean);
}

export default function Overview() {
  const [caseId, setCaseId] = useState("");
  const [symptoms, setSymptoms] = useState("");
  const [history, setHistory] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<SubmitHandle | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setResult(null);
    setBusy(true);
    try {
      const res = await careApi.submitCase({
        case_id: caseId.trim() || `web-${Date.now()}`,
        symptoms: toList(symptoms),
        history: toList(history),
      });
      setResult(res);
      setSymptoms("");
      setHistory("");
      setCaseId("");
    } catch (err) {
      setError(err instanceof CareApiError ? err.message : "Something went wrong.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Decision support"
        title="Submit a case for AI diagnosis"
        subtitle="The case runs the admission-controlled pipeline and pauses for your review before any care plan is issued. Symptoms and history stay on the box — only a hashed case token appears here."
      />

      <div className="grid gap-6 lg:grid-cols-[1.1fr_0.9fr]">
        <section className="card">
          <div className="card-head">New case</div>
          <form onSubmit={submit} className="card-pad space-y-4">
            <div>
              <label className="mb-1 block text-xs font-medium text-ink-600">
                Case ID{" "}
                <span className="font-normal text-ink-400">
                  (optional — a token is generated if blank)
                </span>
              </label>
              <input
                className="field mono"
                placeholder="e.g. MRN-4821 (never shown or logged raw)"
                value={caseId}
                onChange={(e) => setCaseId(e.target.value)}
              />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-ink-600">Symptoms</label>
              <input
                className="field"
                placeholder="chest pain, diaphoresis"
                value={symptoms}
                onChange={(e) => setSymptoms(e.target.value)}
              />
              <p className="mt-1 text-xs text-ink-400">Comma-separated.</p>
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-ink-600">History</label>
              <input
                className="field"
                placeholder="hypertension, smoker"
                value={history}
                onChange={(e) => setHistory(e.target.value)}
              />
              <p className="mt-1 text-xs text-ink-400">Comma-separated.</p>
            </div>

            {error && (
              <div className="rounded-md border border-[#f6c8c2] bg-[#fef1f0] px-3 py-2 text-sm text-[#b42318]">
                {error}
              </div>
            )}

            <button className="btn" disabled={busy}>
              {busy ? "Submitting…" : "Submit case"}
            </button>
          </form>
        </section>

        <section className="space-y-6">
          {result ? (
            <div className="card">
              <div className="card-head">Submitted</div>
              <div className="card-pad space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-ink-500">Status</span>
                  <StatusPill status={result.status} />
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm text-ink-500">Case token</span>
                  <span className="mono text-ink-800">{result.case_token}</span>
                </div>
                <div className="flex flex-wrap gap-2 pt-1">
                  <Link href={`/cases/${result.case_token}`} className="btn-ghost">
                    View case
                  </Link>
                  {result.status === "awaiting_review" && (
                    <Link href="/reviews" className="btn">
                      Go to review queue
                    </Link>
                  )}
                </div>
              </div>
            </div>
          ) : (
            <div className="card">
              <div className="card-head">How a case flows</div>
              <ol className="card-pad space-y-3">
                {PIPELINE.map((s) => (
                  <li key={s.n} className="flex gap-3">
                    <span className="mono mt-0.5 text-xs font-semibold text-clinical-600">
                      {s.n}
                    </span>
                    <div>
                      <div className="text-sm font-medium text-ink-800">{s.label}</div>
                      <div className="text-xs text-ink-400">{s.note}</div>
                    </div>
                  </li>
                ))}
              </ol>
            </div>
          )}
        </section>
      </div>
    </>
  );
}

"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import {
  careApi,
  CareApiError,
  type AuditEvent,
  type CaseEntry,
} from "@/lib/api";
import { PageHeader } from "@/components/PageHeader";
import { StatusPill } from "@/components/StatusPill";
import { ConfidenceGauge } from "@/components/ConfidenceGauge";

const TERMINAL = new Set(["completed", "rejected", "denied", "failed"]);

function decisionTone(decision?: string) {
  if (decision === "deny") return "text-[#b42318]";
  if (decision === "allow") return "text-clinical-700";
  return "text-ink-500";
}

export default function CaseDetail() {
  const { token } = useParams<{ token: string }>();
  const [entry, setEntry] = useState<CaseEntry | null>(null);
  const [audit, setAudit] = useState<AuditEvent[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [e, a] = await Promise.all([
        careApi.getCase(token),
        careApi.getAudit(token).catch(() => ({ case_token: token, events: [] })),
      ]);
      setEntry(e);
      setAudit(a.events);
      setError(null);
    } catch (err) {
      setError(err instanceof CareApiError ? err.message : "Could not load the case.");
    }
  }, [token]);

  useEffect(() => {
    load();
  }, [load]);

  // Poll until the case reaches a terminal state.
  useEffect(() => {
    if (entry && TERMINAL.has(entry.status)) return;
    const id = setInterval(load, 5000);
    return () => clearInterval(id);
  }, [entry, load]);

  return (
    <>
      <PageHeader
        eyebrow="Case"
        title={token}
        subtitle="Everything shown here is PHI-free — a hashed token, the decision-support output, and the audit trail."
        action={
          <div className="flex items-center gap-3">
            {entry && <StatusPill status={entry.status} />}
            <Link href="/reviews" className="btn-ghost">
              Review queue
            </Link>
          </div>
        }
      />

      {error && (
        <div className="mb-4 rounded-md border border-[#f6c8c2] bg-[#fef1f0] px-3 py-2 text-sm text-[#b42318]">
          {error}
        </div>
      )}

      {!entry && !error ? (
        <div className="card card-pad text-sm text-ink-400">Loading case…</div>
      ) : entry ? (
        <div className="grid gap-6 lg:grid-cols-[1fr_1fr]">
          {/* Diagnosis */}
          <section className="card">
            <div className="card-head">Diagnosis</div>
            <div className="card-pad space-y-5">
              <div>
                <div className="text-lg font-semibold text-ink-900">
                  {entry.diagnosis || "Undifferentiated"}
                </div>
                {entry.forced && (
                  <span className="pill pill-review mt-2">
                    Forced progression
                  </span>
                )}
              </div>

              {typeof entry.confidence === "number" && (
                <div>
                  <div className="eyebrow mb-2">AI confidence</div>
                  <ConfidenceGauge value={entry.confidence} />
                </div>
              )}

              <div className="flex flex-wrap gap-x-8 gap-y-3 border-t border-ink-100 pt-4">
                <Metric label="Work-up cost" value={
                  typeof entry.cost_usd === "number" ? `$${Math.round(entry.cost_usd)}` : "—"
                } />
                <Metric label="Reviewer" value={entry.reviewer_id || "—"} />
              </div>

              {entry.plan_steps && entry.plan_steps.length > 0 && (
                <div className="border-t border-ink-100 pt-4">
                  <div className="eyebrow mb-2">Care plan</div>
                  <ul className="space-y-1.5">
                    {entry.plan_steps.map((s, i) => (
                      <li key={i} className="flex gap-2 text-sm text-ink-700">
                        <span className="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-clinical-500" />
                        {s}
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {entry.status === "rejected" && (
                <div className="rounded-md border border-[#f6c8c2] bg-[#fef1f0] px-3 py-2 text-sm text-[#b42318]">
                  Diagnosis rejected by {entry.reviewer_id || "reviewer"} — no care plan issued.
                </div>
              )}
              {entry.status === "denied" && (
                <div className="rounded-md border border-[#f0c9a8] bg-[#fbf0e8] px-3 py-2 text-sm text-[#6b3410]">
                  Halted by governance before any agent ran.
                </div>
              )}
            </div>
          </section>

          {/* Audit trail */}
          <section className="card">
            <div className="card-head">Audit trail</div>
            {audit && audit.length > 0 ? (
              <table className="dtable">
                <thead>
                  <tr>
                    <th>Event</th>
                    <th>Agent</th>
                    <th>Decision</th>
                  </tr>
                </thead>
                <tbody>
                  {audit.map((e, i) => (
                    <tr key={i}>
                      <td className="text-ink-800">{e.kind}</td>
                      <td className="text-ink-500">{e.agent_id || "—"}</td>
                      <td className={decisionTone(e.decision)}>
                        {e.decision || e.reason || "—"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <div className="card-pad text-sm text-ink-400">
                No audit events recorded for this token yet.
              </div>
            )}
          </section>
        </div>
      ) : null}
    </>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="eyebrow">{label}</div>
      <div className="metric mt-0.5 text-sm font-semibold text-ink-800">{value}</div>
    </div>
  );
}

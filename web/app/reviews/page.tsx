"use client";

import { useCallback, useEffect, useState } from "react";
import { careApi, CareApiError, type ReviewSummary } from "@/lib/api";
import { PageHeader } from "@/components/PageHeader";
import { ConfidenceGauge } from "@/components/ConfidenceGauge";

function useReviewerId() {
  const [id, setId] = useState("");
  useEffect(() => {
    try {
      setId(localStorage.getItem("bodh-reviewer") ?? "");
    } catch {}
  }, []);
  const update = (v: string) => {
    setId(v);
    try {
      localStorage.setItem("bodh-reviewer", v);
    } catch {}
  };
  return [id, update] as const;
}

export default function Reviews() {
  const [reviews, setReviews] = useState<ReviewSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reviewerId, setReviewerId] = useReviewerId();
  const [pending, setPending] = useState<string | null>(null); // session id being answered

  const load = useCallback(async () => {
    setError(null);
    try {
      const { reviews } = await careApi.listReviews();
      setReviews(reviews);
    } catch (err) {
      setError(err instanceof CareApiError ? err.message : "Could not load the review queue.");
      setReviews([]);
    }
  }, []);

  useEffect(() => {
    load();
    const id = setInterval(load, 8000);
    return () => clearInterval(id);
  }, [load]);

  async function answer(sessionId: string, approved: boolean) {
    if (!reviewerId.trim()) return;
    setPending(sessionId);
    try {
      await careApi.answerReview(sessionId, approved, reviewerId.trim());
      await load();
    } catch (err) {
      setError(err instanceof CareApiError ? err.message : "Could not submit the decision.");
    } finally {
      setPending(null);
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Human oversight"
        title="Review queue"
        subtitle="Every AI diagnosis pauses here before a care plan is issued. Approve to proceed, or reject to end the case without one. Decisions are recorded against your reviewer ID."
        action={
          <div className="w-56">
            <label className="mb-1 block text-right text-xs font-medium text-ink-500">
              Reviewer ID
            </label>
            <input
              className="field"
              placeholder="e.g. dr-okafor"
              value={reviewerId}
              onChange={(e) => setReviewerId(e.target.value)}
            />
          </div>
        }
      />

      {error && (
        <div className="mb-4 rounded-md border border-[#f6c8c2] bg-[#fef1f0] px-3 py-2 text-sm text-[#b42318]">
          {error}
        </div>
      )}

      {reviews === null ? (
        <div className="card card-pad text-sm text-ink-400">Loading queue…</div>
      ) : reviews.length === 0 ? (
        <div className="card card-pad">
          <div className="text-sm font-medium text-ink-700">Queue is clear</div>
          <p className="mt-1 text-sm text-ink-400">
            No cases are awaiting review. Submit one from Overview to see it here.
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {reviews.map((r) => (
            <article key={r.session_id} className="card card-pad">
              <div className="flex flex-col gap-4 lg:flex-row lg:items-center">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="mono text-xs text-ink-400">{r.case_token}</span>
                    {r.forced && (
                      <span className="pill pill-review" title="Confidence bar not met — progressed by the round limit">
                        Forced
                      </span>
                    )}
                  </div>
                  <div className="mt-1 truncate text-base font-semibold text-ink-900">
                    {r.diagnosis || "Undifferentiated"}
                  </div>
                  <div className="mt-3 max-w-md">
                    <ConfidenceGauge value={r.confidence} />
                  </div>
                </div>

                <div className="flex items-center gap-6 lg:flex-col lg:items-end lg:gap-3">
                  <div className="text-right">
                    <div className="eyebrow">Work-up cost</div>
                    <div className="metric text-lg font-semibold text-ink-800">
                      ${Math.round(r.cost_usd)}
                    </div>
                  </div>
                  <div className="flex gap-2">
                    <button
                      className="btn-ghost btn-danger-ghost"
                      style={{ color: "#b42318", borderColor: "#f6c8c2" }}
                      disabled={!reviewerId.trim() || pending === r.session_id}
                      onClick={() => answer(r.session_id, false)}
                    >
                      Reject
                    </button>
                    <button
                      className="btn"
                      disabled={!reviewerId.trim() || pending === r.session_id}
                      onClick={() => answer(r.session_id, true)}
                    >
                      {pending === r.session_id ? "Saving…" : "Approve"}
                    </button>
                  </div>
                </div>
              </div>
              {!reviewerId.trim() && (
                <p className="mt-3 text-xs text-ink-400">
                  Enter your reviewer ID above to record a decision.
                </p>
              )}
            </article>
          ))}
        </div>
      )}
    </>
  );
}

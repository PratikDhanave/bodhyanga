import { useState, useCallback, useEffect, useRef } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import { Modal } from '../components/Modal';
import { useApi } from '../hooks/useApi';
import { useToast } from '../components/Toast';
import { api } from '../api/client';
import { getCurrentUser } from '../hooks/useAuth';
import type { ReviewDecision } from '../types';

// ---------------------------------------------------------------------------
// Decision confirmation labels (clinical safety — prevents accidental clicks)
// ---------------------------------------------------------------------------
const DECISION_LABELS: Record<ReviewDecision, string> = {
  approved: 'Approve',
  approved_with_modifications: 'Approve with modifications',
  rejected: 'Reject',
};
const DECISION_COLORS: Record<ReviewDecision, string> = {
  approved: 'bg-emerald-600 hover:bg-emerald-700',
  approved_with_modifications: 'bg-amber-600 hover:bg-amber-700',
  rejected: 'bg-rose-600 hover:bg-rose-700',
};

export default function ReviewDetail() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const { showToast } = useToast();
  const { data, loading, error, reload } = useApi(() => api.review(id), [id]);

  // Pre-fill reviewer from JWT when available
  const user = getCurrentUser();
  const [reviewerId, setReviewerId] = useState(user?.email ?? 'clinician-demo');
  const [reason, setReason] = useState('');
  const [modification, setModification] = useState('');
  const [submitting, setSubmitting] = useState(false);

  // Confirmation modal state
  const [confirmDecision, setConfirmDecision] = useState<ReviewDecision | null>(null);
  const [confirmDelay, setConfirmDelay] = useState(2); // 2-second safety delay
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Count down the confirm delay when modal is open
  useEffect(() => {
    if (confirmDecision === null) return;
    setConfirmDelay(2);
    timerRef.current = setInterval(() => {
      setConfirmDelay((prev) => {
        if (prev <= 1) {
          if (timerRef.current) clearInterval(timerRef.current);
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [confirmDecision]);

  const handleDecisionClick = useCallback((decision: ReviewDecision) => {
    setConfirmDecision(decision);
  }, []);

  const handleConfirm = useCallback(async () => {
    if (!confirmDecision) return;
    const decision = confirmDecision;
    setConfirmDecision(null);
    setSubmitting(true);
    try {
      await api.decide(id, {
        reviewer_id: reviewerId,
        decision,
        reason: reason || undefined,
        modification: decision === 'approved_with_modifications' ? modification : undefined,
      });
      showToast(`Review ${decision.replace(/_/g, ' ')} successfully`, 'success');
      navigate('/reviews');
    } catch (e) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setSubmitting(false);
    }
  }, [confirmDecision, id, reviewerId, reason, modification, navigate, showToast]);

  if (loading) return <Page title="Review"><Loader what="review" /></Page>;
  if (error) return <Page title="Review"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data) return <Page title="Review"><p>Not found.</p></Page>;

  const pending = data.status === 'pending';

  return (
    <Page
      title={data.subject}
      subtitle={`Review ${data.id} · ${data.kind}`}
      actions={<Link to="/reviews" className="btn-secondary">Back</Link>}
    >
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="lg:col-span-2 space-y-4">
          <div className="card">
            <div className="card-header">Detail</div>
            <div className="card-body space-y-3 text-sm">
              {data.detail && <p>{data.detail}</p>}
              {data.next_to && (
                <p className="text-bodh-400 text-xs dark:text-gray-400">
                  Resumes to <span className="font-mono">{data.next_to}</span> as <span className="font-mono">{data.next_type}</span>
                </p>
              )}
              {data.payload && (
                <div>
                  <div className="text-xs uppercase tracking-wide text-bodh-400 dark:text-gray-400 mb-1">Payload</div>
                  <pre className="bg-bodh-50 dark:bg-gray-700 rounded p-3 overflow-x-auto text-xs">{tryPrettyJSON(data.payload)}</pre>
                </div>
              )}
            </div>
          </div>
        </div>
        <div>
          <div className="card">
            <div className="card-header">Decision</div>
            <div className="card-body space-y-3 text-sm">
              {!pending && (
                <p className="text-bodh-400 dark:text-gray-400">
                  Decided <span className={`badge badge-${data.status}`}>{data.status}</span>
                  {data.reviewer_id && <> by <span className="font-mono">{data.reviewer_id}</span></>}
                  {data.decided_at && <> at {new Date(data.decided_at).toLocaleString()}</>}
                </p>
              )}
              {pending && (
                <>
                  <label className="block">
                    <span className="text-xs uppercase tracking-wide text-bodh-400 dark:text-gray-400">Reviewer ID</span>
                    <input value={reviewerId} onChange={(e) => setReviewerId(e.target.value)} className="mt-1 w-full border border-bodh-200 dark:border-gray-600 dark:bg-gray-700 rounded px-2 py-1" />
                  </label>
                  <label className="block">
                    <span className="text-xs uppercase tracking-wide text-bodh-400 dark:text-gray-400">Reason</span>
                    <textarea value={reason} onChange={(e) => setReason(e.target.value)} className="mt-1 w-full border border-bodh-200 dark:border-gray-600 dark:bg-gray-700 rounded px-2 py-1" rows={2} />
                  </label>
                  <label className="block">
                    <span className="text-xs uppercase tracking-wide text-bodh-400 dark:text-gray-400">Modified payload (if approving with mods)</span>
                    <textarea value={modification} onChange={(e) => setModification(e.target.value)} className="mt-1 w-full border border-bodh-200 dark:border-gray-600 dark:bg-gray-700 rounded px-2 py-1 font-mono text-xs" rows={4} />
                  </label>
                  <div className="flex flex-col gap-2">
                    <button disabled={submitting} onClick={() => handleDecisionClick('approved')} className="btn">Approve</button>
                    <button disabled={submitting || !modification} onClick={() => handleDecisionClick('approved_with_modifications')} className="btn-secondary">Approve with modifications</button>
                    <button disabled={submitting} onClick={() => handleDecisionClick('rejected')} className="btn-secondary text-rose-600 dark:text-rose-400">Reject</button>
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Confirmation modal — clinical safety: prevents accidental decisions */}
      <Modal
        open={confirmDecision !== null}
        onClose={() => setConfirmDecision(null)}
        title="Confirm Clinical Decision"
        danger={confirmDecision === 'rejected'}
      >
        {confirmDecision && (
          <div className="space-y-4">
            <p className="text-sm">
              You are about to <strong>{DECISION_LABELS[confirmDecision].toLowerCase()}</strong> this review:
            </p>
            <div className="bg-bodh-50 dark:bg-gray-700 rounded p-3 text-sm">
              <div className="font-medium">{data?.subject}</div>
              <div className="text-xs text-bodh-400 dark:text-gray-400 mt-1">{data?.kind} · Review {data?.id}</div>
            </div>
            {reason && (
              <div className="text-xs">
                <span className="text-bodh-400 dark:text-gray-400">Reason:</span> {reason}
              </div>
            )}
            <div className="flex gap-3 justify-end">
              <button onClick={() => setConfirmDecision(null)} className="btn-secondary">
                Cancel
              </button>
              <button
                disabled={confirmDelay > 0}
                onClick={handleConfirm}
                className={`inline-flex items-center px-3 py-1.5 rounded-md text-sm font-medium text-white
                            disabled:opacity-50 disabled:cursor-not-allowed ${DECISION_COLORS[confirmDecision]}`}
              >
                {confirmDelay > 0
                  ? `Confirm (${confirmDelay}s)`
                  : `Confirm ${DECISION_LABELS[confirmDecision]}`
                }
              </button>
            </div>
          </div>
        )}
      </Modal>
    </Page>
  );
}

function tryPrettyJSON(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

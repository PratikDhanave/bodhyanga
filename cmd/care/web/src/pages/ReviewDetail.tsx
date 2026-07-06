import { useState, useCallback } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import ConfirmationModal from '../components/ConfirmationModal';
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

export default function ReviewDetail() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const { showToast } = useToast();
  const { data, loading, error, reload } = useApi(() => api.review(id), [id]);

  // Pre-fill reviewer from JWT when available
  const user = getCurrentUser();
  const [reviewerId, setReviewerId] = useState(user?.email ?? 'clinician-demo');
  const [modification, setModification] = useState('');
  const [submitting, setSubmitting] = useState(false);

  // Confirmation modal state — uses ConfirmationModal with built-in reason + delay
  const [confirmDecision, setConfirmDecision] = useState<ReviewDecision | null>(null);

  const handleDecisionClick = useCallback((decision: ReviewDecision) => {
    setConfirmDecision(decision);
  }, []);

  const handleConfirm = useCallback(async (reason: string) => {
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
  }, [confirmDecision, id, reviewerId, modification, navigate, showToast]);

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
      {confirmDecision && (
        <ConfirmationModal
          isOpen
          title={`Confirm ${DECISION_LABELS[confirmDecision]}`}
          diagnosisSummary={data?.subject ?? ''}
          patientContext={`${data?.kind ?? ''} · Review ${data?.id ?? ''}`}
          confidence={data?.meta?.confidence as number | undefined}
          onConfirm={handleConfirm}
          onCancel={() => setConfirmDecision(null)}
          danger={confirmDecision === 'rejected'}
        />
      )}
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

import { useState } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import { useLiveResource } from '../hooks/useLiveResource';
import { api } from '../api/client';
import type { ReviewDecision } from '../types';

export default function ReviewDetail() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const { data, loading, error, reload } = useLiveResource(() => api.review(id), [id], ['review_submitted','review_decided']);

  const [reviewerId, setReviewerId] = useState('clinician-demo');
  const [reason, setReason] = useState('');
  const [modification, setModification] = useState('');
  const [submitting, setSubmitting] = useState(false);

  async function decide(decision: ReviewDecision) {
    setSubmitting(true);
    try {
      await api.decide(id, {
        reviewer_id: reviewerId,
        decision,
        reason: reason || undefined,
        modification: decision === 'approved_with_modifications' ? modification : undefined,
      });
      navigate('/reviews');
    } catch (e) {
      alert(String(e));
    } finally {
      setSubmitting(false);
    }
  }

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
                <p className="text-bodhyanga-400 text-xs">
                  Resumes to <span className="font-mono">{data.next_to}</span> as <span className="font-mono">{data.next_type}</span>
                </p>
              )}
              {data.payload && (
                <div>
                  <div className="text-xs uppercase tracking-wide text-bodhyanga-400 mb-1">Payload</div>
                  <pre className="bg-bodhyanga-50 rounded p-3 overflow-x-auto text-xs">{tryPrettyJSON(data.payload)}</pre>
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
                <p className="text-bodhyanga-400">
                  Decided <span className={`badge badge-${data.status}`}>{data.status}</span>
                  {data.reviewer_id && <> by <span className="font-mono">{data.reviewer_id}</span></>}
                  {data.decided_at && <> at {new Date(data.decided_at).toLocaleString()}</>}
                </p>
              )}
              {pending && (
                <>
                  <label className="block">
                    <span className="text-xs uppercase tracking-wide text-bodhyanga-400">Reviewer ID</span>
                    <input value={reviewerId} onChange={(e) => setReviewerId(e.target.value)} className="mt-1 w-full border border-bodhyanga-200 rounded px-2 py-1" />
                  </label>
                  <label className="block">
                    <span className="text-xs uppercase tracking-wide text-bodhyanga-400">Reason (optional)</span>
                    <textarea value={reason} onChange={(e) => setReason(e.target.value)} className="mt-1 w-full border border-bodhyanga-200 rounded px-2 py-1" rows={2} />
                  </label>
                  <label className="block">
                    <span className="text-xs uppercase tracking-wide text-bodhyanga-400">Modified payload (if approving with mods)</span>
                    <textarea value={modification} onChange={(e) => setModification(e.target.value)} className="mt-1 w-full border border-bodhyanga-200 rounded px-2 py-1 font-mono text-xs" rows={4} />
                  </label>
                  <div className="flex flex-col gap-2">
                    <button disabled={submitting} onClick={() => decide('approved')} className="btn">Approve</button>
                    <button disabled={submitting || !modification} onClick={() => decide('approved_with_modifications')} className="btn-secondary">Approve with modifications</button>
                    <button disabled={submitting} onClick={() => decide('rejected')} className="btn-secondary">Reject</button>
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      </div>
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

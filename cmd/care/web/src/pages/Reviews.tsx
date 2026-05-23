import { useState } from 'react';
import { Link } from 'react-router-dom';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';

export default function Reviews() {
  const [scope, setScope] = useState<'pending' | 'all'>('pending');
  const { data, loading, error, reload } = useApi(() => api.reviews(scope), [scope]);

  if (loading) return <Page title="HITL reviews"><Loader what="reviews" /></Page>;
  if (error) return <Page title="HITL reviews"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return (
      <Page title="HITL reviews" actions={<ScopeToggle scope={scope} setScope={setScope} />}>
        <Empty title={scope === 'pending' ? 'No pending reviews' : 'No reviews yet'} hint="Reviews appear when agents publish review_request messages." />
      </Page>
    );
  }

  return (
    <Page
      title="HITL reviews"
      subtitle={`${data.length} ${scope === 'pending' ? 'pending' : 'total'}`}
      actions={<ScopeToggle scope={scope} setScope={setScope} />}
    >
      <div className="card overflow-hidden">
        <table className="min-w-full text-sm">
          <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
            <tr>
              <th className="px-4 py-2 text-left">Subject</th>
              <th className="px-4 py-2 text-left">Kind</th>
              <th className="px-4 py-2 text-left">Priority</th>
              <th className="px-4 py-2 text-left">Decision</th>
              <th className="px-4 py-2 text-left">Created</th>
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {data.map((r) => (
              <tr key={r.id} className="border-t border-bodh-100 hover:bg-bodh-50">
                <td className="px-4 py-2 font-medium">{r.subject}</td>
                <td className="px-4 py-2 text-bodh-400 text-xs">{r.kind}</td>
                <td className="px-4 py-2"><span className={`badge badge-${r.priority === 'urgent' || r.priority === 'high' ? 'urgent' : 'normal'}`}>{r.priority || 'normal'}</span></td>
                <td className="px-4 py-2"><span className={`badge badge-${r.status}`}>{r.status}</span></td>
                <td className="px-4 py-2 text-xs text-bodh-400">{new Date(r.submitted_at).toLocaleString()}</td>
                <td className="px-4 py-2 text-right">
                  <Link to={`/reviews/${r.id}`} className="btn-secondary">Open</Link>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Page>
  );
}

function ScopeToggle({ scope, setScope }: { scope: 'pending' | 'all'; setScope: (s: 'pending' | 'all') => void }) {
  return (
    <>
      <button onClick={() => setScope('pending')} className={scope === 'pending' ? 'btn' : 'btn-secondary'}>Pending</button>
      <button onClick={() => setScope('all')} className={scope === 'all' ? 'btn' : 'btn-secondary'}>All</button>
    </>
  );
}

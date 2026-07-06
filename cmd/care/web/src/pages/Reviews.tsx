import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import CardTable from '../components/CardTable';
import type { CardColumn } from '../components/CardTable';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';
import { useI18n } from '../i18n';
import { useTableKeyboardNav } from '../hooks/useTableKeyboardNav';

const COLUMNS: CardColumn[] = [
  { key: 'subject', label: 'Subject', showInCard: true, primary: true },
  { key: 'kind', label: 'Kind', showInCard: true },
  { key: 'priority', label: 'Priority', showInCard: true },
  { key: 'status', label: 'Status', showInCard: true },
  { key: 'submitted_at', label: 'Created', showInCard: true },
];

export default function Reviews() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [scope, setScope] = useState<'pending' | 'all'>('pending');
  const { data, loading, error, reload } = useApi(() => api.reviews(scope), [scope]);
  const { getRowProps } = useTableKeyboardNav(data?.length ?? 0, (i) => {
    if (data?.[i]) navigate(`/reviews/${data[i].id}`);
  });

  if (loading) return <Page title={t('reviews.title')}><Loader what="reviews" /></Page>;
  if (error) return <Page title={t('reviews.title')}><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return (
      <Page title={t('reviews.title')} actions={<ScopeToggle scope={scope} setScope={setScope} />}>
        <Empty title={t('reviews.noReviews')} hint="Reviews appear when agents publish review_request messages." />
      </Page>
    );
  }

  return (
    <Page
      title={t('reviews.title')}
      subtitle={`${data.length} ${scope === 'pending' ? 'pending' : 'total'}`}
      actions={<ScopeToggle scope={scope} setScope={setScope} />}
    >
      <CardTable columns={COLUMNS} data={data as unknown as Record<string, unknown>[]} keyField="id" onRowClick={(row) => navigate(`/reviews/${row.id}`)}>
        <div className="card overflow-hidden">
          <table className="min-w-full text-sm" aria-label="HITL review requests">
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
              {data.map((r, i) => {
                const rp = getRowProps(i);
                return (
                <tr key={r.id} {...rp} className={`border-t border-bodh-100 hover:bg-bodh-50 ${rp.className}`}>
                  <td className="px-4 py-2 font-medium">{r.subject}</td>
                  <td className="px-4 py-2 text-bodh-400 text-xs">{r.kind}</td>
                  <td className="px-4 py-2"><span className={`badge badge-${r.priority === 'urgent' || r.priority === 'high' ? 'urgent' : 'normal'}`}>{r.priority || 'normal'}</span></td>
                  <td className="px-4 py-2"><span className={`badge badge-${r.status}`}>{r.status}</span></td>
                  <td className="px-4 py-2 text-xs text-bodh-400">{new Date(r.submitted_at).toLocaleString()}</td>
                  <td className="px-4 py-2 text-right">
                    <Link to={`/reviews/${r.id}`} className="btn-secondary">Open</Link>
                  </td>
                </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </CardTable>
    </Page>
  );
}

function ScopeToggle({ scope, setScope }: { scope: 'pending' | 'all'; setScope: (s: 'pending' | 'all') => void }) {
  const { t } = useI18n();
  return (
    <div role="radiogroup" aria-label="Filter reviews" className="flex gap-2">
      <button
        role="radio"
        aria-checked={scope === 'pending'}
        onClick={() => setScope('pending')}
        className={scope === 'pending' ? 'btn' : 'btn-secondary'}
      >
        {t('reviews.pending')}
      </button>
      <button
        role="radio"
        aria-checked={scope === 'all'}
        onClick={() => setScope('all')}
        className={scope === 'all' ? 'btn' : 'btn-secondary'}
      >
        {t('reviews.all')}
      </button>
    </div>
  );
}

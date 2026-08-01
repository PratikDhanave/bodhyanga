import { useState } from 'react';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { useLiveResource } from '../hooks/useLiveResource';
import { api } from '../api/client';

export default function Tasks() {
  const { data, loading, error, reload } = useLiveResource(() => api.tasks(), [], ['tcm_tasks_created','inbox_routed','refill_response']);
  const [filter, setFilter] = useState<'all' | 'open' | 'urgent'>('all');

  if (loading) return <Page title="Nurse tasks"><Loader what="tasks" /></Page>;
  if (error) return <Page title="Nurse tasks"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return <Page title="Nurse tasks"><Empty title="No tasks" /></Page>;
  }

  const filtered = data.filter((t) => {
    if (filter === 'open') return t.status === 'open';
    if (filter === 'urgent') return t.priority === 'urgent';
    return true;
  });

  return (
    <Page
      title="Nurse tasks"
      subtitle={`${filtered.length} of ${data.length}`}
      actions={
        <>
          <button onClick={() => setFilter('all')} className={filter === 'all' ? 'btn' : 'btn-secondary'}>All</button>
          <button onClick={() => setFilter('open')} className={filter === 'open' ? 'btn' : 'btn-secondary'}>Open</button>
          <button onClick={() => setFilter('urgent')} className={filter === 'urgent' ? 'btn' : 'btn-secondary'}>Urgent</button>
          <button onClick={reload} className="btn-secondary">Refresh</button>
        </>
      }
    >
      <div className="card overflow-hidden">
        <table className="min-w-full text-sm">
          <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
            <tr>
              <th className="px-4 py-2 text-left">Subject</th>
              <th className="px-4 py-2 text-left">Patient</th>
              <th className="px-4 py-2 text-left">Kind</th>
              <th className="px-4 py-2 text-left">Priority</th>
              <th className="px-4 py-2 text-left">Status</th>
              <th className="px-4 py-2 text-left">Due</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((t) => (
              <tr key={t.id} className="border-t border-bodh-100">
                <td className="px-4 py-2 font-medium">{t.subject}</td>
                <td className="px-4 py-2 font-mono text-xs">{t.patient_id}</td>
                <td className="px-4 py-2 text-bodh-400 text-xs">{t.kind}</td>
                <td className="px-4 py-2"><span className={`badge badge-${t.priority === 'urgent' ? 'urgent' : 'normal'}`}>{t.priority}</span></td>
                <td className="px-4 py-2"><span className={`badge ${t.status === 'done' ? 'badge-done' : 'badge-normal'}`}>{t.status}</span></td>
                <td className="px-4 py-2 text-xs text-bodh-400">{new Date(t.due_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Page>
  );
}

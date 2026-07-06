import { useMemo, useState } from 'react';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { Tabs } from '../components/Tabs';
import CardTable from '../components/CardTable';
import type { CardColumn } from '../components/CardTable';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';
import { useTableKeyboardNav } from '../hooks/useTableKeyboardNav';
import type { NurseTask } from '../types';

const COLUMNS: CardColumn[] = [
  { key: 'subject', label: 'Subject', showInCard: true, primary: true },
  { key: 'patient_id', label: 'Patient', showInCard: true },
  { key: 'kind', label: 'Kind', showInCard: false },
  { key: 'priority', label: 'Priority', showInCard: true },
  { key: 'status', label: 'Status', showInCard: true },
  { key: 'due_at', label: 'Due', showInCard: true },
];

type SortKey = 'due' | 'priority' | 'status';
const PRIORITY_ORDER: Record<string, number> = { urgent: 0, high: 1, normal: 2, low: 3 };

function isOverdue(t: NurseTask): boolean {
  return t.status !== 'done' && t.status !== 'cancelled' && new Date(t.due_at).getTime() < Date.now();
}

export default function Tasks() {
  const { data, loading, error, reload } = useApi(() => api.tasks(), []);
  const [filter, setFilter] = useState<'all' | 'open' | 'urgent' | 'overdue'>('all');
  const [sortBy, setSortBy] = useState<SortKey>('due');

  const counts = useMemo(() => {
    if (!data) return { all: 0, open: 0, urgent: 0, overdue: 0 };
    return {
      all: data.length,
      open: data.filter((t) => t.status === 'open').length,
      urgent: data.filter((t) => t.priority === 'urgent').length,
      overdue: data.filter(isOverdue).length,
    };
  }, [data]);

  const filtered = useMemo(() => {
    if (!data) return [];
    let list = data;
    if (filter === 'open') list = data.filter((t) => t.status === 'open');
    else if (filter === 'urgent') list = data.filter((t) => t.priority === 'urgent');
    else if (filter === 'overdue') list = data.filter(isOverdue);

    return [...list].sort((a, b) => {
      if (sortBy === 'due') return new Date(a.due_at).getTime() - new Date(b.due_at).getTime();
      if (sortBy === 'priority') return (PRIORITY_ORDER[a.priority] ?? 9) - (PRIORITY_ORDER[b.priority] ?? 9);
      return a.status.localeCompare(b.status);
    });
  }, [data, filter, sortBy]);

  const { getRowProps } = useTableKeyboardNav(filtered.length);

  if (loading) return <Page title="Nurse tasks"><Loader what="tasks" /></Page>;
  if (error) return <Page title="Nurse tasks"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return <Page title="Nurse tasks"><Empty title="No tasks" /></Page>;
  }

  function sortHeader(label: string, key: SortKey) {
    const active = sortBy === key;
    return (
      <th
        className={`px-4 py-2 text-left cursor-pointer select-none hover:text-bodh-600 dark:hover:text-gray-200 ${active ? 'text-bodh-700 dark:text-gray-200 underline' : ''}`}
        onClick={() => setSortBy(key)}
      >
        {label} {active && '↓'}
      </th>
    );
  }

  return (
    <Page
      title="Nurse tasks"
      subtitle={`${filtered.length} of ${data.length}`}
      actions={<button onClick={reload} className="btn-secondary">Refresh</button>}
    >
      <div className="flex items-center gap-3 mb-2">
        <Tabs
          tabs={[
            { key: 'all', label: 'All', count: counts.all },
            { key: 'open', label: 'Open', count: counts.open },
            { key: 'urgent', label: 'Urgent', count: counts.urgent },
            { key: 'overdue', label: 'Overdue', count: counts.overdue },
          ]}
          active={filter}
          onChange={(k) => setFilter(k as typeof filter)}
          size="sm"
        />
      </div>

      <CardTable columns={COLUMNS} data={filtered as unknown as Record<string, unknown>[]} keyField="id">
        <div className="card overflow-hidden">
          <table className="min-w-full text-sm" aria-label="Nurse tasks">
            <thead className="bg-bodh-50 dark:bg-gray-800 text-bodh-400 dark:text-gray-500 text-xs uppercase tracking-wide">
              <tr>
                <th className="px-4 py-2 text-left">Subject</th>
                <th className="px-4 py-2 text-left">Patient</th>
                <th className="px-4 py-2 text-left">Kind</th>
                {sortHeader('Priority', 'priority')}
                {sortHeader('Status', 'status')}
                {sortHeader('Due', 'due')}
              </tr>
            </thead>
            <tbody>
              {filtered.map((t, i) => {
                const overdue = isOverdue(t);
                const rp = getRowProps(i);
                return (
                  <tr
                    key={t.id}
                    {...rp}
                    className={`border-t border-bodh-100 dark:border-gray-700 ${overdue ? 'bg-rose-50 dark:bg-rose-900/20' : ''} ${rp.className}`}
                  >
                    <td className="px-4 py-2 font-medium dark:text-gray-100">
                      {t.subject}
                      {overdue && <span className="ml-2 text-xs text-rose-600 dark:text-rose-400 font-normal">overdue</span>}
                    </td>
                    <td className="px-4 py-2 font-mono text-xs dark:text-gray-400">{t.patient_id}</td>
                    <td className="px-4 py-2 text-bodh-400 dark:text-gray-500 text-xs">{t.kind}</td>
                    <td className="px-4 py-2"><span className={`badge badge-${t.priority === 'urgent' ? 'urgent' : 'normal'}`}>{t.priority}</span></td>
                    <td className="px-4 py-2"><span className={`badge ${t.status === 'done' ? 'badge-done' : 'badge-normal'}`}>{t.status}</span></td>
                    <td className="px-4 py-2 text-xs text-bodh-400 dark:text-gray-500">
                      {new Date(t.due_at).toLocaleString()}
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

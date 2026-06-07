import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { SearchInput } from '../components/SearchInput';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';

const PAGE_SIZE = 20;

export default function Patients() {
  const { data, loading, error, reload } = useApi(() => api.patients(), []);
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(0);

  const filtered = useMemo(() => {
    if (!data) return [];
    if (!search) return data;
    const q = search.toLowerCase();
    return data.filter(
      (p) =>
        p.patient_id.toLowerCase().includes(q) ||
        (p.name ?? '').toLowerCase().includes(q) ||
        (p.conditions ?? []).some((c) => c.toLowerCase().includes(q)),
    );
  }, [data, search]);

  // Reset to first page when search changes
  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const safePage = Math.min(page, totalPages - 1);
  const pageSlice = filtered.slice(safePage * PAGE_SIZE, (safePage + 1) * PAGE_SIZE);

  if (loading) return <Page title="Patients"><Loader what="patients" /></Page>;
  if (error) return <Page title="Patients"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return <Page title="Patients"><Empty title="Registry is empty" hint="POST /api/patients to add a record (or run a care demo)." /></Page>;
  }
  return (
    <Page
      title="Patients"
      subtitle={`${filtered.length} in registry`}
      actions={<button onClick={reload} className="btn-secondary">Refresh</button>}
    >
      <div className="mb-3 max-w-sm">
        <SearchInput
          placeholder="Search by ID, name, or condition…"
          onSearch={(q) => { setSearch(q); setPage(0); }}
        />
      </div>

      <div className="card overflow-hidden">
        <table className="min-w-full text-sm" aria-label="Patient registry">
          <thead className="bg-bodh-50 dark:bg-gray-800 text-bodh-400 dark:text-gray-500 text-xs uppercase tracking-wide">
            <tr>
              <th className="px-4 py-2 text-left">Patient ID</th>
              <th className="px-4 py-2 text-left">Name</th>
              <th className="px-4 py-2 text-left">Conditions</th>
              <th className="px-4 py-2 text-left">Meds</th>
              <th className="px-4 py-2 text-left">Last A1c</th>
              <th className="px-4 py-2 text-left">Last BP</th>
              <th className="px-4 py-2 text-left">Discharged</th>
            </tr>
          </thead>
          <tbody>
            {pageSlice.map((p) => (
              <tr key={p.patient_id} className="border-t border-bodh-100 dark:border-gray-700 hover:bg-bodh-50 dark:hover:bg-gray-800/50">
                <td className="px-4 py-2 font-mono text-xs">
                  <Link to={`/patients/${p.patient_id}`} className="text-bodh-600 dark:text-bodh-300 hover:underline">
                    {p.patient_id}
                  </Link>
                </td>
                <td className="px-4 py-2 dark:text-gray-100">{p.name ?? '–'}</td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {(p.conditions ?? []).map((c) => <span key={c} className="badge badge-normal">{c}</span>)}
                  </div>
                </td>
                <td className="px-4 py-2 text-xs text-bodh-400 dark:text-gray-500">{(p.medications ?? []).join(', ') || '–'}</td>
                <td className="px-4 py-2 dark:text-gray-300">{p.last_a1c ?? '–'}</td>
                <td className="px-4 py-2 dark:text-gray-300">{p.last_bp_systolic ? `${p.last_bp_systolic}/${p.last_bp_diastolic}` : '–'}</td>
                <td className="px-4 py-2 text-xs text-bodh-400 dark:text-gray-500">{p.discharged_at ? new Date(p.discharged_at).toLocaleString() : '–'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between mt-3 text-sm text-bodh-400 dark:text-gray-500">
          <span>Page {safePage + 1} of {totalPages}</span>
          <div className="flex gap-1">
            <button
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              disabled={safePage === 0}
              className="btn-secondary text-xs disabled:opacity-40"
            >
              ← Prev
            </button>
            <button
              onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
              disabled={safePage >= totalPages - 1}
              className="btn-secondary text-xs disabled:opacity-40"
            >
              Next →
            </button>
          </div>
        </div>
      )}
    </Page>
  );
}

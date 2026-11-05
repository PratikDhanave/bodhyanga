import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { useLiveResource } from '../hooks/useLiveResource';
import { api } from '../api/client';

export default function Patients() {
  const { data, loading, error, reload } = useLiveResource(() => api.patients(), [], []);
  if (loading) return <Page title="Patients"><Loader what="patients" /></Page>;
  if (error) return <Page title="Patients"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return <Page title="Patients"><Empty title="Registry is empty" hint="POST /api/patients to add a record (or run a care demo)." /></Page>;
  }
  return (
    <Page title="Patients" subtitle={`${data.length} in registry`}>
      <div className="card overflow-hidden">
        <table className="min-w-full text-sm">
          <thead className="bg-bodhyanga-50 text-bodhyanga-400 text-xs uppercase tracking-wide">
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
            {data.map((p) => (
              <tr key={p.patient_id} className="border-t border-bodhyanga-100">
                <td className="px-4 py-2 font-mono text-xs">{p.patient_id}</td>
                <td className="px-4 py-2">{p.name ?? '–'}</td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {(p.conditions ?? []).map((c) => <span key={c} className="badge badge-normal">{c}</span>)}
                  </div>
                </td>
                <td className="px-4 py-2 text-xs text-bodhyanga-400">{(p.medications ?? []).join(', ') || '–'}</td>
                <td className="px-4 py-2">{p.last_a1c ?? '–'}</td>
                <td className="px-4 py-2">{p.last_bp_systolic ? `${p.last_bp_systolic}/${p.last_bp_diastolic}` : '–'}</td>
                <td className="px-4 py-2 text-xs text-bodhyanga-400">{p.discharged_at ? new Date(p.discharged_at).toLocaleString() : '–'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Page>
  );
}

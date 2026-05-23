import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';

export default function PanelReport() {
  const { data, loading, error, reload } = useApi(() => api.panel(), []);
  if (loading) return <Page title="Panel report"><Loader what="panel" /></Page>;
  if (error) return <Page title="Panel report"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.gaps.length === 0) {
    return <Page title="Panel report"><Empty title="No gaps detected" hint="Run cmd/caredemo -workflow=panel to populate the registry." /></Page>;
  }

  return (
    <Page title="Panel report" subtitle={`${data.patients} patients · ${data.gaps.length} open gaps`}>
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="lg:col-span-2">
          <div className="card overflow-hidden">
            <div className="card-header">Care gaps</div>
            <table className="min-w-full text-sm">
              <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
                <tr>
                  <th className="px-4 py-2 text-left">Patient</th>
                  <th className="px-4 py-2 text-left">Measure</th>
                  <th className="px-4 py-2 text-left">Severity</th>
                  <th className="px-4 py-2 text-left">Reason</th>
                </tr>
              </thead>
              <tbody>
                {data.gaps.map((g, i) => (
                  <tr key={`${g.patient_id}-${g.measure}-${i}`} className="border-t border-bodh-100">
                    <td className="px-4 py-2 font-mono text-xs">{g.patient_id}</td>
                    <td className="px-4 py-2">{g.measure}</td>
                    <td className="px-4 py-2"><span className={`badge badge-${g.severity === 'high' || g.severity === 'urgent' ? 'urgent' : 'normal'}`}>{g.severity}</span></td>
                    <td className="px-4 py-2 text-xs text-bodh-400">{g.reason}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
        <div className="card">
          <div className="card-header">By condition</div>
          <div className="card-body space-y-2 text-sm">
            {Object.entries(data.by_condition).map(([k, v]) => (
              <div key={k} className="flex justify-between border-b border-bodh-100 pb-1 last:border-0">
                <span className="font-medium">{k}</span>
                <span className="font-mono text-bodh-600">{v}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </Page>
  );
}

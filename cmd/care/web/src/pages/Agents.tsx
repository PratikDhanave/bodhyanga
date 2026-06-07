import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';

export default function Agents() {
  const { data, loading, error, reload } = useApi(() => api.agents(), []);

  if (loading) return <Page title="Agents"><Loader what="agents" /></Page>;
  if (error) return <Page title="Agents"><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return <Page title="Agents"><Empty title="No agents registered" hint="cmd/care registers all agents at startup." /></Page>;
  }

  return (
    <Page title="Agents" subtitle={`${data.length} registered`}>
      <div className="card">
        <table className="min-w-full text-sm" aria-label="Registered agents">
          <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
            <tr>
              <th className="px-4 py-2 text-left">ID</th>
              <th className="px-4 py-2 text-left">Name</th>
              <th className="px-4 py-2 text-left">Capabilities</th>
            </tr>
          </thead>
          <tbody>
            {data.map((a) => (
              <tr key={a.id} className="border-t border-bodh-100">
                <td className="px-4 py-2 font-mono text-xs">{a.id}</td>
                <td className="px-4 py-2">{a.name}</td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {a.capabilities.map((c) => (
                      <span key={c} className="badge badge-normal">{c}</span>
                    ))}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Page>
  );
}

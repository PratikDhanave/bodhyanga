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
  { key: 'id', label: 'ID', showInCard: true, primary: true },
  { key: 'name', label: 'Name', showInCard: true },
  { key: 'capabilities', label: 'Capabilities', showInCard: true },
];

export default function Agents() {
  const { t } = useI18n();
  const { data, loading, error, reload } = useApi(() => api.agents(), []);
  const { getRowProps } = useTableKeyboardNav(data?.length ?? 0);

  if (loading) return <Page title={t('agents.title')}><Loader what="agents" /></Page>;
  if (error) return <Page title={t('agents.title')}><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.length === 0) {
    return <Page title={t('agents.title')}><Empty title={t('agents.noAgents')} hint={t('agents.noAgents_hint')} /></Page>;
  }

  return (
    <Page title={t('agents.title')} subtitle={`${data.length} registered`}>
      <CardTable columns={COLUMNS} data={data as unknown as Record<string, unknown>[]} keyField="id">
        <div className="card">
          <table className="min-w-full text-sm" aria-label="Registered agents">
            <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
              <tr>
                <th className="px-4 py-2 text-left">{t('agents.col_id')}</th>
                <th className="px-4 py-2 text-left">{t('agents.col_name')}</th>
                <th className="px-4 py-2 text-left">{t('agents.col_capabilities')}</th>
              </tr>
            </thead>
            <tbody>
              {data.map((a, i) => {
                const rp = getRowProps(i);
                return (
                <tr key={a.id} {...rp} className={`border-t border-bodh-100 ${rp.className}`}>
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
                );
              })}
            </tbody>
          </table>
        </div>
      </CardTable>
    </Page>
  );
}

import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import CardTable from '../components/CardTable';
import type { CardColumn } from '../components/CardTable';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';
import { useI18n } from '../i18n';
import { useTableKeyboardNav } from '../hooks/useTableKeyboardNav';

const PANEL_COLUMNS: CardColumn[] = [
  { key: 'patient_id', label: 'Patient', showInCard: true, primary: true },
  { key: 'measure', label: 'Measure', showInCard: true },
  { key: 'severity', label: 'Severity', showInCard: true },
  { key: 'reason', label: 'Reason', showInCard: true },
];

export default function PanelReport() {
  const { t } = useI18n();
  const { data, loading, error, reload } = useApi(() => api.panel(), []);
  const { getRowProps } = useTableKeyboardNav(data?.gaps?.length ?? 0);
  if (loading) return <Page title={t('panelReport.title')}><Loader what="panel" /></Page>;
  if (error) return <Page title={t('panelReport.title')}><ErrorBox error={error} retry={reload} /></Page>;
  if (!data || data.gaps.length === 0) {
    return <Page title={t('panelReport.title')}><Empty title={t('panelReport.noGaps')} hint={t('panelReport.noGaps_hint')} /></Page>;
  }

  return (
    <Page title={t('panelReport.title')} subtitle={`${data.patients} patients · ${data.gaps.length} open gaps`}>
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="lg:col-span-2">
          <CardTable columns={PANEL_COLUMNS} data={data.gaps as unknown as Record<string, unknown>[]} keyField="measure">
            <div className="card overflow-hidden">
              <div className="card-header">{t('panelReport.careGaps')}</div>
              <table className="min-w-full text-sm" aria-label="Panel report metrics">
                <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
                  <tr>
                    <th className="px-4 py-2 text-left">{t('panelReport.col_patient')}</th>
                    <th className="px-4 py-2 text-left">{t('panelReport.col_measure')}</th>
                    <th className="px-4 py-2 text-left">{t('panelReport.col_severity')}</th>
                    <th className="px-4 py-2 text-left">{t('panelReport.col_reason')}</th>
                  </tr>
                </thead>
                <tbody>
                  {data.gaps.map((g, i) => {
                    const rp = getRowProps(i);
                    return (
                    <tr key={`${g.patient_id}-${g.measure}-${i}`} {...rp} className={`border-t border-bodh-100 ${rp.className}`}>
                      <td className="px-4 py-2 font-mono text-xs">{g.patient_id}</td>
                      <td className="px-4 py-2">{g.measure}</td>
                      <td className="px-4 py-2"><span className={`badge badge-${g.severity === 'high' || g.severity === 'urgent' ? 'urgent' : 'normal'}`}>{g.severity}</span></td>
                      <td className="px-4 py-2 text-xs text-bodh-400">{g.reason}</td>
                    </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </CardTable>
        </div>
        <div className="card">
          <div className="card-header">{t('panelReport.byCondition')}</div>
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

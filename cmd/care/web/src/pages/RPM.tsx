import { useMemo } from 'react';
import Page from '../components/Page';
import CardTable from '../components/CardTable';
import type { CardColumn } from '../components/CardTable';
import { useI18n } from '../i18n';
import { useTableKeyboardNav } from '../hooks/useTableKeyboardNav';

// Stub HR history for demo purposes — each patient has 7 readings (last 7 days).
// When the real /rpm/feed endpoint lands, replace this with useApi().
const STUB_PATIENTS: Array<{
  patient_id: string;
  name: string;
  last_hr: number;
  hr_history: number[]; // 7 values, oldest first
  last_spo2: number;
  last_bp: string;
  alert?: string;
}> = [
  {
    patient_id: 'P-001',
    name: 'Alice Okafor',
    last_hr: 88,
    hr_history: [72, 75, 80, 84, 91, 86, 88],
    last_spo2: 97,
    last_bp: '128/82',
    alert: 'HR trending up',
  },
  {
    patient_id: 'P-002',
    name: 'Bob Lindström',
    last_hr: 64,
    hr_history: [68, 66, 65, 63, 62, 64, 64],
    last_spo2: 99,
    last_bp: '118/74',
  },
  {
    patient_id: 'P-003',
    name: 'Carlos Reyes',
    last_hr: 102,
    hr_history: [88, 90, 94, 98, 100, 99, 102],
    last_spo2: 95,
    last_bp: '145/90',
    alert: 'Tachycardia + elevated BP',
  },
  {
    patient_id: 'P-004',
    name: 'Daria Novak',
    last_hr: 76,
    hr_history: [78, 77, 75, 74, 76, 77, 76],
    last_spo2: 98,
    last_bp: '122/78',
  },
  {
    patient_id: 'P-005',
    name: 'Emi Tanaka',
    last_hr: 58,
    hr_history: [62, 60, 59, 57, 56, 58, 58],
    last_spo2: 99,
    last_bp: '110/68',
  },
];

const RPM_COLUMNS: CardColumn[] = [
  { key: 'name', label: 'Patient', showInCard: true, primary: true },
  { key: 'patient_id', label: 'ID', showInCard: true },
  { key: 'last_hr', label: 'HR', showInCard: true },
  { key: 'last_spo2', label: 'SpO2', showInCard: true },
  { key: 'last_bp', label: 'BP', showInCard: true },
  { key: 'alert', label: 'Alert', showInCard: true },
];

// ---- Mini HR sparkline ----
function HRSparkline({ readings }: { readings: number[] }) {
  const W = 70;
  const H = 24;
  const PAD = 2;

  const points = useMemo(() => {
    if (readings.length === 0) return '';
    const minV = Math.min(...readings);
    const maxV = Math.max(...readings, minV + 1);
    const stepX = (W - PAD * 2) / Math.max(readings.length - 1, 1);
    return readings
      .map((v, i) => {
        const x = PAD + i * stepX;
        const y = PAD + ((maxV - v) / (maxV - minV)) * (H - PAD * 2);
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      })
      .join(' ');
  }, [readings]);

  const last = readings[readings.length - 1] ?? 0;
  const prev = readings[readings.length - 2] ?? last;
  const trending = last > prev + 5 ? '#ef4444' : last < prev - 5 ? '#6366f1' : '#10b981';

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      width={W}
      height={H}
      style={{ display: 'inline-block', verticalAlign: 'middle' }}
      aria-label={`HR sparkline: ${readings.join(', ')} bpm`}
    >
      <polyline
        points={points}
        fill="none"
        stroke={trending}
        strokeWidth="1.5"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}

export default function RPM() {
  const { t } = useI18n();
  const { getRowProps } = useTableKeyboardNav(STUB_PATIENTS.length);
  const alertCount = STUB_PATIENTS.filter((p) => p.alert).length;

  return (
    <Page title={t('rpm.title')} subtitle={t('rpm.subtitle')}>
      {/* Synthetic data safeguard notice */}
      <div className="rounded-lg border border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-900/20 px-4 py-2 text-sm text-amber-800 dark:text-amber-300 flex items-center gap-2 mb-4" role="status">
        <span className="font-bold">⚠</span>
        <span>Synthetic data — not connected to live monitoring systems</span>
      </div>

      {/* Alert summary banner */}
      {alertCount > 0 && (
        <div className="mb-4 rounded-lg border border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-900/20 px-4 py-3 flex items-center gap-2">
          <span className="text-amber-600 dark:text-amber-400 font-bold text-lg">!</span>
          <span className="text-sm text-amber-800 dark:text-amber-300">
            {alertCount} {alertCount > 1 ? t('rpm.alertBanner_plural') : t('rpm.alertBanner')}
          </span>
        </div>
      )}

      <CardTable columns={RPM_COLUMNS} data={STUB_PATIENTS as unknown as Record<string, unknown>[]} keyField="patient_id">
        <div className="card overflow-hidden">
          <table className="min-w-full text-sm" aria-label="Patient monitoring observations">
            <thead className="bg-bodh-50 dark:bg-gray-800 text-bodh-400 dark:text-gray-500 text-xs uppercase tracking-wide">
              <tr>
                <th className="px-4 py-2 text-left">{t('rpm.col_patient')}</th>
                <th className="px-4 py-2 text-left">{t('rpm.col_hr')}</th>
                <th className="px-4 py-2 text-left">{t('rpm.col_trend')}</th>
                <th className="px-4 py-2 text-right">{t('rpm.col_spo2')}</th>
                <th className="px-4 py-2 text-right">{t('rpm.col_bp')}</th>
                <th className="px-4 py-2 text-left">{t('rpm.col_alert')}</th>
              </tr>
            </thead>
            <tbody>
              {STUB_PATIENTS.map((p, i) => {
                const rp = getRowProps(i);
                return (
                <tr key={p.patient_id} {...rp} className={`border-t border-bodh-100 dark:border-gray-700 ${p.alert ? 'bg-amber-50 dark:bg-amber-900/20' : ''} ${rp.className}`}>
                  <td className="px-4 py-2">
                    <div className="font-medium dark:text-gray-100">{p.name}</div>
                    <div className="text-xs text-bodh-400 dark:text-gray-500">{p.patient_id}</div>
                  </td>
                  <td className="px-4 py-2 font-mono dark:text-gray-300">{p.last_hr}</td>
                  <td className="px-4 py-2">
                    <HRSparkline readings={p.hr_history} />
                  </td>
                  <td className="px-4 py-2 text-right dark:text-gray-300">{p.last_spo2}%</td>
                  <td className="px-4 py-2 text-right font-mono dark:text-gray-300">{p.last_bp}</td>
                  <td className="px-4 py-2">
                    {p.alert ? (
                      <span className="inline-flex items-center gap-1 text-amber-700 dark:text-amber-400 text-xs font-medium">
                        <span>!</span>
                        {p.alert}
                      </span>
                    ) : (
                      <span className="text-xs text-bodh-300 dark:text-gray-600">—</span>
                    )}
                  </td>
                </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </CardTable>
      <p className="text-xs text-bodh-400 dark:text-gray-500">
        {t('rpm.demoNote')}
      </p>
    </Page>
  );
}

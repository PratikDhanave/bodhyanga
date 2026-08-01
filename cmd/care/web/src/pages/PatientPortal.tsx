import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { useLiveResource } from '../hooks/useLiveResource';
import { api } from '../api/client';
import { useI18n } from '../i18n';

// ---------------------------------------------------------------------------
// Patient Portal — read-only patient-facing view
// Care plan, upcoming panel gaps, recent vitals table
// ---------------------------------------------------------------------------

export default function PatientPortal() {
  const { t } = useI18n();
  const patients = useLiveResource(() => api.patients(), [], ['care_plan_ready','panel_report','adt_event']);
  const panel = useLiveResource(() => api.panel(), [], ['care_plan_ready','panel_report','adt_event']);

  if (patients.loading || panel.loading) {
    return (
      <Page title={t('portal.title')}>
        <Loader what={t('common.loading')} />
      </Page>
    );
  }

  if (patients.error) {
    return (
      <Page title={t('portal.title')}>
        <ErrorBox error={patients.error} retry={patients.reload} />
      </Page>
    );
  }

  const patient = patients.data?.[0] ?? null;

  if (!patient) {
    return (
      <Page title={t('portal.title')}>
        <Empty title={t('portal.no_patient')} hint={t('portal.no_patient_hint')} />
      </Page>
    );
  }

  // Panel gaps relevant to this patient
  const gaps = (panel.data?.gaps ?? []).filter(
    (g) => g.patient_id === patient.patient_id,
  );

  return (
    <Page title={t('portal.title')} subtitle={`${t('portal.welcome')}${patient.name ? `, ${patient.name}` : ''}`}>
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Care plan card */}
        <CarePlanCard patient={patient} />

        {/* Upcoming panel gaps */}
        <PanelGapsCard gaps={gaps} />
      </div>

      {/* RPM vitals table */}
      <VitalsTable patient={patient} />
    </Page>
  );
}

// ---------------------------------------------------------------------------
// Care plan display
// ---------------------------------------------------------------------------
function CarePlanCard({ patient }: { patient: import('../types').PatientRecord }) {
  const { t } = useI18n();
  const conditions = patient.conditions ?? [];
  const medications = patient.medications ?? [];

  // Derive next appointment date as +30 days from today (placeholder)
  const nextAppt = new Date();
  nextAppt.setDate(nextAppt.getDate() + 30);

  return (
    <div className="card">
      <div className="card-header">{t('portal.care_plan')}</div>
      <div className="card-body space-y-4 text-sm">
        <section>
          <h3 className="text-xs uppercase tracking-wide text-bodh-400 mb-2">{t('portal.conditions')}</h3>
          {conditions.length === 0 ? (
            <p className="text-bodh-400">{t('portal.no_conditions')}</p>
          ) : (
            <ul className="space-y-1">
              {conditions.map((c) => (
                <li key={c} className="flex items-center gap-2">
                  <span className="w-2 h-2 rounded-full bg-bodh-500 inline-block" />
                  {c}
                </li>
              ))}
            </ul>
          )}
        </section>

        <section>
          <h3 className="text-xs uppercase tracking-wide text-bodh-400 mb-2">{t('portal.medications')}</h3>
          {medications.length === 0 ? (
            <p className="text-bodh-400">{t('portal.no_medications')}</p>
          ) : (
            <ul className="space-y-1">
              {medications.map((m) => (
                <li key={m} className="flex items-center gap-2">
                  <span className="badge badge-normal">{m}</span>
                </li>
              ))}
            </ul>
          )}
        </section>

        <section>
          <h3 className="text-xs uppercase tracking-wide text-bodh-400 mb-2">{t('portal.goals')}</h3>
          <ul className="space-y-1 text-bodh-600">
            {conditions.includes('diabetes') && (
              <li>{t('portal.goal.a1c')}</li>
            )}
            {conditions.includes('hypertension') && (
              <li>{t('portal.goal.bp')}</li>
            )}
            <li>{t('portal.goal.followup')}</li>
            <li>{t('portal.goal.symptoms')}</li>
          </ul>
        </section>

        <section>
          <h3 className="text-xs uppercase tracking-wide text-bodh-400 mb-2">{t('portal.next_appointment')}</h3>
          <p className="text-bodh-700 font-medium">{nextAppt.toLocaleDateString(undefined, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' })}</p>
          <p className="text-xs text-bodh-400 mt-0.5">{t('portal.arrive_early')}</p>
        </section>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Upcoming panel gaps
// ---------------------------------------------------------------------------
function PanelGapsCard({ gaps }: { gaps: import('../types').PanelGap[] }) {
  const { t } = useI18n();

  function formatMeasure(measure: string): string {
    const key = `portal.measure.${measure}`;
    const translated = t(key);
    // If translation returns the key itself, the measure is unknown — show raw
    return translated === key ? measure : translated;
  }

  return (
    <div className="card">
      <div className="card-header">{t('portal.care_reminders')}</div>
      <div className="card-body space-y-3">
        {gaps.length === 0 && (
          <p className="text-sm text-bodh-400">{t('portal.up_to_date')}</p>
        )}
        {gaps.map((g, i) => (
          <div key={i} className={`rounded-md p-3 border ${g.severity === 'high' ? 'border-rose-200 bg-rose-50' : 'border-amber-200 bg-amber-50'}`}>
            <div className="text-sm font-medium text-bodh-800">{formatMeasure(g.measure)}</div>
            <p className="text-xs text-bodh-600 mt-1">{g.reason}</p>
            <span className={`badge mt-2 ${g.severity === 'high' ? 'badge-urgent' : 'badge-pending'}`}>{g.severity} {t('portal.priority')}</span>
          </div>
        ))}
        {/* Static example when no gaps exist for demo purposes */}
        {gaps.length === 0 && (
          <div className="rounded-md p-3 border border-bodh-100 bg-bodh-50">
            <div className="text-sm font-medium text-bodh-700">{t('portal.annual_wellness')}</div>
            <p className="text-xs text-bodh-400 mt-1">{t('portal.annual_wellness_hint')}</p>
          </div>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// RPM vitals table
// ---------------------------------------------------------------------------
function VitalsTable({ patient }: { patient: import('../types').PatientRecord }) {
  const { t } = useI18n();

  // Derive a synthetic vitals table from the stored aggregate values
  const rows: { date: string; measure: string; value: string; unit: string }[] = [];

  if (patient.last_a1c != null) {
    rows.push({ date: t('portal.vitals.most_recent'), measure: t('portal.vitals.a1c'), value: patient.last_a1c.toFixed(1), unit: '%' });
  }
  if (patient.last_bp_systolic != null) {
    rows.push({
      date: t('portal.vitals.most_recent'),
      measure: t('portal.vitals.bp'),
      value: `${patient.last_bp_systolic}/${patient.last_bp_diastolic ?? '–'}`,
      unit: 'mmHg',
    });
  }
  if (patient.last_weight_lbs != null) {
    rows.push({ date: t('portal.vitals.most_recent'), measure: t('portal.vitals.weight'), value: patient.last_weight_lbs.toFixed(1), unit: 'lbs' });
  }

  return (
    <div className="card overflow-hidden">
      <div className="card-header">{t('portal.vitals')}</div>
      {rows.length === 0 ? (
        <div className="card-body">
          <p className="text-sm text-bodh-400">{t('portal.no_vitals')}</p>
        </div>
      ) : (
        <table className="min-w-full text-sm">
          <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
            <tr>
              <th className="px-4 py-2 text-left">{t('portal.vitals.date')}</th>
              <th className="px-4 py-2 text-left">{t('portal.vitals.measure')}</th>
              <th className="px-4 py-2 text-left">{t('portal.vitals.value')}</th>
              <th className="px-4 py-2 text-left">{t('portal.vitals.unit')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r, i) => (
              <tr key={i} className="border-t border-bodh-100">
                <td className="px-4 py-2 text-xs text-bodh-400">{r.date}</td>
                <td className="px-4 py-2 font-medium">{r.measure}</td>
                <td className="px-4 py-2">{r.value}</td>
                <td className="px-4 py-2 text-xs text-bodh-400">{r.unit}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

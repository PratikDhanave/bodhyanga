import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';

// ---------------------------------------------------------------------------
// Patient Portal — read-only patient-facing view
// Care plan, upcoming panel gaps, recent vitals table
// ---------------------------------------------------------------------------

export default function PatientPortal() {
  const patients = useApi(() => api.patients(), []);
  const panel = useApi(() => api.panel(), []);

  if (patients.loading || panel.loading) {
    return (
      <Page title="Patient Portal">
        <Loader what="patient data" />
      </Page>
    );
  }

  if (patients.error) {
    return (
      <Page title="Patient Portal">
        <ErrorBox error={patients.error} retry={patients.reload} />
      </Page>
    );
  }

  const patient = patients.data?.[0] ?? null;

  if (!patient) {
    return (
      <Page title="Patient Portal">
        <Empty title="No patient record found" hint="Run a care demo or POST /api/patients to register a patient." />
      </Page>
    );
  }

  // Panel gaps relevant to this patient
  const gaps = (panel.data?.gaps ?? []).filter(
    (g) => g.patient_id === patient.patient_id,
  );

  return (
    <Page title="Patient Portal" subtitle={`Welcome${patient.name ? `, ${patient.name}` : ''}`}>
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
  const conditions = patient.conditions ?? [];
  const medications = patient.medications ?? [];

  // Derive next appointment date as +30 days from today (placeholder)
  const nextAppt = new Date();
  nextAppt.setDate(nextAppt.getDate() + 30);

  return (
    <div className="card">
      <div className="card-header">Your Care Plan</div>
      <div className="card-body space-y-4 text-sm">
        <section>
          <h3 className="text-xs uppercase tracking-wide text-bodh-400 mb-2">Conditions</h3>
          {conditions.length === 0 ? (
            <p className="text-bodh-400">No conditions on file.</p>
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
          <h3 className="text-xs uppercase tracking-wide text-bodh-400 mb-2">Medications</h3>
          {medications.length === 0 ? (
            <p className="text-bodh-400">No medications on file.</p>
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
          <h3 className="text-xs uppercase tracking-wide text-bodh-400 mb-2">Goals</h3>
          <ul className="space-y-1 text-bodh-600">
            {conditions.includes('diabetes') && (
              <li>Maintain A1c below 7.0%</li>
            )}
            {conditions.includes('hypertension') && (
              <li>Keep blood pressure below 130/80 mmHg</li>
            )}
            <li>Attend all scheduled follow-up appointments</li>
            <li>Report new symptoms via the portal within 24 hours</li>
          </ul>
        </section>

        <section>
          <h3 className="text-xs uppercase tracking-wide text-bodh-400 mb-2">Next Appointment</h3>
          <p className="text-bodh-700 font-medium">{nextAppt.toLocaleDateString(undefined, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' })}</p>
          <p className="text-xs text-bodh-400 mt-0.5">Please arrive 10 minutes early to complete check-in.</p>
        </section>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Upcoming panel gaps
// ---------------------------------------------------------------------------
function PanelGapsCard({ gaps }: { gaps: import('../types').PanelGap[] }) {
  return (
    <div className="card">
      <div className="card-header">Upcoming Care Reminders</div>
      <div className="card-body space-y-3">
        {gaps.length === 0 && (
          <p className="text-sm text-bodh-400">You are up to date on all preventive care measures.</p>
        )}
        {gaps.map((g, i) => (
          <div key={i} className={`rounded-md p-3 border ${g.severity === 'high' ? 'border-rose-200 bg-rose-50' : 'border-amber-200 bg-amber-50'}`}>
            <div className="text-sm font-medium text-bodh-800">{formatMeasure(g.measure)}</div>
            <p className="text-xs text-bodh-600 mt-1">{g.reason}</p>
            <span className={`badge mt-2 ${g.severity === 'high' ? 'badge-urgent' : 'badge-pending'}`}>{g.severity} priority</span>
          </div>
        ))}
        {/* Static example when no gaps exist for demo purposes */}
        {gaps.length === 0 && (
          <div className="rounded-md p-3 border border-bodh-100 bg-bodh-50">
            <div className="text-sm font-medium text-bodh-700">Annual Wellness Visit</div>
            <p className="text-xs text-bodh-400 mt-1">Your annual wellness check is due within the next 30 days.</p>
          </div>
        )}
      </div>
    </div>
  );
}

function formatMeasure(measure: string): string {
  const map: Record<string, string> = {
    a1c: 'Your A1c is due for review',
    bp: 'Blood pressure monitoring needed',
    mammogram: 'Mammogram screening due',
    colonoscopy: 'Colonoscopy screening due',
    flu_vaccine: 'Annual flu vaccine recommended',
  };
  return map[measure] ?? measure;
}

// ---------------------------------------------------------------------------
// RPM vitals table
// ---------------------------------------------------------------------------
function VitalsTable({ patient }: { patient: import('../types').PatientRecord }) {
  // Derive a synthetic vitals table from the stored aggregate values
  const rows: { date: string; measure: string; value: string; unit: string }[] = [];

  if (patient.last_a1c != null) {
    rows.push({ date: 'Most recent', measure: 'A1c', value: patient.last_a1c.toFixed(1), unit: '%' });
  }
  if (patient.last_bp_systolic != null) {
    rows.push({
      date: 'Most recent',
      measure: 'Blood Pressure',
      value: `${patient.last_bp_systolic}/${patient.last_bp_diastolic ?? '–'}`,
      unit: 'mmHg',
    });
  }
  if (patient.last_weight_lbs != null) {
    rows.push({ date: 'Most recent', measure: 'Weight', value: patient.last_weight_lbs.toFixed(1), unit: 'lbs' });
  }

  return (
    <div className="card overflow-hidden">
      <div className="card-header">Recent Vitals</div>
      {rows.length === 0 ? (
        <div className="card-body">
          <p className="text-sm text-bodh-400">No vitals on file.</p>
        </div>
      ) : (
        <table className="min-w-full text-sm">
          <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
            <tr>
              <th className="px-4 py-2 text-left">Date</th>
              <th className="px-4 py-2 text-left">Measure</th>
              <th className="px-4 py-2 text-left">Value</th>
              <th className="px-4 py-2 text-left">Unit</th>
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

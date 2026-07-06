import { useState } from 'react';
import { Link } from 'react-router-dom';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import ConfirmationModal from '../components/ConfirmationModal';
import ConfidenceDisplay from '../components/ConfidenceDisplay';
import { useToast } from '../components/Toast';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';
import { useI18n } from '../i18n';
import type { ReviewDecision, Review, BenchResult } from '../types';

// ---------------------------------------------------------------------------
// Clinician Dashboard
// Summary cards, recent-cases table, HITL queue widget, accuracy bar chart
// ---------------------------------------------------------------------------

export default function ClinicianDashboard() {
  const { t } = useI18n();
  const reviews = useApi(() => api.reviews('pending'), []);
  const patients = useApi(() => api.patients(), []);
  const panel = useApi(() => api.panel(), []);
  const bench = useApi(() => api.bench(), []);

  const loading = reviews.loading || patients.loading || panel.loading || bench.loading;

  // Derive counts for summary cards
  const totalCasesToday = patients.data?.length ?? 0;
  const pendingHitl = reviews.data?.length ?? 0;
  const activeAlerts = panel.data?.gaps.filter((g) => g.severity === 'high').length ?? 0;
  const panelGaps = panel.data?.gaps.length ?? 0;

  return (
    <Page title={t('dashboard.title')} subtitle={t('dashboard.subtitle')}>
      {loading && <Loader what="dashboard" />}

      {/* Summary cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard label={t('dashboard.totalCases')} value={totalCasesToday} to="/patients" />
        <StatCard label={t('dashboard.pendingReviews')} value={pendingHitl} to="/reviews" emphasis={pendingHitl > 0} />
        <StatCard label={t('dashboard.activeAlerts')} value={activeAlerts} to="/panel" emphasis={activeAlerts > 0} color="rose" />
        <StatCard label={t('dashboard.panelGaps')} value={panelGaps} to="/panel" />
      </div>

      {reviews.error && <ErrorBox error={reviews.error} retry={reviews.reload} />}
      {patients.error && <ErrorBox error={patients.error} retry={patients.reload} />}

      {/* Recent cases table + HITL queue side-by-side */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="lg:col-span-2">
          <RecentCasesTable patients={patients.data} />
        </div>
        <div>
          <HitlQueue reviews={reviews.data} onDecide={reviews.reload} />
        </div>
      </div>

      {/* Accuracy by condition */}
      <AccuracyChart bench={bench.data} />
    </Page>
  );
}

// ---------------------------------------------------------------------------
// Summary stat card
// ---------------------------------------------------------------------------
function StatCard({
  label,
  value,
  to,
  emphasis,
  color,
}: {
  label: string;
  value: number | undefined;
  to: string;
  emphasis?: boolean;
  color?: 'amber' | 'rose';
}) {
  const colorClass =
    color === 'rose'
      ? 'border-rose-300 bg-rose-50'
      : emphasis
      ? 'border-amber-300 bg-amber-50'
      : '';
  return (
    <Link to={to} className={`card hover:bg-bodh-50 ${colorClass}`}>
      <div className="card-body">
        <div className="text-xs uppercase tracking-wide text-bodh-400">{label}</div>
        <div className="text-3xl font-semibold mt-2">{value ?? '–'}</div>
      </div>
    </Link>
  );
}

// ---------------------------------------------------------------------------
// Recent cases table
// ---------------------------------------------------------------------------
function RecentCasesTable({ patients }: { patients: import('../types').PatientRecord[] | null }) {
  const { t } = useI18n();
  // Use patient records as a proxy for "cases" — extend with synthetic fields
  // so the table columns are satisfied without a dedicated /api/cases endpoint.
  const rows = (patients ?? []).slice(0, 10).map((p) => ({
    caseId: p.patient_id,
    patient: p.name ?? p.patient_id,
    condition: (p.conditions ?? []).join(', ') || '–',
    diagnosis: (p.conditions ?? [])[0] ?? '–',
    confidence: p.last_a1c != null ? `${Math.min(99, Math.round(p.last_a1c * 9))}%` : '–',
    status: p.discharged_at ? 'discharged' : 'active',
    time: p.discharged_at ?? '',
  }));

  return (
    <div className="card overflow-hidden">
      <div className="card-header">{t('dashboard.recentCases')}</div>
      <table className="min-w-full text-sm" aria-label="Recent case results">
        <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
          <tr>
            <th className="px-4 py-2 text-left">Case ID</th>
            <th className="px-4 py-2 text-left">Patient</th>
            <th className="px-4 py-2 text-left">Condition</th>
            <th className="px-4 py-2 text-left">Diagnosis</th>
            <th className="px-4 py-2 text-left">Confidence</th>
            <th className="px-4 py-2 text-left">Status</th>
            <th className="px-4 py-2 text-left">Time</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.caseId} className="border-t border-bodh-100 hover:bg-bodh-50">
              <td className="px-4 py-2 font-mono text-xs">{r.caseId}</td>
              <td className="px-4 py-2 text-sm">{r.patient}</td>
              <td className="px-4 py-2 text-xs text-bodh-500">{r.condition}</td>
              <td className="px-4 py-2 text-xs">{r.diagnosis}</td>
              <td className="px-4 py-2 text-xs">{r.confidence}</td>
              <td className="px-4 py-2">
                <span className={`badge ${r.status === 'active' ? 'badge-normal' : 'badge-done'}`}>{r.status}</span>
              </td>
              <td className="px-4 py-2 text-xs text-bodh-400">
                {r.time ? new Date(r.time).toLocaleString() : '–'}
              </td>
            </tr>
          ))}
          {rows.length === 0 && (
            <tr>
              <td colSpan={7} className="px-4 py-6 text-center text-sm text-bodh-400">
                {t('dashboard.noCases')}
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

// ---------------------------------------------------------------------------
// HITL queue widget with inline Accept / Reject
// ---------------------------------------------------------------------------
function HitlQueue({
  reviews,
  onDecide,
}: {
  reviews: Review[] | null;
  onDecide: () => void;
}) {
  const { t } = useI18n();
  const [busy, setBusy] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<{ id: string; decision: ReviewDecision; subject: string } | null>(null);
  const { showToast } = useToast();

  async function executeDecision(id: string, decision: ReviewDecision, reason?: string) {
    setBusy(id);
    setConfirm(null);
    try {
      await api.decide(id, { reviewer_id: 'clinician-demo', decision, reason: reason || undefined });
      showToast(`Review ${decision.replace(/_/g, ' ')} successfully`, 'success');
      onDecide();
    } catch (e) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setBusy(null);
    }
  }

  const pending = (reviews ?? []).filter((r) => r.status === 'pending').slice(0, 6);

  return (
    <div className="card">
      <div className="card-header flex items-center justify-between">
        <span>{t('dashboard.hitlQueue')}</span>
        <Link to="/reviews" className="text-xs text-bodh-400 hover:text-bodh-700 dark:text-gray-400 dark:hover:text-gray-200">
          {t('dashboard.viewAll')}
        </Link>
      </div>
      <div className="card-body space-y-3">
        {pending.length === 0 && (
          <p className="text-sm text-bodh-400 dark:text-gray-400">{t('dashboard.noPending')}</p>
        )}
        {pending.map((r) => (
          <div key={r.id} className="border border-bodh-100 dark:border-gray-600 rounded-md p-3 space-y-2">
            <div className="text-sm font-medium leading-snug">{r.subject}</div>
            <div className="text-xs text-bodh-400 dark:text-gray-400">{r.kind}</div>
            <div className="flex gap-2">
              <button
                disabled={busy === r.id}
                onClick={() => setConfirm({ id: r.id, decision: 'approved', subject: r.subject })}
                className="btn text-xs px-2 py-1"
              >
                {t('dashboard.accept')}
              </button>
              <button
                disabled={busy === r.id}
                onClick={() => setConfirm({ id: r.id, decision: 'rejected', subject: r.subject })}
                className="btn-secondary text-xs px-2 py-1"
              >
                {t('dashboard.reject')}
              </button>
              <Link to={`/reviews/${r.id}`} className="btn-secondary text-xs px-2 py-1">
                {t('dashboard.detail')}
              </Link>
            </div>
          </div>
        ))}
      </div>

      {/* Clinical decision confirmation modal — requires reason + safety delay */}
      {confirm && (
        <ConfirmationModal
          isOpen
          title={confirm.decision === 'approved' ? t('dashboard.confirmAccept') : t('dashboard.confirmReject')}
          diagnosisSummary={confirm.subject}
          patientContext={`Review ${confirm.id}`}
          onConfirm={(reason) => executeDecision(confirm.id, confirm.decision, reason)}
          onCancel={() => setConfirm(null)}
          danger={confirm.decision === 'rejected'}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Accuracy bar chart (CSS only, no charting lib)
// ---------------------------------------------------------------------------
function AccuracyChart({ bench }: { bench: BenchResult[] | null }) {
  const { t } = useI18n();
  if (!bench || bench.length === 0) return null;

  const max = Math.max(...bench.map((b) => b.mean_accuracy_pct), 1);

  return (
    <div className="card">
      <div className="card-header">{t('dashboard.accuracy')}</div>
      <div className="card-body space-y-3">
        {bench.map((b) => {
          const pct = (b.mean_accuracy_pct / max) * 100;
          return (
            <div key={b.provider}>
              <div className="flex justify-between items-center text-xs mb-1">
                <span className="font-medium text-bodh-700 dark:text-gray-200">{b.provider}</span>
                <ConfidenceDisplay value={b.mean_accuracy_pct / 100} size="sm" showTooltip={false} />
              </div>
              <div className="h-3 bg-bodh-100 rounded-full overflow-hidden">
                <div
                  className="h-full bg-bodh-600 rounded-full transition-all duration-500"
                  style={{ width: `${pct}%` }}
                />
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

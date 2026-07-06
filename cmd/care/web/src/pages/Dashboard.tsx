import { Link } from 'react-router-dom';
import Page from '../components/Page';
import Loader, { ErrorBox } from '../components/Loader';
import { useApi } from '../hooks/useApi';
import { api } from '../api/client';
import { useI18n } from '../i18n';

export default function Dashboard() {
  const { t } = useI18n();
  const agents = useApi(() => api.agents(), []);
  const tasks = useApi(() => api.tasks(), []);
  const reviews = useApi(() => api.reviews('pending'), []);
  const patients = useApi(() => api.patients(), []);

  return (
    <Page title={t('adminDashboard.title')} subtitle={t('adminDashboard.subtitle')}>
      {(agents.loading || tasks.loading || reviews.loading || patients.loading) && (
        <Loader what="dashboard counts" />
      )}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Stat label={t('adminDashboard.registeredAgents')} value={agents.data?.length} to="/agents" />
        <Stat label={t('adminDashboard.pendingHITL')} value={reviews.data?.length} to="/reviews" emphasis />
        <Stat label={t('adminDashboard.openTasks')} value={tasks.data?.filter((t) => t.status === 'open').length} to="/tasks" />
        <Stat label={t('adminDashboard.patientsInRegistry')} value={patients.data?.length} to="/patients" />
      </div>

      {agents.error && <ErrorBox error={agents.error} retry={agents.reload} />}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card title={t('adminDashboard.recentReviews')}>
          {reviews.data?.slice(0, 5).map((r) => (
            <Link key={r.id} to={`/reviews/${r.id}`} className="block py-2 border-b border-bodh-100 last:border-0 hover:bg-bodh-50 -mx-4 px-4">
              <div className="text-sm font-medium">{r.subject}</div>
              <div className="text-xs text-bodh-400 mt-0.5">
                {r.kind} · <span className={`badge badge-${r.status}`}>{r.status}</span>
              </div>
            </Link>
          ))}
          {reviews.data && reviews.data.length === 0 && (
            <p className="text-sm text-bodh-400 py-2">{t('adminDashboard.noPendingReviews')}</p>
          )}
        </Card>
        <Card title={t('adminDashboard.topOpenTasks')}>
          {tasks.data
            ?.filter((t) => t.status === 'open')
            .slice(0, 5)
            .map((t) => (
              <div key={t.id} className="py-2 border-b border-bodh-100 last:border-0">
                <div className="text-sm font-medium">{t.subject}</div>
                <div className="text-xs text-bodh-400 mt-0.5">
                  {t.patient_id} · <span className={`badge badge-${t.priority === 'urgent' ? 'urgent' : 'normal'}`}>{t.priority}</span>
                </div>
              </div>
            ))}
          {tasks.data && tasks.data.filter((t) => t.status === 'open').length === 0 && (
            <p className="text-sm text-bodh-400 py-2">{t('adminDashboard.noOpenTasks')}</p>
          )}
        </Card>
      </div>
    </Page>
  );
}

function Stat({ label, value, to, emphasis }: { label: string; value: number | undefined; to: string; emphasis?: boolean }) {
  return (
    <Link to={to} className={`card hover:bg-bodh-50 ${emphasis ? 'border-amber-300 bg-amber-50' : ''}`}>
      <div className="card-body">
        <div className="text-xs uppercase tracking-wide text-bodh-400">{label}</div>
        <div className="text-3xl font-semibold mt-2">{value ?? '–'}</div>
      </div>
    </Link>
  );
}

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="card">
      <div className="card-header">{title}</div>
      <div className="card-body">{children}</div>
    </div>
  );
}

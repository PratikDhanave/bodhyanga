import { useEffect, useMemo, useRef, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import Page from '../components/Page';
import { api } from '../api/client';
import { useI18n } from '../i18n';
import type { TraceEvent } from '../types';

/** MARA stage ordering for visual grouping */
const STAGE_ORDER: Record<string, number> = {
  intake: 0,
  test_plan: 1,
  diagnostic: 2,
  diagnosis: 3,
  care_plan: 4,
  run_complete: 5,
};

function stageOf(type: string): string {
  if (type in STAGE_ORDER) return type;
  if (type.startsWith('test_')) return 'test_plan';
  if (type.startsWith('diag')) return 'diagnostic';
  return 'other';
}

const STAGE_COLORS: Record<string, string> = {
  intake: 'border-blue-400 dark:border-blue-600',
  test_plan: 'border-amber-400 dark:border-amber-600',
  diagnostic: 'border-purple-400 dark:border-purple-600',
  diagnosis: 'border-green-500 dark:border-green-600',
  care_plan: 'border-emerald-400 dark:border-emerald-600',
  run_complete: 'border-gray-400 dark:border-gray-600',
  other: 'border-bodh-200 dark:border-gray-600',
};

export default function RunTrace() {
  const { t } = useI18n();
  const { runId = '' } = useParams();
  const [events, setEvents] = useState<TraceEvent[]>([]);
  const [closed, setClosed] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!runId) return;
    const close = api.traceEvents(
      runId,
      (e) => {
        try {
          const data: TraceEvent = JSON.parse(e.data);
          setEvents((es) => [...es, data]);
          if (data.type === 'diagnosis' || data.type === 'run_complete') {
            setClosed(true);
          }
        } catch (err) {
          setError(String(err));
        }
      },
      () => setClosed(true),
    );
    return close;
  }, [runId]);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [events.length]);

  // Summary stats
  const summary = useMemo(() => {
    if (events.length === 0) return null;
    const agents = new Set(events.map((e) => e.from).filter(Boolean));
    const stages = new Set(events.map((e) => stageOf(e.type)));
    const firstTs = new Date(events[0].ts).getTime();
    const lastTs = new Date(events[events.length - 1].ts).getTime();
    const durationMs = lastTs - firstTs;
    return { agents: agents.size, stages: stages.size, durationMs };
  }, [events]);

  // Group events by stage for visual grouping
  const grouped = useMemo(() => {
    const groups: Array<{ stage: string; events: Array<{ event: TraceEvent; index: number }> }> = [];
    let currentStage = '';
    for (let i = 0; i < events.length; i++) {
      const stage = stageOf(events[i].type);
      if (stage !== currentStage) {
        currentStage = stage;
        groups.push({ stage, events: [] });
      }
      groups[groups.length - 1].events.push({ event: events[i], index: i });
    }
    return groups;
  }, [events]);

  return (
    <Page
      title={t('runTrace.title')}
      subtitle={`Run ${runId} · ${events.length} event${events.length === 1 ? '' : 's'} ${closed ? '(closed)' : '(streaming)'}`}
      actions={<Link to="/runs" className="btn-secondary">{t('runTrace.newRun')}</Link>}
    >
      {error && (
        <div className="card border-rose-200 dark:border-rose-800 bg-rose-50 dark:bg-rose-900/20">
          <div className="card-body text-sm text-rose-800 dark:text-rose-300">{error}</div>
        </div>
      )}

      {/* Summary card */}
      {summary && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-4">
          <div className="card">
            <div className="card-body py-2">
              <div className="text-xs uppercase tracking-wide text-bodh-400 dark:text-gray-500">{t('runTrace.stat_events')}</div>
              <div className="text-2xl font-semibold dark:text-gray-100">{events.length}</div>
            </div>
          </div>
          <div className="card">
            <div className="card-body py-2">
              <div className="text-xs uppercase tracking-wide text-bodh-400 dark:text-gray-500">{t('runTrace.stat_agents')}</div>
              <div className="text-2xl font-semibold dark:text-gray-100">{summary.agents}</div>
            </div>
          </div>
          <div className="card">
            <div className="card-body py-2">
              <div className="text-xs uppercase tracking-wide text-bodh-400 dark:text-gray-500">{t('runTrace.stat_stages')}</div>
              <div className="text-2xl font-semibold dark:text-gray-100">{summary.stages}</div>
            </div>
          </div>
          <div className="card">
            <div className="card-body py-2">
              <div className="text-xs uppercase tracking-wide text-bodh-400 dark:text-gray-500">{t('runTrace.stat_duration')}</div>
              <div className="text-2xl font-semibold dark:text-gray-100">
                {summary.durationMs < 1000 ? `${summary.durationMs}ms` : `${(summary.durationMs / 1000).toFixed(1)}s`}
              </div>
            </div>
          </div>
        </div>
      )}

      <div className="card">
        <div className="card-header flex justify-between items-center dark:border-gray-700">
          <span className="dark:text-gray-100">{t('runTrace.messageStream')}</span>
          <span className={`badge ${closed ? 'badge-done' : 'badge-pending'}`}>{closed ? t('runTrace.status_closed') : t('runTrace.status_live')}</span>
        </div>
        <div ref={containerRef} className="max-h-[70vh] overflow-y-auto p-4 space-y-3 text-sm font-mono dark:bg-gray-900" aria-live="polite">
          {events.length === 0 && <p className="text-bodh-400 dark:text-gray-500 italic">{t('runTrace.waiting')}</p>}
          {grouped.map((group) => (
            <div key={`${group.stage}-${group.events[0]?.index}`} className="space-y-1">
              <div className="text-[10px] font-semibold uppercase tracking-widest text-bodh-300 dark:text-gray-600 pl-4">
                {group.stage.replace(/_/g, ' ')}
              </div>
              {group.events.map(({ event: e, index: i }) => (
                <div key={i} className={`border-l-2 ${STAGE_COLORS[stageOf(e.type)] ?? STAGE_COLORS.other} pl-3 py-1`}>
                  <div className="text-xs text-bodh-400 dark:text-gray-500">
                    <span className="font-mono">{new Date(e.ts).toLocaleTimeString()}</span> · <span className="badge badge-normal">{e.type}</span> · <span className="font-mono">{e.from}</span> → <span className="font-mono">{e.to || '(broadcast)'}</span>
                  </div>
                  {e.content_preview && (
                    <pre className="mt-1 whitespace-pre-wrap text-xs dark:text-gray-300">{e.content_preview}</pre>
                  )}
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    </Page>
  );
}

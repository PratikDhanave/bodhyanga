import { useEffect, useRef, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import Page from '../components/Page';
import { api } from '../api/client';
import type { TraceEvent } from '../types';

export default function RunTrace() {
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
          // If terminal type ("diagnosis"), close the stream
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

  return (
    <Page
      title="Run trace"
      subtitle={`Run ${runId} · ${events.length} event${events.length === 1 ? '' : 's'} ${closed ? '(closed)' : '(streaming)'}`}
      actions={<Link to="/runs" className="btn-secondary">New run</Link>}
    >
      {error && <div className="card border-rose-200 bg-rose-50"><div className="card-body text-sm text-rose-800">{error}</div></div>}
      <div className="card">
        <div className="card-header flex justify-between items-center">
          <span>Agent message stream</span>
          <span className={`badge ${closed ? 'badge-done' : 'badge-pending'}`}>{closed ? 'closed' : 'live'}</span>
        </div>
        <div ref={containerRef} className="max-h-[70vh] overflow-y-auto p-4 space-y-2 text-sm font-mono">
          {events.length === 0 && <p className="text-bodhyanga-400 italic">Waiting for first message…</p>}
          {events.map((e, i) => (
            <div key={i} className="border-l-2 border-bodhyanga-200 pl-3 py-1">
              <div className="text-xs text-bodhyanga-400">
                <span className="font-mono">{new Date(e.ts).toLocaleTimeString()}</span> · <span className={`badge badge-normal`}>{e.type}</span> · <span className="font-mono">{e.from}</span> → <span className="font-mono">{e.to || '(broadcast)'}</span>
              </div>
              {e.content_preview && (
                <pre className="mt-1 whitespace-pre-wrap text-xs">{e.content_preview}</pre>
              )}
            </div>
          ))}
        </div>
      </div>
    </Page>
  );
}

import { useState, useCallback } from 'react';
import Page from '../components/Page';
import { ErrorBox } from '../components/Loader';
import Empty from '../components/Empty';

// ---------------------------------------------------------------------------
// AuditEvent type — mirrors the HIPAA append-only audit log in pkg/audit
// ---------------------------------------------------------------------------
interface AuditEvent {
  id: string;
  ts: string;
  agent: string;
  kind: string;
  case_id?: string;
  decision?: string;
  reason?: string;
}

interface AuditPage {
  events: AuditEvent[];
  next_cursor?: string;
}

const BASE = '/api';

async function fetchAudit(params: {
  limit: number;
  after?: string;
  case_id?: string;
  kind?: string;
  agent?: string;
}): Promise<AuditPage> {
  const qs = new URLSearchParams({ limit: String(params.limit) });
  if (params.after) qs.set('after', params.after);
  if (params.case_id) qs.set('case_id', params.case_id);
  if (params.kind) qs.set('kind', params.kind);
  if (params.agent) qs.set('agent', params.agent);

  const res = await fetch(`${BASE}/audit?${qs.toString()}`, {
    headers: { 'Content-Type': 'application/json' },
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`HTTP ${res.status}: ${body}`);
  }
  // The endpoint may not exist yet — return empty page gracefully.
  try {
    const data = await res.json();
    // Handle both {events: [...]} and plain array shapes
    if (Array.isArray(data)) return { events: data };
    return data as AuditPage;
  } catch {
    return { events: [] };
  }
}

// ---------------------------------------------------------------------------
// AuditLog page
// ---------------------------------------------------------------------------
export default function AuditLog() {
  const [filterCaseId, setFilterCaseId] = useState('');
  const [filterKind, setFilterKind] = useState('');
  const [filterAgent, setFilterAgent] = useState('');

  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [hasMore, setHasMore] = useState(true);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [initialLoaded, setInitialLoaded] = useState(false);

  const load = useCallback(
    async (reset: boolean) => {
      setLoading(true);
      setError(null);
      try {
        const page = await fetchAudit({
          limit: 50,
          after: reset ? undefined : cursor,
          case_id: filterCaseId || undefined,
          kind: filterKind || undefined,
          agent: filterAgent || undefined,
        });
        if (reset) {
          setEvents(page.events);
        } else {
          setEvents((prev) => [...prev, ...page.events]);
        }
        setCursor(page.next_cursor);
        setHasMore(!!page.next_cursor && page.events.length === 50);
        setInitialLoaded(true);
      } catch (e) {
        setError(e instanceof Error ? e : new Error(String(e)));
      } finally {
        setLoading(false);
      }
    },
    [cursor, filterCaseId, filterKind, filterAgent],
  );

  // Load on first render
  if (!initialLoaded && !loading && !error) {
    load(true);
  }

  function applyFilters() {
    setCursor(undefined);
    setEvents([]);
    setHasMore(true);
    setInitialLoaded(false);
  }

  function clearFilters() {
    setFilterCaseId('');
    setFilterKind('');
    setFilterAgent('');
    setCursor(undefined);
    setEvents([]);
    setHasMore(true);
    setInitialLoaded(false);
  }

  return (
    <Page title="Audit Log" subtitle="HIPAA append-only event viewer">
      {/* Filters */}
      <div className="card">
        <div className="card-header">Filters</div>
        <div className="card-body">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <label className="block">
              <span className="text-xs uppercase tracking-wide text-bodh-400">Case ID</span>
              <input
                type="text"
                value={filterCaseId}
                onChange={(e) => setFilterCaseId(e.target.value)}
                placeholder="e.g. a3f1b9c2"
                className="mt-1 w-full border border-bodh-200 rounded px-2 py-1 text-sm"
              />
            </label>
            <label className="block">
              <span className="text-xs uppercase tracking-wide text-bodh-400">Event Kind</span>
              <input
                type="text"
                value={filterKind}
                onChange={(e) => setFilterKind(e.target.value)}
                placeholder="e.g. diagnosis, review_request"
                className="mt-1 w-full border border-bodh-200 rounded px-2 py-1 text-sm"
              />
            </label>
            <label className="block">
              <span className="text-xs uppercase tracking-wide text-bodh-400">Agent</span>
              <input
                type="text"
                value={filterAgent}
                onChange={(e) => setFilterAgent(e.target.value)}
                placeholder="e.g. diagnostician"
                className="mt-1 w-full border border-bodh-200 rounded px-2 py-1 text-sm"
              />
            </label>
          </div>
          <div className="flex gap-2 mt-3">
            <button onClick={applyFilters} className="btn">Apply</button>
            <button onClick={clearFilters} className="btn-secondary">Clear</button>
          </div>
        </div>
      </div>

      {/* Error state */}
      {error && <ErrorBox error={error} retry={() => load(true)} />}

      {/* Table */}
      {!error && (
        <div className="card overflow-hidden">
          <table className="min-w-full text-sm">
            <thead className="bg-bodh-50 text-bodh-400 text-xs uppercase tracking-wide">
              <tr>
                <th className="px-4 py-2 text-left">Timestamp</th>
                <th className="px-4 py-2 text-left">Agent</th>
                <th className="px-4 py-2 text-left">Kind</th>
                <th className="px-4 py-2 text-left">Case ID</th>
                <th className="px-4 py-2 text-left">Decision</th>
                <th className="px-4 py-2 text-left">Reason</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e) => (
                <tr key={e.id} className="border-t border-bodh-100 hover:bg-bodh-50">
                  <td className="px-4 py-2 text-xs text-bodh-400 whitespace-nowrap">
                    {new Date(e.ts).toLocaleString()}
                  </td>
                  <td className="px-4 py-2 font-mono text-xs">{e.agent}</td>
                  <td className="px-4 py-2">
                    <span className="badge badge-normal">{e.kind}</span>
                  </td>
                  <td className="px-4 py-2 font-mono text-xs">{e.case_id ?? '–'}</td>
                  <td className="px-4 py-2">
                    {e.decision ? (
                      <span className={`badge badge-${decisionBadge(e.decision)}`}>{e.decision}</span>
                    ) : (
                      <span className="text-bodh-300">–</span>
                    )}
                  </td>
                  <td className="px-4 py-2 text-xs text-bodh-500 max-w-xs truncate" title={e.reason}>
                    {e.reason ?? '–'}
                  </td>
                </tr>
              ))}
              {!loading && events.length === 0 && initialLoaded && (
                <tr>
                  <td colSpan={6} className="px-4 py-8">
                    <div className="text-center text-sm text-bodh-400">
                      No audit events found.
                    </div>
                  </td>
                </tr>
              )}
            </tbody>
          </table>

          {/* Loading spinner row */}
          {loading && (
            <div className="px-4 py-6 text-center text-sm text-bodh-400 animate-pulse">
              Loading audit events…
            </div>
          )}

          {/* Load more */}
          {!loading && hasMore && events.length > 0 && (
            <div className="px-4 py-3 border-t border-bodh-100 text-center">
              <button onClick={() => load(false)} className="btn-secondary">
                Load more
              </button>
            </div>
          )}

          {/* End-of-log indicator */}
          {!loading && !hasMore && events.length > 0 && (
            <div className="px-4 py-3 border-t border-bodh-100 text-center text-xs text-bodh-300">
              End of audit log ({events.length} events)
            </div>
          )}
        </div>
      )}

      {/* Empty state before first load */}
      {!loading && !error && events.length === 0 && !initialLoaded && (
        <Empty title="Loading audit events…" hint="Fetching from GET /api/audit" />
      )}
    </Page>
  );
}

function decisionBadge(decision: string): string {
  if (decision === 'approved') return 'approved';
  if (decision === 'rejected') return 'rejected';
  if (decision.startsWith('approved_with')) return 'approved';
  return 'normal';
}

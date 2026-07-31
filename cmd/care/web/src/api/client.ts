import type {
  Agent,
  CaseFixture,
  LLMTraceLog,
  Notification,
  PanelReport,
  PatientRecord,
  Review,
  ReviewDecision,
  RunRequest,
  RunSummary,
  BenchResult,
  NurseTask,
  CostAnalytics,
} from '../types';
import { getToken, clearToken } from '../hooks/useAuth';

// API base. In dev, Vite proxies /api to the Go backend on :8088.
// In prod, the Go binary serves the SPA from / and the API from /api on
// the same origin.
const BASE = '/api';

async function http<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getToken();
  const authHeader: Record<string, string> = token
    ? { Authorization: `Bearer ${token}` }
    : {};
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', ...authHeader, ...(init?.headers || {}) },
    ...init,
  });
  if (res.status === 401) {
    clearToken();
    window.location.href = '/ui/login';
    return undefined as T;
  }
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`HTTP ${res.status}: ${body}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

// streamTicket exchanges the session JWT (sent as a header) for a short-lived
// token that is safe to place in an EventSource/WebSocket URL. Returns '' on
// failure so the caller opens the stream unauthenticated (and the server 401s).
async function streamTicket(): Promise<string> {
  try {
    const r = await http<{ token: string }>('/auth/stream-ticket', { method: 'POST' });
    return r?.token ?? '';
  } catch {
    return '';
  }
}

export const api = {
  // Read-only
  agents: () => http<Agent[]>('/agents'),
  tasks: () => http<NurseTask[]>('/tasks'),
  reviews: (status?: 'pending' | 'all') =>
    http<Review[]>(`/reviews${status === 'pending' ? '?status=pending' : ''}`),
  review: (id: string) => http<Review>(`/reviews/${id}`),
  patients: () => http<PatientRecord[]>('/patients'),
  notifications: () => http<Notification[]>('/notifications'),
  fixtures: () => http<CaseFixture[]>('/fixtures'),
  panel: () => http<PanelReport>('/panel/report'),
  bench: () => http<BenchResult[]>('/bench/latest'),
  llmTraces: (limit = 100) => http<LLMTraceLog[]>(`/llm-traces?limit=${limit}`),
  costAnalytics: (tenant: string, from: string, to: string) =>
    http<CostAnalytics>(`/analytics/cost?tenant=${encodeURIComponent(tenant)}&from=${from}&to=${to}`),
  fineTuneStatus: () =>
    http<{
      sft_lines: number;
      dpo_pairs: number;
      decided_reviews: number;
      pending_reviews: number;
    }>('/fine-tune/status'),

  // Mutations
  decide: (id: string, body: { reviewer_id: string; decision: ReviewDecision; reason?: string; modification?: string }) =>
    http<Review>(`/reviews/${id}/decide`, { method: 'POST', body: JSON.stringify(body) }),
  startRun: (req: RunRequest) =>
    http<RunSummary>('/runs', { method: 'POST', body: JSON.stringify(req) }),

  // SSE. EventSource cannot set an Authorization header, so we mint a
  // short-lived stream ticket (60s) and pass it as ?access_token — a leak of a
  // URL-borne token then has a one-minute blast radius instead of the full
  // session. The ticket fetch is async; the returned unsubscribe closes the
  // stream whether or not it has opened yet.
  traceEvents: (runId: string, onEvent: (e: MessageEvent) => void, onError?: (e: Event) => void) => {
    let es: EventSource | null = null;
    let closed = false;
    void streamTicket().then((ticket) => {
      if (closed) return;
      const q = ticket ? `?access_token=${encodeURIComponent(ticket)}` : '';
      es = new EventSource(`${BASE}/runs/${runId}/events${q}`);
      es.onmessage = onEvent;
      if (onError) es.onerror = onError;
    });
    return () => {
      closed = true;
      es?.close();
    };
  },
};

export type { Agent, NurseTask, Review, ReviewDecision, PatientRecord, Notification, CaseFixture, RunRequest, RunSummary, BenchResult, LLMTraceLog, PanelReport };

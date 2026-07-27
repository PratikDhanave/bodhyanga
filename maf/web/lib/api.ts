// Typed client for the Bodh care API (pkg/careapi). All calls go through the
// same-origin /api/care proxy (see next.config.ts) so the browser never hits
// the loopback Go server cross-origin.

export type CaseStatus =
  | "running"
  | "awaiting_review"
  | "completed"
  | "rejected"
  | "denied"
  | "failed";

// Mirrors careapi.Entry — PHI-free: opaque tokens + decision-support output only.
export interface CaseEntry {
  case_token: string;
  session_id: string;
  status: CaseStatus;
  diagnosis?: string;
  confidence?: number;
  forced?: boolean;
  reviewer_id?: string;
  plan_steps?: string[];
  note?: string;
  cost_usd?: number;
}

// Mirrors careapi.handleResponse.
export interface SubmitHandle {
  session_id: string;
  case_token: string;
  status: CaseStatus;
}

// Mirrors careapi.reviewSummary.
export interface ReviewSummary {
  session_id: string;
  case_token: string;
  diagnosis: string;
  confidence: number;
  forced: boolean;
  cost_usd: number;
}

// Mirrors careapi.auditEvent.
export interface AuditEvent {
  kind: string;
  agent_id?: string;
  type?: string;
  decision?: string;
  reason?: string;
  case_token?: string;
}

export interface PatientCaseInput {
  case_id: string;
  symptoms: string[];
  history: string[];
}

/** Thrown when the care API is unreachable or returns a non-2xx status. */
export class CareApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
    this.name = "CareApiError";
  }
}

const BASE = "/api/care/v1";

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${BASE}${path}`, {
      ...init,
      headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
      cache: "no-store",
    });
  } catch {
    throw new CareApiError("Care API is not reachable. Is `cmd/care` running?", 0);
  }
  const text = await res.text();
  const body = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const msg = (body && (body.error as string)) || `Request failed (${res.status})`;
    throw new CareApiError(msg, res.status);
  }
  return body as T;
}

export const careApi = {
  submitCase: (c: PatientCaseInput) =>
    call<SubmitHandle>("/cases", { method: "POST", body: JSON.stringify(c) }),
  getCase: (token: string) => call<CaseEntry>(`/cases/${encodeURIComponent(token)}`),
  listReviews: () => call<{ reviews: ReviewSummary[] }>("/reviews"),
  answerReview: (sessionId: string, approved: boolean, reviewerId: string, note?: string) =>
    call<CaseEntry>(`/reviews/${encodeURIComponent(sessionId)}`, {
      method: "POST",
      body: JSON.stringify({ approved, reviewer_id: reviewerId, note }),
    }),
  getAudit: (token: string) =>
    call<{ case_token: string; events: AuditEvent[] }>(
      `/cases/${encodeURIComponent(token)}/audit`,
    ),
};

export const STATUS_LABEL: Record<CaseStatus, string> = {
  running: "Running",
  awaiting_review: "Awaiting review",
  completed: "Completed",
  rejected: "Rejected",
  denied: "Denied",
  failed: "Failed",
};

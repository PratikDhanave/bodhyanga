// Shared TypeScript types mirroring the Go API responses. These should
// match the JSON shapes emitted by cmd/care's /api/* endpoints.

export interface Agent {
  id: string;
  name: string;
  capabilities: string[];
}

export interface NurseTask {
  id: string;
  kind: string;
  patient_id: string;
  priority: 'urgent' | 'high' | 'normal' | 'low';
  status: 'open' | 'in_progress' | 'done' | 'cancelled';
  subject: string;
  detail?: string;
  due_at: string;
  created_at: string;
}

export type ReviewStatus =
  | 'pending'
  | 'approved'
  | 'approved_with_modifications'
  | 'rejected'
  | 'expired';

// Decision strings accepted by POST /api/reviews/{id}/decide.
export type ReviewDecision =
  | 'approved'
  | 'approved_with_modifications'
  | 'rejected';

export interface Review {
  id: string;
  kind: string;
  status: ReviewStatus;
  patient_id?: string;
  case_id?: string;
  subject: string;
  detail?: string;
  payload?: string;
  modification?: string;
  next_to?: string;
  next_type?: string;
  priority?: string;
  sla_at?: string;
  submitted_at: string;
  submitted_by?: string;
  reviewer_id?: string;
  reason?: string;
  decided_at?: string;
  meta?: Record<string, unknown>;
}

export interface PatientRecord {
  patient_id: string;
  name?: string;
  dob?: string;
  conditions?: string[];
  medications?: string[];
  last_a1c?: number;
  last_bp_systolic?: number;
  last_bp_diastolic?: number;
  last_weight_lbs?: number;
  discharged_at?: string;
}

export interface RPMObservation {
  patient_id: string;
  kind: string;
  value: number;
  unit?: string;
  observed_at: string;
}

export interface Notification {
  id: string;
  channel: string;
  to: string;
  template: string;
  body?: string;
  sent_at: string;
}

export interface CaseFixture {
  case_id: string;
  condition: string;
  chief_complaint: string;
  hpi: string;
  demographics?: string;
  vitals?: string;
  gold_diagnosis: string;
  max_cost_usd: number;
  file: string;
}

export interface RunRequest {
  fixture?: string;
  diagnostician?: string;
  presentation?: {
    chief_complaint: string;
    hpi: string;
    demographics?: string;
    vitals?: string;
    condition?: string;
  };
  max_cost_usd?: number;
  gold_diagnosis?: string;
}

export interface RunSummary {
  run_id: string;
  case_id: string;
  diagnostician: string;
  started_at: string;
}

export interface TraceEvent {
  ts: string;
  from: string;
  to: string;
  type: string;
  role: string;
  content_preview?: string;
  full_content?: string;
  metadata?: Record<string, unknown>;
}

export interface PanelGap {
  patient_id: string;
  measure: string;
  severity: string;
  reason: string;
}

export interface PanelReport {
  patients: number;
  gaps: PanelGap[];
  by_condition: Record<string, number>;
}

export interface BenchResult {
  provider: string;
  cases: number;
  mean_accuracy_pct: number;
  mean_cost_usd: number;
  under_budget_pct: number;
}

export interface LLMTraceLog {
  started_at: string;
  duration_ms: number;
  provider: string;
  model: string;
  agent_id?: string;
  case_id?: string;
  trace_id?: string;
  input_tokens: number;
  output_tokens: number;
  cached_tokens: number;
  stop_reason?: string;
  tool_calls?: number;
  error?: string;
  kind: string;
}

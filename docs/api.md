# Bodhyanga — HTTP API Reference

Bodhyanga's HTTP surface lives in `cmd/care`. Start it with `go run ./cmd/care -addr=:8088`.

> **No authentication is enforced today; do not put PHI through this binary.** The auth target architecture (SMART on FHIR + OIDC + mTLS) is described in [docs/compliance.md](compliance.md#authentication).

> **Message types reference:** [Message types](#message-types) section below.

---

## Contents

- [Conventions](#conventions)
- [Patient registry](#patient-registry)
- [Care services](#care-services)
- [Events](#events)
- [FHIR R4](#fhir-r4)
- [HL7 v2](#hl7-v2)
- [HITL reviews](#hitl-reviews)
- [Audit](#audit)
- [Reads](#reads)
- [Message types](#message-types)
- [Required metadata keys](#required-metadata-keys)
- [Error envelopes](#error-envelopes)

---

## Conventions

- **Body format:** JSON unless otherwise noted (HL7 v2 uses `application/hl7-v2` or `text/plain`).
- **Timestamps:** RFC3339 / ISO 8601 with timezone, UTC preferred.
- **IDs:** Opaque strings. `case_id` and `patient_id` are routing tokens, **never MRNs**.
- **Tenant:** Single-tenant today (set by `BODHYANGA_TENANT_ID` env var at boot). Multi-tenant per-request routing is planned.
- **Backends:** When `BODHYANGA_DB_DSN` is set, persistence is Postgres-backed (audit, eval, patient registry). Otherwise in-memory.

---

## Patient registry

### `POST /patients`

Upsert a patient record into the in-memory roster (and Postgres if configured).

**Body:** [`medical.PatientRecord`](data-model.md#patientrecord)

```json
{
  "patient_id": "pt-001",
  "name": "REDACTED",
  "age": 72,
  "sex": "F",
  "conditions": ["chf", "htn"],
  "last_a1c": 6.8,
  "last_a1c_date": "2026-03-15T00:00:00Z",
  "last_bp_systolic": 145,
  "rpm_enrolled": true
}
```

**Response:** 200 OK, empty body.

### `GET /patients`

List all registered patients.

**Response:** 200 OK, JSON array of `PatientRecord`.

---

## Care services

### `POST /cdm`

Publish a `cdm_request` for the chronic disease management workflow.

**Body:**
```json
{
  "patient_id": "pt-002",
  "condition": "dm",
  "case_id": "case-cdm-001"
}
```

`condition` ∈ `htn`, `dm`, `chf`, `ckd`, `copd`, `asthma`.

**Response:** 202 Accepted. CDM planner emits `care_plan_ready` async to the engagement agent (gated by `care_plan_review` if HITL is on).

### `POST /tcm/discharge`

Trigger TCM follow-ups.

**Body:** [`medical.DischargeSummary`](data-model.md#dischargesummary)

```json
{
  "patient_id": "pt-001",
  "case_id": "case-tcm-001",
  "primary_dx": "ADHF",
  "disposition": "home with home health",
  "home_meds": ["furosemide 40mg BID", "lisinopril 10mg daily"],
  "new_meds": ["spironolactone 25mg daily"],
  "stopped_meds": [],
  "follow_up_days": 7,
  "high_risk": true
}
```

**Response:** 202 Accepted. TCM coordinator publishes `tcm_tasks_created` with N nurse tasks and a TCM welcome SMS (gated by `tcm_welcome_review` if HITL is on).

### `POST /panel`

Run panel rules over the population roster.

**Body:**
```json
{ "condition": "all" }
```

`condition` ∈ `all`, `htn`, `dm`, `chf`, `copd`, `asthma`. Use `all` to evaluate every rule against every patient.

**Response:** 202 Accepted. Panel manager publishes `panel_report` (broadcast) and one `engagement_request` per detected gap.

### `POST /pre_visit`

Pre-visit chart prep.

**Body:**
```json
{ "patient_id": "pt-001", "encounter_id": "enc-2026-05-24-001" }
```

**Response:** 200 OK with `pre_visit_ready` summary inline (read-only operation).

### `POST /inbox`

Triage an inbound patient message.

**Body:**
```json
{
  "patient_id": "pt-001",
  "subject": "Need refill on metformin",
  "body": "Hi doctor, I'm running low on metformin. Can you send a refill to my pharmacy?"
}
```

**Response:** 202 Accepted. Inbox triage routes to `refill_request`, `prior_auth_request`, or queues an urgent nurse task.

### `POST /refill`

Direct refill request (skipping inbox triage).

**Body:**
```json
{ "patient_id": "pt-002", "medication": "metformin 500mg" }
```

**Response:** 202 Accepted. Refill manager auto-approves on-plan meds, otherwise creates a nurse task.

### `POST /priorauth`

Prior authorization request.

**Body:**
```json
{ "patient_id": "pt-003", "code": "70551", "payer": "BCBS" }
```

`code` is a CPT/HCPCS code. The agent auto-approves low-risk procedures.

**Response:** 202 Accepted.

---

## Events

### `POST /adt`

Native ADT event ingest. For HL7 v2 wire format, use `POST /hl7/adt`.

**Body:** [`medical.ADTEvent`](data-model.md#adtevent)

```json
{
  "event_id": "evt-001",
  "patient_id": "pt-001",
  "type": "discharge",
  "facility": "Mercy ED",
  "occurred_at": "2026-05-24T14:30:00Z",
  "primary_dx": "ADHF"
}
```

`type` ∈ `admit`, `discharge`, `transfer`, `update`.

**Response:** 202 Accepted. Readmission tracker publishes `readmission_risk` and (for discharges) `discharge_event` to trigger TCM (gated by `discharge_review` if HITL is on).

### `POST /rpm/observation`

Single RPM reading.

**Body:** [`medical.RPMObservation`](data-model.md#rpmobservation)

```json
{
  "patient_id": "pt-001",
  "device": "scale",
  "metric": "weight_lb",
  "value": 192.4,
  "unit": "lb",
  "taken_at": "2026-05-24T06:00:00Z"
}
```

**Response:** 200 OK with any alerts inline.

---

## FHIR R4

The shipped adapter ([pkg/fhir](../pkg/fhir/)) covers the four resources most commonly needed for ingest: Patient, Encounter, Condition, Observation. Bulk Data export is planned.

### `POST /fhir/Patient`

**Body:** FHIR R4 Patient resource. Mapped to `medical.PatientRecord` and upserted into the registry.

### `POST /fhir/Encounter`

**Body:** FHIR R4 Encounter resource. Mapped to `medical.ADTEvent`.

### `POST /fhir/Condition`

**Body:** FHIR R4 Condition resource. Appends condition code to the patient.

---

## HL7 v2

### `POST /hl7/adt`

Native HL7 v2.x ADT message ingest (PR #8).

**Content-Type:** `application/hl7-v2` or `text/plain`.

**Body:** Raw pipe-delimited HL7 v2 message. The parser supports MSH (with custom delimiters per MSH-1/MSH-2), EVN, PID, PV1, DG1. Trigger events: ADT^A01, A02, A03, A04, A08.

**Example body:**
```
MSH|^~\&|ADTSYS|MERCY|BODHYANGA|MERCY|20260524143000||ADT^A01|MSG00001|P|2.5
EVN|A01|20260524143000
PID|||MR12345^^^MERCY^MR||REDACTED^REDACTED||19540101|F|||...
PV1||I|MERCY^EDOBS^01|||||||MED||||||...
DG1|1|I10|I50.23^Acute on chronic systolic heart failure^I10|||W
```

**Response (success):** 200 OK with the AA ACK in the body:

```
MSH|^~\&|BODHYANGA|MERCY|ADTSYS|MERCY|20260524143005||ACK^A01^ACK|ACK-MSG00001|P|2.5
MSA|AA|MSG00001|
```

**Response (parse / mapping failure):** 400 Bad Request with the AE ACK:

```
MSH|^~\&|BODHYANGA|MERCY|ADTSYS|MERCY|<now>||ACK^?^ACK|ACK-MSG-ERR|P|2.5
MSA|AE|<original_control_id>|<diagnostic_text>
```

The parsed event publishes as `adt_event` on the bus and reaches `readmission_tracker`. See [pkg/hl7v2](../pkg/hl7v2/) for the parser internals.

---

## HITL reviews

### `GET /reviews`

List reviews.

**Query:** `?status=pending` (or `approved`, `rejected`, `expired`) to filter.

**Response:** 200 OK, JSON array of `medical.ReviewRequest`.

### `GET /reviews/{id}`

Single review.

**Response:** 200 OK with the `ReviewRequest`, or 404 if not found.

### `POST /reviews/{id}/decide`

Make a decision on a pending review.

**Body:**
```json
{
  "reviewer_id": "rn-rachel",
  "decision": "approve",
  "reason": "Vitals consistent with admit criteria",
  "modification": null
}
```

`decision` ∈ `approve`, `approve_with_modifications`, `reject`.

**Response:** 200 OK. On approve, the original payload is republished to the intended recipient (the `hitl_next_to` from the envelope). On reject, the review is logged as a `KindReviewDecision` audit event and dropped — **no resume**.

---

## Audit

### `GET /audit` (PR #12)

Read the audit log.

**Query:**
- `?kind=message` — filter by audit kind (`message`, `policy_decision`, `review_decision`, `agent_error`)
- `?limit=100` — default 100, max 1000

**Response:** 200 OK, JSON array of `audit.Event`:

```json
[
  {
    "id": "01H...",
    "at": "2026-05-24T07:02:11Z",
    "kind": "policy_decision",
    "agent_id": "orchestrator",
    "message_id": "6ab0f882-...",
    "from": "user",
    "to": "intake",
    "type": "presentation",
    "tenant_id": "t-1",
    "case_id": "case-smoke",
    "decision": "allow",
    "reason": "case_id present"
  },
  {
    "id": "01H...",
    "at": "2026-05-24T07:02:11Z",
    "kind": "message",
    "agent_id": "intake",
    "message_id": "6ab0f882-...",
    "from": "user",
    "to": "intake",
    "type": "presentation",
    "tenant_id": "t-1",
    "case_id": "case-smoke"
  }
]
```

**Field selection is deliberate** — see [governance.md § Audit events](governance.md#audit-events) for the rationale. No narrative, no diagnosis text, no PHI.

---

## Reads

### `GET /tasks`

Nurse work queue (TCM follow-ups, refills, prior auths, inbox).

**Response:** 200 OK, JSON array of `tasks.NurseTask`, sorted by `DueAt`.

### `GET /notifications`

Outreach log + adherence stats.

**Response:** 200 OK, JSON object:
```json
{
  "notifications": [...],
  "adherence_rate": 0.87
}
```

### `GET /healthz`

Liveness probe.

**Response:** 200 OK, `{"status":"ok"}`.

### `GET /readyz`

Readiness probe.

**Response:** 200 OK when bus subscribers are wired and any required backends are reachable. 503 otherwise.

---

## Message types

Complete catalog of `Message.Type` values used across diagnostic + care + HITL flows.

### Diagnostic flow

| Type | Direction | Content | Required metadata |
|---|---|---|---|
| `presentation` | user → `intake` | `PatientPresentation` JSON | `case_id` |
| `case_state` | various | `CaseState` JSON | `case_id` |
| `questions_complete` | `questioner` → supervisor | `CaseState` JSON | `case_id` |
| `tests_complete` | `cost_guardian` → supervisor | `CaseState` JSON | `case_id` |
| `tests_revise` | `cost_guardian` → supervisor | `CaseState` JSON + downgrade hint | `case_id` |
| `diagnosis_proposal` | `diagnostician` → verifier | `DiagnosisProposal` JSON | `case_id` |
| `diagnosis` | verifier → supervisor | `CaseState` JSON (final) | `case_id` |
| `order_test` | (future per-test messages) | test JSON | `case_id`, `test_cost_usd`, `spent_usd` |
| `ask_question` | (future single-Q turn) | question string | `case_id` |
| `verify_reasoning` | (reserved) | rubric payload | `case_id` |

### Care services

| Type | Direction | Content |
|---|---|---|
| `cdm_request` | user → `cdm_planner` | `{patient_id, condition, case_id}` |
| `care_plan_ready` | `cdm_planner` → engagement | `CarePlan` JSON |
| `discharge_event` | user/ADT → `tcm_coordinator` | `DischargeSummary` JSON |
| `tcm_tasks_created` | `tcm_coordinator` → engagement | `{patient_id, case_id, task_count}` |
| `panel_request` | user → `panel_manager` | `{condition?}` |
| `panel_report` | `panel_manager` → (broadcast) | `PanelReport` JSON |
| `engagement_request` | `panel_manager` → engagement | `CareGap` JSON |
| `pre_visit_request` | user → `pre_visit_prep` | `{patient_id, encounter_id}` |
| `pre_visit_ready` | `pre_visit_prep` → (broadcast) | summary JSON |
| `inbox_message` | user → `inbox_triage` | `{patient_id, subject, body}` |
| `inbox_routed` | `inbox_triage` → (broadcast) | `{category}` |
| `refill_request` | user/inbox → `refill_manager` | `{patient_id, medication}` |
| `refill_response` | `refill_manager` → engagement | `{status, reason}` |
| `prior_auth_request` | user/inbox → `prior_auth` | `{patient_id, code, payer}` |
| `prior_auth_status` | `prior_auth` → (broadcast) | `{status}` |
| `adt_event` | user/HL7 → `readmission_tracker` | `ADTEvent` JSON |
| `readmission_risk` | `readmission_tracker` → (broadcast) | `{score, band}` |
| `engagement_sent` | `engagement` → (broadcast) | `{id, channel, template}` |

### HITL

| Type | Direction | Content |
|---|---|---|
| `review_request` | gated agent → `human_review` | original payload + envelope metadata |

---

## Required metadata keys

| Key | Required when | Purpose |
|---|---|---|
| `case_id` | all clinical types | correlation; never log MRNs |
| `patient_id` | care-services types | population correlation |
| `tenant_id` | multi-tenant deployments | required by `RequireTenantPolicy` |
| `trace_id` | recommended always | distributed tracing |
| `max_cost_usd` | intake / case budget | SD-Bench cost ceiling |
| `gold_diagnosis` | benchmarks only | offline eval, never present in production |
| `spent_usd`, `test_cost_usd` | test ordering | per-message budget check |
| `scopes` | SMART-on-FHIR | space-separated OAuth scopes |
| `image_path` | vision diagnostician | local path to attach to LLM message |
| `hitl_*` | HITL envelope | see [agents.md § HITL](agents.md#hitl) |

---

## Error envelopes

JSON error responses use this shape:

```json
{
  "error": "policy_denied",
  "reason": "case_id required for clinical messages",
  "message_id": "6ab0f882-..."
}
```

| Status | Error code | When |
|---|---|---|
| 400 | `bad_request` | malformed JSON, missing required body field |
| 400 | `policy_denied` | governance policy denied the message |
| 400 | `parse_error` | HL7 v2 / FHIR parse failure (HL7 returns AE ACK + 400) |
| 404 | `not_found` | unknown patient_id / case_id / review id |
| 409 | `conflict` | review already decided |
| 422 | `unprocessable` | semantic validation failed (e.g. unknown `condition`) |
| 500 | `internal_error` | unexpected; check logs / audit |
| 503 | `not_ready` | `/readyz` returned 503 — backend unavailable |

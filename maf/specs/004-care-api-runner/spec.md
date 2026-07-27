# Feature Specification: Care API on the Runner

**Feature Branch**: `feat/care-api-runner`

**Created**: 2026-07-16

**Status**: Draft

**Input**: User description: "An HTTP service that drives the MAF-go pipeline Runner — submit a
case and get a run id; poll status and result; list pending physician reviews and post a decision
that resumes a paused run; read the PHI-free audit trail for a case. The bodh-maf analog of bodh's
`cmd/care`, API only (no React SPA). It wraps the EXISTING `pkg/pipeline.Runner`
(`Run`/`Resume`/`Responder`), preserving the admission boundary and PHI-free-audit invariants at
the HTTP edge. Success = each user story proven by an httptest handler test; durable review answered
via the API after a simulated process restart, exactly as `specs/001` proved at the CLI."

## Clarifications

### Session 2026-07-16

- Q: What is the case-status source of truth, given `Runner.Run` returns an in-memory `Outcome` and
  `pkg/ckptpg` only stores opaque PHI-bearing checkpoint payloads? → A: A **new, PHI-free case
  registry** owned by the API (`pkg/careapi`) is the source of truth for {caseToken → sessionID,
  status, terminal summary}. It is a documented prerequisite (P0), not assumed to exist on the
  Runner. Checkpoints remain the *durable-resume* substrate; the registry is the *queryable-status*
  substrate. The two are kept consistent by the API writing the registry on every terminal/paused
  transition.
- Q: Auth posture for the slice? → A: **No auth middleware** — documented non-goal / roadmap,
  matching bodh's `cmd/care` default (ADR-0006 "research-grade substrate"). The service binds
  loopback by default and states the gap loudly.
- Q: Does the Runner support answering a *specific* pending review across a process restart? → A:
  Yes, by composition: `Runner.Resume(sessionID)` republishes the same pending request and the
  API's `Responder` returns the posted decision. `specs/001` proved kill→resume→approve at the CLI
  (`pkg/ckptpg.TestKillResume`); this feature exposes the identical mechanism over HTTP. No new
  Runner engine is introduced.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Submit a case and receive a run handle (Priority: P1)

As a clinical-integration operator, I POST a patient case to the service and immediately receive a
stable run handle (a session id and the case's PHI-free fingerprint), so that I can track the case
without holding a connection open for the whole pipeline and without ever putting a raw identifier
on the wire back to me.

**Why this priority**: Nothing else is reachable until a case can enter the pipeline over HTTP and
be named by a handle the later endpoints key on. This is the API's front door and the minimum
demoable slice.

**Independent Test**: POST a valid case to `/v1/cases`; assert `201` with a body carrying a
non-empty `session_id` and a `case_token` equal to `Runner.CaseToken(pc)`; assert the run actually
executed the pipeline (a subsequent status read reflects a terminal or paused state, not "unknown").

**Acceptance Scenarios**:

1. **Given** the service is up with an allow policy and the rule engine, **When** a valid case is
   POSTed, **Then** the response is `201` with `session_id` + `case_token`, and the case reaches a
   terminal or paused state.
2. **Given** a case whose diagnosis clears the confidence bar, **When** it is POSTed, **Then** the
   run pauses at physician review and the response reports `status: "awaiting_review"` (not an error).
3. **Given** a malformed request body (not a `PatientCase`), **When** it is POSTed, **Then** the
   response is `400` and nothing enters the pipeline.
4. **Given** a deny-all policy, **When** a case is POSTed, **Then** the response reports
   `status: "denied"` with no care plan, and the denial is on the audit trail (governance boundary
   is enforced at the HTTP edge exactly as at the CLI).

---

### User Story 2 - Poll a case's status and result (Priority: P2)

As an operator holding a run handle, I GET the case's current state and, when finished, its outcome
(care plan, rejection, denial, or failure) — so a caller can drive the pipeline asynchronously by
polling rather than blocking.

**Why this priority**: A submit that returns a handle is only useful if the handle can be resolved.
This closes the async loop opened by US1 and is the read side every other story reports through.

**Independent Test**: submit a case, then GET its status; assert the state matches the known
outcome (`completed` with a care plan for a low-confidence forced case under review-disabled config,
or `awaiting_review` for a paused one); GET a never-submitted handle and assert `404`.

**Acceptance Scenarios**:

1. **Given** a completed case, **When** its status is read, **Then** the response is `200` with
   `status: "completed"` and the care plan (diagnosis + steps), keyed by `case_token` only.
2. **Given** a case awaiting review, **When** its status is read, **Then** the response is `200`
   with `status: "awaiting_review"` and the review summary (diagnosis, confidence, forced flag) —
   never the raw case payload.
3. **Given** a rejected case, **When** its status is read, **Then** `status: "rejected"` with the
   reviewer id and no care plan.
4. **Given** a handle that was never submitted, **When** its status is read, **Then** the response
   is `404`.
5. **Given** a failed case (e.g. malformed LLM output), **When** its status is read, **Then**
   `status: "failed"` and the failure is reported without a fabricated diagnosis.

---

### User Story 3 - List pending reviews and post a decision that resumes the run (Priority: P3)

As a reviewing physician (or their client), I GET the list of cases awaiting my review, and I POST
my approval or rejection for one of them; posting the decision resumes the paused run and drives it
to its terminal state — and this works even if the service process restarted while the case sat
waiting.

**Why this priority**: This is the human-oversight surface and the feature's hardest guarantee: it
exercises `Runner.Resume` over HTTP and must preserve `specs/001`'s durable-review proof. It depends
on US1 (a case to pause) and US2 (a way to see the result after the decision).

**Independent Test**: submit a case that pauses at review; GET `/v1/reviews` and assert it lists the
case with its token, diagnosis, and confidence; **discard the in-memory Runner/registry and rebuild
them from the Postgres checkpoint store** (simulating a restart); POST an approval to
`/v1/reviews/{session_id}`; assert `200`, a care plan on the subsequent status read, and an audit
trail spanning both lifetimes — the HTTP analog of `pkg/ckptpg.TestKillResume`.

**Acceptance Scenarios**:

1. **Given** one case paused at review, **When** `/v1/reviews` is read, **Then** it returns that
   case's `session_id`, `case_token`, diagnosis, confidence, and forced flag — and no PHI.
2. **Given** a listed pending review, **When** an approval with a reviewer id is POSTed to it,
   **Then** the run resumes, produces a care plan, the review disappears from the pending list, and
   the approval is on the audit trail.
3. **Given** a listed pending review, **When** a rejection is POSTed, **Then** the case terminates
   with no care plan and the rejection is recorded.
4. **Given** a case that paused, **When** the service process restarts (durable store only) and the
   physician then POSTs a decision, **Then** the decision still lands and the case completes as if
   no restart occurred.
5. **Given** a decision POSTed with an empty reviewer id, **When** it is submitted, **Then** the
   response is `400` and no verdict is applied (anonymous approvals never land).

---

### User Story 4 - Read a case's PHI-free audit trail (Priority: P4)

As a compliance auditor, I GET the audit trail for one case by its fingerprint and receive the
ordered, PHI-free record of everything that happened to it — admissions, executions, the review
request and decision, and finalization — so the case lifecycle is reconstructible over HTTP, and I
can confirm no raw identifier or clinical content leaked to the API surface.

**Why this priority**: The trail is the regulatory backbone but reads the same store US1–US3 already
populate; it ships last because it depends on their events existing. It is a thin, high-value read.

**Independent Test**: run one case to completion via the API; GET `/v1/cases/{case_token}/audit`;
assert the returned events reconstruct the lifecycle (admission before execution per hop, review
request + decision, finalization) and that an automated scan of every returned field finds zero raw
case ids and zero clinical strings.

**Acceptance Scenarios**:

1. **Given** a completed case, **When** its audit trail is read, **Then** the response lists that
   case's events in a coherent order and every event is keyed by the hashed token only.
2. **Given** any returned audit event, **When** its fields are inspected, **Then** identifiers
   appear only as fingerprints and no clinical narrative is present.
3. **Given** a case token with no events, **When** its trail is read, **Then** the response is `404`
   (unknown case) — distinct from `200` with an empty list.
4. **Given** a denied case, **When** its trail is read, **Then** the denial record is present
   (denials are never dropped from the HTTP-visible trail).

---

### Edge Cases

- **Submit while another case is running**: each submit gets its own session id and runs
  independently; the service must serve concurrent submits without cross-contaminating handles,
  registry rows, or audit trails (trails are filtered by case token).
- **Answer a review that doesn't exist** (wrong/unknown session id, or already-answered): the API
  returns `404` for an unknown session and `409` for one no longer awaiting review — the stale
  verdict is never silently applied (mirrors FR-009 / `pkg/pipeline/review.go` stale-response path).
- **Poll a finished or failed case**: status is served from the registry (terminal outcomes are
  recorded there), so polling after completion returns the terminal state, never "unknown"; polling
  a failed case returns `failed` without a fabricated result.
- **Restart mid-review, then answer via the API**: with the durable store configured, a rebuilt
  service reconstructs the pending-review list from checkpoints and answers via `Runner.Resume`; the
  decision lands and the trail spans both lifetimes. Without the durable store, a restart loses
  in-flight paused cases — this MUST be reported at submit/list time, not silently.
- **Post a decision for a case that already terminated between list and post** (race): the resume
  finds no pending request; the API returns `409` and records nothing new.
- **Audit sink error mid-case**: care flow continues (fail-open, inherited from `pkg/admit`); the
  audit read reflects whatever was durably recorded, and the API never blocks a care action on an
  audit write.
- **Concurrent decisions on the same session**: only the first resume that delivers a response wins;
  the second observes no pending request and returns `409`.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The service MUST accept a `PatientCase` over `POST /v1/cases` and drive it through the
  existing `pkg/pipeline.Runner` (`Run`) — it MUST NOT introduce a second pipeline engine.
- **FR-002**: A submit MUST return a stable handle: the run's `session_id` and the case's
  `case_token` (`Runner.CaseToken`), and MUST NOT return any raw patient identifier or clinical
  content in the response body.
- **FR-003**: The service MUST expose a case's current state and terminal result via
  `GET /v1/cases/{case_token}` (or `{session_id}`), served from a **PHI-free case registry** that
  the API updates on every paused/terminal transition; an unknown handle MUST return `404`.
- **FR-004**: The registry MUST distinguish at least: `running`, `awaiting_review`, `completed`,
  `rejected`, `denied`, `failed` — and MUST never store raw case payloads (only hashed tokens,
  diagnosis text under review, and terminal summaries).
- **FR-005**: The service MUST list cases awaiting physician review via `GET /v1/reviews`, returning
  for each: `session_id`, `case_token`, diagnosis, confidence, forced flag — and no PHI.
- **FR-006**: The service MUST accept a physician decision via `POST /v1/reviews/{session_id}` and
  apply it by setting `Runner.Responder` to return that `ReviewDecision` and calling
  `Runner.Resume(session_id)`; the run MUST proceed to its terminal state and the review MUST leave
  the pending list.
- **FR-007**: A decision with an empty `reviewer_id` MUST be rejected with `400` and never applied
  (anonymous approvals never land — preserves FR-009 of specs/001).
- **FR-008**: A decision for an unknown session MUST return `404`; a decision for a session no
  longer awaiting review MUST return `409`; neither applies a verdict.
- **FR-009**: With a durable checkpoint store configured, a decision POSTed **after a service
  process restart** MUST still land and complete the case — the HTTP analog of specs/001's
  kill/resume proof. Without a durable store, the service MUST surface that paused cases will not
  survive restart (documented degraded mode), not fail silently.
- **FR-010**: The service MUST expose a case's PHI-free audit trail via
  `GET /v1/cases/{case_token}/audit`, filtered to that case token; an unknown token MUST return
  `404`; a denied case's denial record MUST be present.
- **FR-011**: Every admission and audit invariant enforced at the CLI MUST hold identically at the
  HTTP edge: fail-closed governance denies reach the caller as a governed `denied` status (not a
  500), and no HTTP handler MAY copy case content into an audit event or a response.
- **FR-012**: Handler tests MUST run without live LLM services and without Docker where possible:
  US1/US2/US4 use the rule engine + in-memory audit + in-memory registry; only the US3
  restart-durability test requires Postgres (testcontainers), reusing `pkg/ckptpg`.
- **FR-013**: Authentication/authorization is a documented **non-goal** for this slice (no auth
  middleware wired), mirroring bodh's `cmd/care` research-grade default (ADR-0006); the service
  binds loopback by default and states the gap in its startup banner and README.
- **FR-014**: The service MUST support concurrent case submissions without cross-contaminating
  handles, registry state, or per-case audit trails.

### Key Entities

- **Case handle**: the `{session_id, case_token}` pair returned at submit; the key every later
  endpoint resolves. `case_token` is `sha256(caseID)[:8]`, never the raw id.
- **Case registry entry**: one PHI-free row per submitted case — `case_token`, `session_id`,
  `status`, and a terminal summary (care-plan steps, rejection reviewer, or failure note). The
  API's status source of truth; **a new component this feature introduces** (the Runner returns an
  in-memory `Outcome` only).
- **Pending review**: a case parked at the review gate — surfaced as `{session_id, case_token,
  diagnosis, confidence, forced}`; at most one active per session (mirrors specs/001).
- **Review decision (HTTP)**: the physician's `{approved, reviewer_id, note}` POST body, mapped 1:1
  to `agents.ReviewDecision` and delivered through `Runner.Responder` + `Runner.Resume`.
- **Audit view**: the case-filtered projection of `audit.Recorder.List()` — PHI-free by
  construction, ordered for lifecycle reconstruction.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A valid case POSTed to the running service returns a handle and reaches a terminal or
  paused state 100% of the time under the allow policy; a deny-all policy yields a `denied` status
  and a denial audit record, never a 500.
- **SC-002**: For any submitted case, its status is resolvable by handle at all times after submit
  (never "unknown"), and terminal outcomes remain readable after the run goroutine has returned.
- **SC-003**: 100% of cases paused at review are answerable via `POST /v1/reviews/{session_id}`
  after a simulated process restart (registry + pending list rebuilt from the Postgres checkpoint
  store), with the decision landing and the case completing — the HTTP analog of SC-003 in specs/001.
- **SC-004**: An automated scan of every field of every submit/status/reviews/audit response finds
  zero raw case ids and zero clinical narrative across the whole endpoint surface.
- **SC-005**: Every user story (US1–US4) is proven by at least one `net/http/httptest` handler test;
  US1/US2/US4 run with no Docker and no live LLM; US3's durability test uses testcontainers Postgres.
- **SC-006**: Concurrent submission of N distinct cases produces N independent handles, registry
  rows, and case-filtered audit trails with no cross-contamination.

## Assumptions

- The existing `pkg/pipeline.Runner` (`Run`, `Resume`, `Responder`, `CaseToken`, `WithCheckpointStore`)
  and its `Outcome` type are the sole pipeline engine; this feature wraps them and adds no orchestration.
- `Runner.Resume(sessionID)` republishing the same pending review request across a process restart is
  proven (`pkg/ckptpg.TestKillResume`, README exit criterion 3); the API relies on that behavior
  rather than re-proving MAF's resume semantics.
- Postgres (via `pkg/ckptpg`) is available for the durability story; automated tests provision it
  ephemerally (testcontainers), consistent with specs/001.
- The audit source for the HTTP read is bodh's `audit.Recorder` (in-memory in the slice; the
  production sink chain is bodh's concern, unchanged here).
- The service is single-process; distribution (A2A, multiple API replicas sharing one registry) is
  out of scope — the registry is process-local, rebuildable from checkpoints for the restart case.
- No clinical UI/SPA ships with this feature; the API is the entire deliverable (matches bodh-maf's
  non-goals in README).

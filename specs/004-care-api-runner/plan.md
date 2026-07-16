# Implementation Plan: Care API on the Runner

**Branch**: `feat/care-api-runner` | **Date**: 2026-07-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/004-care-api-runner/spec.md`

## Summary

Put a thin `net/http` layer in front of the **existing** `pkg/pipeline.Runner` so a case can be
submitted, polled, reviewed, and audited over HTTP — the bodh-maf analog of bodh's `cmd/care`, API
only. The handlers wrap `Runner.Run` (submit), a new PHI-free **case registry** (status/result),
`Runner.Resume` + `Runner.Responder` (answer a pending review, durable across restart), and a
case-filtered projection of `audit.Recorder.List()` (audit read). No new pipeline engine, no new
orchestration: the safety boundary (`pkg/admit`) and the PHI-free-audit discipline are inherited
unchanged and re-asserted at the HTTP edge by tests. The one genuinely new component is the
registry, which exists because the Runner returns an in-memory `Outcome` and has no queryable
status accessor (see Complexity Tracking + the flagged prerequisite tasks).

## Technical Context

**Language/Version**: Go 1.26 (module `github.com/PratikDhanave/bodh-maf`)

**Primary Dependencies**: stdlib `net/http` + `net/http/httptest` (no web framework);
`github.com/PratikDhanave/bodh-maf/pkg/pipeline` (Runner — the engine, unchanged);
`bodh/pkg/audit` (`Recorder`, `Event`) and `bodh/pkg/eval` via the existing `replace ../bodh`;
`pkg/ckptpg` (Postgres checkpoint store, reused for the restart-durability test);
`jackc/pgx/v5` (already a dep); `testcontainers-go` (test-only, US3 durability test only)

**Storage**: a **process-local, PHI-free case registry** (in-memory `map[caseToken]entry` behind a
mutex for the slice; interface-shaped so a durable backing is a later swap). Durable *resume* state
stays in `pkg/ckptpg`'s `checkpoints` table — unchanged. Audit uses bodh's `Recorder` (in-memory in
the slice). **No new SQL migration** is required for the slice registry.

**Testing**: stdlib `go test` + `net/http/httptest` for every handler; the rule engine + in-memory
audit + in-memory registry cover US1/US2/US4 with **no Docker and no live LLM** (constitution II);
only the US3 restart test needs testcontainers Postgres, reusing `pkg/ckptpg` and the specs/001
kill/resume pattern (build a Runner, pause a case, drop it, rebuild registry+Runner from the store,
answer over HTTP).

**Target Platform**: single-process binary `cmd/care` (darwin/linux); loopback bind by default

**Project Type**: Go library package (`pkg/careapi`) + one thin CLI (`cmd/care`), mirroring the
`pkg/pipeline` + `cmd/slice` split so the HTTP handlers are testable without booting a server.

**Performance Goals**: submit adds one `Runner.Run` (same cost as the CLI, <5s rule engine, no
network on the allow path) + one registry write; status/reviews/audit reads are O(events) in-memory
map/slice scans — sub-millisecond for slice-scale data.

**Constraints**: fail-closed governance / fail-open audit inherited verbatim from `pkg/admit` — a
governance denial is a `denied` *status*, never an HTTP 500; **zero PHI in any response body,
registry entry, or log line** (case ids appear only as `agents.HashCaseID` tokens; the raw
`PatientCase` lives only inside the pipeline payload/checkpoint jsonb); handlers never copy case
content into audit events.

**Scale/Scope**: 1 new package (`pkg/careapi`), 1 new CLI (`cmd/care`), 4 endpoints, 1 registry
type, ~4 prerequisite Runner-observability accessors, ~14 handler/registry tests.

## Constitution Check

*GATE: evaluated against `.specify/memory/constitution.md` (bodh constitution, inherited).*

| Principle | Status | Notes |
|---|---|---|
| I. MARA pipeline | PASS | No pipeline change; the HTTP layer only *drives* the existing 4-stage graph via `Runner` |
| II. LLM-agnostic provider abstraction | PASS | Diagnostician selection is unchanged (`cmd/slice`'s `-diagnostician` logic reused); handler tests use the rule engine and never call real APIs |
| III. HIPAA (non-negotiable) | PASS | PHI-free registry + case-filtered audit read; responses keyed by hashed token only; HITL gate preserved (review answered over HTTP still requires a named reviewer); admission boundary re-asserted at the edge |
| IV. Test-first, coverage gates | PASS | httptest handler tests per user story (tests-first); testcontainers only for the restart test; ≥90% coverage target on `pkg/careapi` |
| V. Package-first | PASS | Logic in `pkg/careapi` (registry + handlers as a `http.Handler`); `cmd/care` is wiring only |
| VI. Observability by default | DEFERRED | Structured request logging only; OTel wiring stays out of scope as in specs/001 — recorded gap, not a violation |
| (Auth) | DOCUMENTED NON-GOAL | No auth middleware, loopback bind, banner + README state the gap — matches bodh `cmd/care` / ADR-0006 research-grade default |

**Post-design re-check (Phase 1)**: no new violations; the registry is the one added component and
is justified below.

## Technical Approach

### Endpoint surface (naming borrowed from bodh's `cmd/care` / `pkg/web` where sensible)

| Method + path | Maps to | Runner call |
|---|---|---|
| `POST /v1/cases` | submit a `PatientCase`, get a handle (US1) | `Runner.Run(ctx, pc, "")` → registry write |
| `GET /v1/cases/{case_token}` | status + terminal result (US2) | registry read (no Runner call) |
| `GET /v1/reviews` | list pending physician reviews (US3) | registry read of `awaiting_review` rows |
| `POST /v1/reviews/{session_id}` | answer a review, resume the run (US3) | set `Runner.Responder`, `Runner.Resume(sessionID)` → registry update |
| `GET /v1/cases/{case_token}/audit` | PHI-free trail for the case (US4) | filter `Recorder.List()` by `CaseID == token` |
| `GET /healthz`, `GET /readyz` | liveness/readiness | mirrors `cmd/care` contract referenced in bodh `pkg/runtime/doc.go` |

### How review-answer maps to Runner resume (the load-bearing mechanism)

`pkg/pipeline/runner.go` gives exactly the two handles this needs:
- `Runner.Responder func(*workflow.ExternalRequest) (*workflow.ExternalResponse, error)` (runner.go:35)
  — the per-run answer hook. `cmd/slice/main.go:104` already shows the pattern: build a
  `ReviewDecision`, set `Responder` to `req.CreateResponse(dec)`.
- `Runner.Resume(ctx, sessionID) (*Outcome, error)` (runner.go:104) — replays from the latest
  checkpoint; MAF republishes the *same* pending request, which the `Responder` answers. Requires
  `Runner.WithCheckpointStore` (runner.go:65) to have been called.

The `POST /v1/reviews/{session_id}` handler therefore: (1) validates a non-empty `reviewer_id`
(else `400`, FR-007); (2) confirms the registry marks the session `awaiting_review` (else `404`
unknown / `409` not-pending, FR-008); (3) sets `Responder` to return the posted decision;
(4) calls `Resume`; (5) writes the terminal `Outcome` into the registry and removes the pending
row. Because `Resume` reads the checkpoint store, this works in a **freshly restarted process** —
the same guarantee `pkg/ckptpg.TestKillResume` proves at the CLI (README exit criterion 3).

### Keeping the invariants at the HTTP edge

- **Admission (fail-closed)**: the boundary lives in `pkg/admit` and is wired into the graph via
  `pipeline.Config.Boundary`; the HTTP layer changes nothing. A denied case returns from `Run` with
  `Outcome.Denied == true` (runner.go:167/174); the submit handler maps that to a `denied` *status*
  body (200/202-class with an explicit status field), **never** an HTTP 500 (FR-011).
- **Audit (fail-open, PHI-free)**: the handler reads `Recorder.List()` and filters by hashed token.
  Every `audit.Event` field is already PHI-free by construction (`bodh/pkg/audit/recorder.go` — only
  correlation handles, `CaseID` is the hashed token the bridge writes). The handler adds no fields
  and copies no case content. A per-story PHI scan (reuse `pkg/auditbridge` invariant helpers if
  exported) asserts SC-004 across the whole response surface.
- **PHI at submit**: the request body carries a raw `PatientCase`, which is correct — it enters the
  pipeline where `pkg/phi` de-identifies at intake. The *response* and registry carry only
  `Runner.CaseToken(pc)`.

### Auth posture

Explicitly out of scope, matching bodh's `cmd/care` default (ADR-0006 "research-grade substrate":
no auth middleware wired). `cmd/care` binds `127.0.0.1` by default, prints a startup banner naming
the gap, and the README states it. A `-addr` flag allows non-loopback for demos with the banner
still warning. This is a documented roadmap item, not a silent omission (FR-013).

### Test strategy (no live LLM / no Docker where possible)

- **Handlers are tested as an `http.Handler`** via `httptest.NewRecorder` + `http.NewRequest` — no
  socket, no `cmd/care` boot. `pkg/careapi.New(runner, registry, recorder)` returns the handler.
- US1/US2/US4: rule engine (`pipeline.Config` with `Diagnostician: nil`), `audit.NewInMemory`,
  in-memory registry — deterministic, fast, offline.
- US3 non-durable path (list + answer within one process): file/in-memory checkpoint store or the
  in-process pause, still no Docker.
- US3 durable/restart path: **one** testcontainers-Postgres test that mirrors
  `pkg/ckptpg.TestKillResume` but drives the answer through the HTTP handler after rebuilding the
  registry from `store.RetrieveIndex` (the pending-review reconstruction — see prerequisite T-R2).
- No handler test calls a real LLM (constitution II); live-LLM is manual behind env flags as in
  specs/001.

### MAF-go / Runner integration points (real, cited)

- `pipeline.NewRunner(cfg, rec) (*Runner, error)` — runner.go:53 (build the engine)
- `(*Runner).WithCheckpointStore(store) *Runner` — runner.go:65 (durable + Resume)
- `(*Runner).Run(ctx, pc, sessionID) (*Outcome, error)` — runner.go:88 (submit)
- `(*Runner).Resume(ctx, sessionID) (*Outcome, error)` — runner.go:104 (answer-after-restart)
- `(*Runner).Responder` — runner.go:35 (the decision hook; usage at cmd/slice/main.go:104)
- `(*Runner).CaseToken(pc) string` — runner.go:73 (the response/registry key)
- `Outcome{Plan, Rejected, Denied, Paused, SessionID, Eval, Failures}` — runner.go:39 (status source)
- `agents.ReviewDecision{Approved, ReviewerID, Note}` — pkg/agents/review.go:16 (POST body target)
- `agents.ReviewRequest{CaseToken, Diagnosis, Confidence, Forced, Rounds}` — pkg/agents/review.go:6
  (the pending-review projection surfaced by `GET /v1/reviews`)
- `audit.Recorder.List() []audit.Event` — bodh/pkg/audit/recorder.go:185 (audit read source)

### Runner capability GAPS — flagged as prerequisite tasks, NOT assumed

The Runner today is **synchronous and stateless about past runs**: `Run`/`Resume` return an
in-memory `*Outcome` and nothing indexes cases or enumerates paused sessions. The API needs three
observability affordances the Runner does not provide. These are called out as prerequisite tasks
(T-R1..T-R3) in tasks.md and are the *only* net-new logic beyond HTTP glue:

1. **No case→status registry** — the API owns a new `pkg/careapi` registry (PHI-free). *This lives
   in the new package, so it is a build task, not a Runner change.*
2. **No list-pending-reviews accessor** — when `Outcome.Paused` is true the API records the pending
   review (`session_id`, `case_token`, and the `ReviewRequest` fields) into the registry at submit
   time. To recover the pending list **after a restart**, the API must rebuild it from
   `store.RetrieveIndex(ctx, sessionID, nil)` per session; there is **no** cross-session
   "list all sessions with pending requests" call on the store or Runner today — the API must track
   the set of session ids it has started (a `sessions` registry column / index), or accept that only
   sessions it was told about are recoverable. **Flagged as T-R2 (prerequisite + design note).**
3. **`Run` returns Paused without surfacing the pending `ReviewRequest` contents to the caller** —
   the parked `ReviewRequest` is emitted on the event stream as a `RequestInfoEvent` and consumed
   inside `drain` (runner.go:139); the returned `Outcome` exposes `Paused` and `Events` but not a
   convenience "the pending review is X". The API extracts the `ReviewRequest` from
   `Outcome.Events` (scan for `workflow.RequestInfoEvent`), OR a tiny Runner accessor
   `Outcome.PendingReview() *agents.ReviewRequest` is added. **Flagged as T-R3 (choose: scan events
   in careapi vs. add the accessor to pkg/pipeline).**

None of these require changing MAF-go or the pipeline graph; T-R1 is pure new code in `pkg/careapi`,
T-R2/T-R3 are small, isolated, and independently testable.

## Project Structure

### Documentation (this feature)

```text
specs/004-care-api-runner/
├── spec.md
├── plan.md              # this file
└── tasks.md             # Phase 2
```

### Source Code (repository root)

```text
cmd/care/                thin CLI: flags (-addr, -policy, -diagnostician, -db), server boot,
                         startup banner (auth-gap warning), reuses cmd/slice provider wiring
pkg/careapi/             the HTTP layer + registry (the testable seam)
  handler.go             http.ServeMux: routes → handlers; New(runner, registry, recorder)
  registry.go            PHI-free case registry (map + mutex behind an interface)
  submit.go              POST /v1/cases
  status.go              GET /v1/cases/{case_token}
  reviews.go             GET /v1/reviews, POST /v1/reviews/{session_id}
  audit.go               GET /v1/cases/{case_token}/audit
  probes.go              /healthz, /readyz
```

Reuse (no edits): `pkg/pipeline` (Runner), `pkg/admit`, `pkg/agents`, `pkg/auditbridge`,
`pkg/ckptpg`, `pkg/evalhook`, `cmd/slice/providers.go` logic (copied or extracted — see T007).

**Structure Decision**: `pkg/careapi` + thin `cmd/care`, mirroring `pkg/pipeline` + `cmd/slice`
(constitution V), so handlers are `httptest`-able without a live server and each endpoint maps 1:1
to a user story for independent landing.

## Risks

- **Registry ↔ checkpoint divergence** (top risk): the PHI-free registry and the durable checkpoint
  store are two sources of truth that must not drift. Mitigation: the registry is *derived* — on
  restart it is rebuilt from the checkpoint store's session index (T-R2), and it is only ever
  written on a paused/terminal transition the API itself observed. If a session exists in
  checkpoints but not the registry after restart, the API treats the checkpoint as authoritative and
  re-materializes the registry row.
- **Cross-session pending-review recovery has no Runner/store accessor** (feasibility risk): the
  store indexes checkpoints *by session id* (`RetrieveIndex`, ckptpg/store.go:79) but offers no
  "which sessions have a pending request" query. The API must persist the set of session ids it
  starts to recover them post-restart; for the slice this set can live in the same
  (rebuildable-from-checkpoints-per-session) registry, but a truly stateless restart would need a
  new store method — noted as a scope boundary, not solved here.
- **Concurrent resume races** on one session: two POSTs racing to answer the same review — only the
  first `Resume` delivering a response wins; the second finds no pending request and must map to
  `409`. Mitigation: per-session lock in the registry around Resume + status transition.
- **Preview churn / synchronous Run blocking a request goroutine**: `Run` is synchronous, so a
  submit holds the request goroutine for the pipeline duration (fine for the rule engine <5s; a slow
  LLM could exceed sensible HTTP timeouts). Mitigation for the slice: document it; a future async
  submit (return 202 + background Run) is a non-goal here.

## Non-goals (1-week scope boundary)

- Authentication/authorization, TLS, rate limiting, multi-tenancy (documented roadmap, FR-013).
- A React/SPA or any UI — API only (matches bodh-maf README non-goals).
- Async/background submit (202 + poll-only) — submit is synchronous for the slice; only *review*
  answering is inherently async (via pause/resume).
- Multiple API replicas sharing one registry / a durable registry backing — the registry is
  process-local and rebuildable from checkpoints; distribution is out of scope.
- FHIR/HL7 ingestion, RAG, the full eval harness, the remaining agents — unchanged bodh-maf non-goals.
- Streaming/SSE status — polling only.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| New `pkg/careapi` registry (state not on the Runner) | `Runner.Run` returns an in-memory `*Outcome` and has no case-status accessor; async submit + later poll is impossible without one | "Just re-run/replay to get status" rejected — re-running re-executes the pipeline (side effects, duplicate audit + eval records); reading checkpoints directly rejected — payloads are opaque + PHI-bearing (ckptpg/store.go doc), not a queryable status |
| Prerequisite accessors T-R2/T-R3 touch `pkg/pipeline` | pending-review recovery + surfacing the parked `ReviewRequest` need either an event scan or a tiny accessor | Doing it entirely in `pkg/careapi` by scanning `Outcome.Events` is the default; the accessor is offered only if the scan proves awkward — kept minimal and isolated |

# Tasks: Care API on the Runner

**Input**: Design documents from `specs/004-care-api-runner/`

**Prerequisites**: plan.md, spec.md

**Tests**: INCLUDED — the spec mandates them (SC-005: every user story proven by an httptest
handler test) and they are tests-first (red before the handler that satisfies them).

**Organization**: grouped by user story (P1→P4); each story lands independently testable and green
before the next starts. Runner-observability prerequisites (T-R*) are called out explicitly because
the current Runner does not provide them (plan.md § Runner capability GAPS).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US4 per spec.md; **[PRE]** = prerequisite affordance the Runner lacks today

## Phase 1: Setup

- [ ] T001 Scaffold `pkg/careapi/` and `cmd/care/` package skeletons + a `doc.go` in
  `/Users/genesis/Developer/goworkspace/src/github.com/PratikDhanave/bodh-maf/pkg/careapi/doc.go`
  stating the wrap-the-Runner contract, the PHI-free-response invariant, and the auth non-goal; `go build ./...` green
- [ ] T002 [P] Confirm no new deps needed (`net/http`, `net/http/httptest` stdlib; `pkg/pipeline`,
  `bodh/pkg/audit`, `pkg/ckptpg` already present); `go mod tidy` no-op in
  `/Users/genesis/Developer/goworkspace/src/github.com/PratikDhanave/bodh-maf/go.mod`

## Phase 2: Foundational (blocking prerequisites)

**These exist because the Runner is synchronous and stateless about past runs (plan.md § GAPS).**

- [ ] T-R1 [P] [PRE] Case registry in `pkg/careapi/registry.go`: PHI-free `Registry` interface
  (`Put(entry)`, `Get(caseToken) (Entry, bool)`, `BySession(sessionID) (Entry, bool)`,
  `Pending() []Entry`) + in-memory impl behind a mutex; `Entry{CaseToken, SessionID, Status,
  Diagnosis, Confidence, Forced, ReviewerID, PlanSteps, Note}` — statuses `running |
  awaiting_review | completed | rejected | denied | failed` (FR-004). Unit tests in
  `pkg/careapi/registry_test.go`: round-trip, `Pending()` returns only `awaiting_review`, concurrent
  Put/Get race-free (`go test -race`), and a **PHI-scan** asserting no field can hold a raw case id
- [ ] T-R2 [P] [PRE] Pending-review recovery helper in `pkg/careapi/recover.go`: given a
  `pkg/ckptpg.Store` and a set of known session ids, rebuild `awaiting_review` registry entries by
  calling `store.RetrieveIndex(ctx, sessionID, nil)` (ckptpg/store.go:79) to confirm a session has
  checkpoints, then re-materialize its pending-review row. **Design note in code**: the store has no
  cross-session "list sessions with pending requests" call, so the API persists the set of session
  ids it started; document this scope boundary (plan.md risk 2). Unit test with a fake store
- [ ] T-R3 [PRE] Pending-`ReviewRequest` extraction in `pkg/careapi/pending.go`:
  `pendingReview(out *pipeline.Outcome) *agents.ReviewRequest` scanning `Outcome.Events` for
  `workflow.RequestInfoEvent` and decoding its `ReviewRequest` (mirrors cmd/slice/main.go:105's
  `PortableValueAs[agents.ReviewRequest]`). Table test with synthetic `Outcome.Events`. *(If the
  scan proves awkward, the alternative is a one-line `Outcome.PendingReview()` accessor in
  `pkg/pipeline/runner.go` — decide here; default is the careapi-local scan, no pipeline edit.)*
- [ ] T004 [P] Test helper in `pkg/careapi/testutil_test.go`: `newTestAPI(t, cfg)` building a rule-
  engine `pipeline.Runner` (allow policy, in-memory audit, in-memory registry) wired into a
  `pkg/careapi` `http.Handler`, plus a `doRequest(method, path, body)` shim over `httptest` — shared
  by US1–US4 handler tests, no Docker, no LLM

**Checkpoint**: registry + recovery + pending-extraction + test harness exist — user story phases can begin.

## Phase 3: User Story 1 — Submit a case and receive a run handle (P1) 🎯 MVP

**Goal**: `POST /v1/cases` drives `Runner.Run` and returns a PHI-free `{session_id, case_token}` handle.

**Independent test**: POST a valid case ⇒ 201 with handle; deny-all ⇒ `denied` status not 500;
malformed body ⇒ 400; confident case ⇒ `awaiting_review` (SC-001).

- [ ] T005 [P] [US1] Handler tests (write first, red) in `pkg/careapi/submit_test.go`: valid case ⇒
  201 + non-empty `session_id` + `case_token == Runner.CaseToken(pc)` (runner.go:73); malformed JSON
  ⇒ 400 and pipeline never invoked; deny-all policy ⇒ body `status:"denied"` (not HTTP 500,
  FR-011); confident rule case with `Review:true` ⇒ `status:"awaiting_review"`; response body PHI-
  scan finds no raw case id / symptom strings (SC-004)
- [ ] T006 [US1] Submit handler in `pkg/careapi/submit.go`: decode `agents.PatientCase`, call
  `Runner.Run(ctx, pc, "")` (runner.go:88), map `Outcome` → registry `Put` (Denied→`denied`,
  Paused→`awaiting_review` + pending row via T-R3, Plan→`completed`, Rejected→`rejected`,
  Failures→`failed`), respond with `{session_id, case_token, status}` only — never case content (FR-002)
- [ ] T007 [US1] Router + `New(runner, registry, recorder) http.Handler` in `pkg/careapi/handler.go`
  (`http.ServeMux`, path params via Go 1.22+ patterns); provider/policy wiring in `cmd/care/main.go`
  reusing `cmd/slice/providers.go`'s `buildDiagnostician` + `parsePolicy` (extract to a shared
  internal helper or copy — no edit to cmd/slice), flags `-addr -policy -diagnostician -db`
- [ ] T008 [US1] Startup banner + loopback default in `cmd/care/main.go`: bind `127.0.0.1:8088` by
  default, print an auth-gap warning banner (FR-013, ADR-0006), `/healthz` + `/readyz` in
  `pkg/careapi/probes.go` matching the `cmd/care` probe contract (bodh `pkg/runtime/doc.go`)

**Checkpoint**: US1 functional — a case can be submitted over HTTP and returns a handle; MVP demoable.

## Phase 4: User Story 2 — Poll a case's status and result (P2)

**Goal**: `GET /v1/cases/{case_token}` resolves any handle to its state + terminal result from the registry.

**Independent test**: submit → status matches known outcome; unknown handle ⇒ 404; failed case ⇒
`failed` without a fabricated diagnosis (SC-002).

- [ ] T009 [P] [US2] Handler tests (red) in `pkg/careapi/status_test.go`: completed case ⇒ 200 +
  `status:"completed"` + care-plan steps + diagnosis; awaiting_review case ⇒ 200 +
  `status:"awaiting_review"` + review summary (no raw payload); rejected ⇒ reviewer id + no plan;
  unknown token ⇒ 404; failed case ⇒ `status:"failed"` no fabricated result; response PHI-scan (SC-004)
- [ ] T010 [US2] Status handler in `pkg/careapi/status.go`: `registry.Get(caseToken)` →
  serialize the PHI-free `Entry`; 404 on miss (FR-003). No Runner call — status is served from the
  registry the submit/resume paths write (plan.md § approach)

**Checkpoint**: the async submit→poll loop closes; every handle is resolvable (SC-002).

## Phase 5: User Story 3 — List pending reviews + answer to resume the run (P3)

**Goal**: `GET /v1/reviews` lists parked cases; `POST /v1/reviews/{session_id}` answers via
`Runner.Responder` + `Runner.Resume`, durable across a process restart.

**Independent test**: pause → list shows it → rebuild registry+Runner from Postgres (restart) →
POST approval → care plan + trail spanning both lifetimes; empty reviewer ⇒ 400; unknown ⇒ 404;
not-pending ⇒ 409 (SC-003).

- [ ] T011 [P] [US3] Handler tests (red, no Docker) in `pkg/careapi/reviews_test.go`: one paused
  case ⇒ `GET /v1/reviews` returns its `session_id`, `case_token`, diagnosis, confidence, forced
  (no PHI); POST approval w/ reviewer ⇒ 200, subsequent status `completed`, review leaves the list;
  POST rejection ⇒ `rejected` no plan; empty `reviewer_id` ⇒ 400 nothing applied (FR-007); unknown
  session ⇒ 404; session not `awaiting_review` ⇒ 409 (FR-008)
- [ ] T012 [US3] List handler in `pkg/careapi/reviews.go`: `GET /v1/reviews` → `registry.Pending()`
  projected to `{session_id, case_token, diagnosis, confidence, forced}` (from
  `agents.ReviewRequest`, review.go:6) — PHI-free (FR-005)
- [ ] T013 [US3] Answer handler in `pkg/careapi/reviews.go`: `POST /v1/reviews/{session_id}` →
  validate reviewer id (400), registry state (`404`/`409`), set
  `Runner.Responder` to return `agents.ReviewDecision{Approved, ReviewerID, Note}` (review.go:16;
  pattern at cmd/slice/main.go:104), call `Runner.Resume(ctx, sessionID)` (runner.go:104), write the
  terminal `Outcome` to the registry, drop the pending row; per-session lock so concurrent answers
  race to `409` (FR-006, FR-008, plan.md risk 3)
- [ ] T014 [US3] **Restart-durability test** in `pkg/careapi/restart_test.go` (testcontainers
  Postgres, reuses `pkg/ckptpg`): build Runner `WithCheckpointStore(ckptpg.New(pool))`
  (runner.go:65), submit a confident case over HTTP ⇒ `awaiting_review`; **discard the API + Runner
  + in-memory registry**; rebuild a fresh Runner + registry, recover the pending list from the store
  via T-R2; `GET /v1/reviews` shows the case; POST approval ⇒ 200 + care plan; assert the audit
  trail spans both lifetimes — the HTTP analog of `pkg/ckptpg.TestKillResume` (SC-003, FR-009)
- [ ] T015 [US3] `cmd/care/main.go`: `-db` wires `ckptpg.New(pool)` +
  `runner.WithCheckpointStore(store)` + `store.EnsureSchema` (as cmd/slice/main.go:86); without
  `-db`, print a degraded-mode warning that paused cases will not survive restart (FR-009)

**Checkpoint**: durable human oversight proven over HTTP, end-to-end, across a restart.

## Phase 6: User Story 4 — Read a case's PHI-free audit trail (P4)

**Goal**: `GET /v1/cases/{case_token}/audit` returns the case-filtered, ordered, PHI-free trail.

**Independent test**: complete a case → GET its audit → lifecycle reconstructible; PHI scan finds
nothing; unknown token ⇒ 404; denied case's denial present (SC-004).

- [ ] T016 [P] [US4] Handler tests (red) in `pkg/careapi/audit_test.go`: completed case ⇒ 200 +
  ordered events for that token (admission before execution per hop, review request+decision,
  finalization); every returned field PHI-scanned (raw case id / clinical strings) ⇒ zero hits
  (SC-004); unknown token ⇒ 404 (distinct from empty 200, FR-010); deny-all case ⇒ denial record present
- [ ] T017 [US4] Audit handler in `pkg/careapi/audit.go`: `recorder.List()`
  (bodh/pkg/audit/recorder.go:185) filtered to `Event.CaseID == caseToken`; 404 if the token is
  unknown to the registry AND has no events; reuse `pkg/auditbridge` invariant/PHI-scan helpers if
  exported, else a local field scan (FR-010, FR-011)

**Checkpoint**: full case lifecycle reconstructible over HTTP and provably PHI-free.

## Phase 7: Polish & Cross-Cutting

- [ ] T018 [P] Concurrency test in `pkg/careapi/concurrent_test.go`: submit N distinct cases in
  parallel ⇒ N independent handles, registry rows, and case-filtered audit trails with no cross-
  contamination (SC-006, FR-014); `go test -race ./pkg/careapi/...` clean
- [ ] T019 [P] Coverage ≥90% on `pkg/careapi` (`go test -cover`); `go vet ./...` + staticcheck clean
- [ ] T020 Update `README.md` (add the Care API to scope / usage: `go run ./cmd/care`, curl
  examples), `architecture.md` (an §"HTTP surface" note + endpoint table), and `CLAUDE.md` layout +
  commands if the care binary changes the quick-reference
- [ ] T021 Manual quickstart: `go run ./cmd/care`, curl submit → status → reviews → answer → audit
  against a local Postgres for the durable path; fix any doc/behavior drift

## Dependencies

- Setup (T001–T002) → Foundational (T-R1, T-R2, T-R3, T004) → US1 (T005–T008)
- US2 (T009–T010) requires US1's submit + registry writes
- US3 (T011–T015) requires US1 (a case to pause) + the registry; T014 also requires `pkg/ckptpg`
  (already built) + T-R2 recovery
- US4 (T016–T017) requires US1–US3 events to exist in the recorder; independent of US3's durability path
- Polish (T018–T021) last

Story order: **US1 → US2 → US3 → US4** — US2 and US4 are thin reads that can be pulled forward once
their producing story (US1 for US2; US1–US3 for US4) is green.

## Parallel example (within Foundational)

```
T-R1 (registry) ∥ T-R2 (recovery helper) ∥ T004 (test harness)
then T-R3 (pending extraction) → US1
```

## Implementation strategy

MVP = Phase 1–3 (US1): a case can be submitted over HTTP and returns a handle, with the admission
boundary enforced at the edge — already demoable (`curl -X POST /v1/cases`, `-policy deny` returns a
governed `denied`). Then land one story per commit/PR in priority order; each checkpoint leaves the
suite green and the API usable. The US3 restart test is the feature's proof obligation — it is the
HTTP analog of specs/001's kill/resume and the reason the registry-recovery prerequisite (T-R2)
exists.

# Tasks: Slice Safety Boundary

**Input**: Design documents from `specs/001-slice-safety-boundary/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: INCLUDED — the spec mandates them (FR-014: every exit criterion proven by a
test) and tests are derived from the contracts' machine-checkable invariants.

**Organization**: grouped by user story (P1→P5); each story lands independently
testable and green before the next starts.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US5 per spec.md

## Phase 1: Setup

- [ ] T001 Add feature dependencies to go.mod: `require github.com/PratikDhanave/bodh` (resolved via existing replace), `jackc/pgx/v5`, `testcontainers-go` (test-only); `go mod tidy` green in /Users/genesis/Developer/goworkspace/src/github.com/PratikDhanave/bodh-maf/go.mod

## Phase 2: Foundational (blocking prerequisites)

- [ ] T002 [P] Extract the workflow-run test harness from pkg/pipeline/pipeline_test.go into pkg/pipeline/testutil_test.go + internal/slicetest/run.go (collects events, output, errors) so US1–US5 tests share one runner
- [ ] T003 [P] Demo policy factory in pkg/admit/policies.go: `AllowAll()`, `DenyAll()`, `DenyAgent(id)`, `Erroring()` as `governance.Policy` implementations (PolicyResult per bodh contract), with unit tests in pkg/admit/policies_test.go

**Checkpoint**: harness + policies exist — user story phases can begin.

## Phase 3: User Story 1 — Every agent action is admission-controlled (P1) 🎯 MVP

**Goal**: one admission mechanism on every hop; fail-closed; denials audited synchronously.

**Independent test**: deny-all policy ⇒ zero agent work, one denial record per attempted
hop, run halts; allow ⇒ same case completes (SC-001).

- [ ] T004 [P] [US1] Contract tests (write first, red) in pkg/admit/admit_test.go: allow→handler runs after allow-record; deny→handler NOT run, deny-record written before ErrDenied returns; policy error ⇒ deny; audit-write failure on allow path does not block handler (fail-open); unlabeled message type ⇒ deny
- [ ] T005 [P] [US1] Envelope mapping in pkg/admit/envelope.go: message-type label registry (patient_case, diagnostic_query, workup, diagnosis_result, care_plan), `buildEnvelope(from, to string, payload any) (protocol.Message, error)` per data-model.md; Content always empty; unit tests in pkg/admit/envelope_test.go
- [ ] T006 [US1] Boundary core in pkg/admit/admit.go: `New(governance.Policy, audit.Recorder, *slog.Logger)`, `Wrap[In,Out]`, `ErrDenied`, synchronous deny writes, agent_error recording on inner failure — per contracts/admission-and-review.md
- [ ] T007 [US1] Agent-middleware variant in pkg/admit/middleware.go: `(*Boundary).Middleware(agentID string) agent.Middleware` short-circuiting the provider run with identical evaluate/record semantics; test in pkg/admit/middleware_test.go with a fake RunFunc
- [ ] T008 [US1] Wire admission into graph assembly in pkg/pipeline/pipeline.go: `Config.Boundary *admit.Boundary`; every executor handler wrapped via admit.Wrap; nil Boundary = no-op (step-1 tests keep passing)
- [ ] T009 [US1] Integration tests in pkg/pipeline/admission_test.go: deny-all (zero execution, per-hop deny records, no CarePlan), allow (completes), deny-agent=care_plan (upstream hops run, halt at care_plan) — SC-001
- [ ] T010 [US1] CLI wiring in cmd/slice/main.go: `-policy allow|deny|deny-agent=<id>` flag, exit code 2 on ErrDenied, print deny reason

**Checkpoint**: US1 fully functional — the MVP safety boundary exists.

## Phase 4: User Story 2 — Complete, PHI-free audit trail (P2)

**Goal**: full case lifecycle reconstructible from the trail; zero PHI anywhere in it.

**Independent test**: canonical case trail matches contracts/audit-trail.md ordered
table; automated PHI scan finds nothing (SC-002).

- [ ] T011 [P] [US2] Trail-invariant checker in pkg/auditbridge/invariants.go (+ _test.go): completeness (allow before message per hop), deny-completeness, PHI-freedom scan (raw case ID / payload strings), review anchoring, duplicate-tolerant subsequence match — the 6 invariants from contracts/audit-trail.md, exported for reuse by US3 tests
- [ ] T012 [US2] Event bridge in pkg/auditbridge/bridge.go: consume `run.WatchStream`, map ExecutorCompleted→message, ExecutorFailed→agent_error, SuperStepCompleted(w/ checkpoint)→message/checkpoint, OutputEvent→message/case_finalized per data-model.md kind table; async, dedup-tolerant; unit tests with synthetic events in pkg/auditbridge/bridge_test.go
- [ ] T013 [US2] Run loop owning bridge + boundary in pkg/pipeline/runner.go: `Runner{Workflow, Boundary, Recorder}` starting the stream consumer and returning collected output — cmd/slice and all integration tests use it
- [ ] T014 [US2] Trail contract test in pkg/pipeline/trail_test.go: canonical happy-path case (allow, 1 loop-back) asserts the ordered trail table + all invariants; fail-open test with an erroring Recorder proves care continues and error is logged — SC-002
- [ ] T015 [US2] cmd/slice/main.go: run via pipeline.Runner; `-trail` flag prints the case's audit events (PHI-free by construction)

**Checkpoint**: trail is complete and provably PHI-free.

## Phase 5: User Story 3 — Physician review survives restart (P3)

**Goal**: review gate pauses the case durably; kill/restart/resume lands the decision.

**Independent test**: pause → kill → resume from Postgres → approve → correct terminal
state, trail spans both lifetimes (SC-003).

- [ ] T016 [P] [US3] Review types + gate in pkg/pipeline/review.go: ReviewRequest/ReviewDecision (data-model.md), RequestPort "physician_review" bound between diagnostician and care_plan; reject ⇒ terminal without care plan; stale/duplicate RequestID or empty ReviewerID ⇒ discarded + audited review_decision/reject/stale_response; unit tests in pkg/pipeline/review_test.go
- [ ] T017 [P] [US3] Migration db/0001_checkpoints.sql: checkpoints table + session index + append-only grants per data-model.md
- [ ] T018 [US3] Postgres store in pkg/ckptpg/store.go implementing `checkpoint.Store[json.RawMessage]` (CreateCheckpoint/RetrieveCheckpoint/RetrieveIndex per contracts/checkpoint-postgres.md); never log `data`
- [ ] T019 [US3] Store tests in pkg/ckptpg/store_test.go (testcontainers): byte round-trip, lineage/RetrieveIndex ordering + parent filter, missing-row error, concurrent sessions
- [ ] T020 [US3] Runner checkpointing + resume in pkg/pipeline/runner.go: WithCheckpointing(NewJSONManager(store)); `Resume(sessionID)` republishing the pending ReviewRequest with the SAME ID; audit events for review_requested / checkpoint / review_decision
- [ ] T021 [US3] Kill/resume integration test in pkg/ckptpg/killresume_test.go per contracts/checkpoint-postgres.md §test: pause at RequestInfoEvent, close run, new runner resumes, same request ID, approve ⇒ care plan + case_finalized + trail invariant 6 across lifetimes — SC-003
- [ ] T022 [US3] CLI in cmd/slice/main.go: `-db`, `-resume <session>`, `-approve/-reject`, `-reviewer <id>`; exit code 3 when paused; prints session + request IDs

**Checkpoint**: durable human oversight proven end-to-end.

## Phase 6: User Story 4 — Diagnostic engine is a configuration choice (P4)

**Goal**: rule | Anthropic | Ollama behind a flag; identical typed output; zero graph edits.

**Independent test**: same case on rule and fake-LLM engines ⇒ structurally identical
outputs and trail shapes; malformed LLM output ⇒ agent_error, no fabricated result (SC-004).

- [ ] T023 [P] [US4] Fake provider in pkg/agents/fakeprovider_test.go: `agent.ProviderConfig{Run: scripted RunFunc}` emitting valid and malformed structured outputs — no real API calls ever (constitution II / FR-012)
- [ ] T024 [US4] LLM diagnostician in pkg/agents/llm_diagnostician.go: agentworkflow binding consuming Workup, WithStructuredOutput(&DiagnosisResult{}), post-validation (Confidence∈[0,1], non-empty Diagnosis, CaseID overwritten from input), loop-back/forced logic identical to rule path, admission middleware attached from Config.Boundary
- [ ] T025 [US4] Provider constructors in cmd/slice/providers.go: `-diagnostician rule|llm-anthropic|llm-ollama`; anthropicprovider.NewAgent(ANTHROPIC_API_KEY, BODH_ANTHROPIC_MODEL) and openaiprovider.NewChatCompletionsAgent(option.WithBaseURL($OLLAMA_HOST/v1), BODH_OLLAMA_MODEL); graph unchanged — only pipeline.Config.Diagnostician binding differs
- [ ] T026 [US4] Engine-swap tests in pkg/pipeline/engine_swap_test.go: same case via rule vs fake-LLM ⇒ same output struct shape, same trail shape (invariant checker), review gate applies to both; malformed-output case ⇒ agent_error recorded, run fails without DiagnosisResult — SC-004, FR-011

**Checkpoint**: provider-pluggability preserved under the new boundary.

## Phase 7: User Story 5 — Every finished case yields an evaluation record (P5)

**Goal**: exactly one InteractionRecord per terminal case; no perpetual "pending".

**Independent test**: approved / rejected / forced cases each produce one correct
record (SC-005).

- [ ] T027 [P] [US5] Eval hook in pkg/evalhook/hook.go: observe terminal outcome from Runner (OutputEvent + review decision), write one eval.InteractionRecord per research.md R7 mapping; unit tests in pkg/evalhook/hook_test.go covering approved (Success=true), rejected, forced (Success=false + forced metric)
- [ ] T028 [US5] Wire into pkg/pipeline/runner.go (`Config.EvalStore eval.Store`) and cmd/slice (print eval summary after terminal state); integration assertion added to kill/resume test: exactly one record after resume-approve
- [ ] T029 [US5] Exit-criteria test in pkg/pipeline/exitcriteria_test.go: one test per README exit criterion 1–5 (thin wrappers asserting the five SCs hold in one run each) — FR-014, SC-006

**Checkpoint**: eval-mirror gap closed; all five exit criteria automated.

## Phase 8: Polish & Cross-Cutting

- [ ] T030 [P] Run quickstart.md end-to-end manually (allow/deny, kill/resume via compose Postgres, engine swap vs Ollama if available); fix drift between docs and behavior
- [ ] T031 [P] Coverage ≥90% on pkg/admit, pkg/auditbridge, pkg/ckptpg, pkg/evalhook (`go test -cover`); `go vet ./...` + staticcheck clean
- [ ] T032 Update README.md exit-criteria table (proven ✅ per test), architecture.md delivery table (steps 2–5 done), and CLAUDE.md if commands changed
- [ ] T033 Write the go/no-go verdict back to ../bodh/session.md (delivery-plan step 5): what MAF carried cleanly, what needed shims, recommendation for the wider platform and specs/002

## Dependencies

- Setup (T001) → Foundational (T002–T003) → US1 (T004–T010)
- US2 (T011–T015) requires US1's Boundary; US3 (T016–T022) requires US2's Runner+bridge
- US4 (T023–T026) requires US1 (middleware) + US2 (trail-shape assertions); independent of US3
- US5 (T027–T029) requires US2 (Runner) + US3 (review decision in record); T029 requires all stories
- Polish (T030–T033) last

Story order: **US1 → US2 → (US3 ∥ US4) → US5** — US3 and US4 can proceed in parallel
after US2.

## Parallel example (within US3)

```
T016 (review gate, pkg/pipeline) ∥ T017 (SQL migration) ∥ T023 (US4 fake provider)
then T018 → T019 → T020 → T021 → T022
```

## Implementation strategy

MVP = Phase 1–3 (US1): the admission boundary alone already answers the migration's
core question and is demoable (`-policy deny`). Then land one story per commit/PR in
priority order; each checkpoint leaves the suite green and the CLI usable.

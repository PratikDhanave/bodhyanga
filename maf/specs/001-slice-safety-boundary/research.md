# Phase 0 Research: Slice Safety Boundary

All unknowns were resolved by first-hand source reading: MAF-go at the pinned commit
(reviewed 2026-07-11, three deep-read reports in `bodh/session.md`) and bodh's
governance/audit/eval packages (read 2026-07-12). No open NEEDS CLARIFICATION items.

## R1 — Governance evaluation contract

**Decision**: Admission calls `bodh/pkg/governance.Policy.Evaluate(ctx, protocol.Message)
→ (PolicyResult, error)`. Any result other than `DecisionAllow`, and any non-nil error,
is a denial (fail-closed).

**Rationale**: This is byte-for-byte the monolith's contract
(`bodh/pkg/governance/policy.go`): `PolicyResult.Decision/Reason` flow verbatim into
`audit.Event.Decision/Reason`, so reusing it preserves the on-disk audit schema and lets
bodh's existing policy stack (`CompositePolicy`, kill switches, OPA) drop in unchanged.

**Alternatives considered**: A new MAF-native policy interface — rejected: it would
fork the compliance authority and invalidate audit parity (SC-002).

## R2 — The hop envelope (what governance sees)

**Decision**: `pkg/admit` synthesizes a PHI-free `protocol.Message` per hop:
`From` = sending executor ID (or "operator" for the initial input), `To` = target
executor ID, `Type` = slice message-type label (e.g. `diagnostic_query`, `workup`,
`diagnosis_result`), `Metadata["case_id"]` = the case's opaque token, `Content` = "".

**Rationale**: bodh policies route on the envelope triple + metadata, not payload;
leaving `Content` empty makes PHI leakage into governance/audit structurally impossible
rather than procedurally avoided (constitution III).

**Alternatives considered**: Marshaling the typed payload into `Content` for policy
richness — rejected: violates the never-copy-PHI rule and no shipped policy needs it.

## R3 — Admission placement in MAF

**Decision**: A generic handler decorator `admit.Wrap[In, Out]` applied to every
executor at graph assembly, PLUS an equivalent `agent.Middleware` chained onto the LLM
diagnostician's agent so the provider call is guarded even inside the agent loop.

**Rationale**: MAF's `agent.Middleware` short-circuits agent runs (verified:
`agent/middleware.go` — middleware may yield an error and never call `next`), but
pure-Go executors never traverse an agent. The decorator keeps ONE admission
implementation for all hops (FR-001); the middleware variant reuses the same core.

**Alternatives considered**: (a) Edge conditions as gates — rejected: a false edge
condition silently drops the message (`DeliveryStatusDroppedConditionFalse`), which is
fail-silent, not fail-closed-with-audit. (b) Guarding only agents — rejected: leaves
80% of hops unguarded.

## R4 — Deny durability vs audit fail-open

**Decision**: The decorator writes the `policy_decision` event synchronously
(same goroutine, before returning `admit.ErrDenied`). Audit write failures never block
the flow in either direction: on the allow path care continues; on the deny path the
action is already blocked — the failed write is loudly logged either way.

**Rationale**: Matches `audit.Recorder` doc ("log-and-continue", fail-open seam) while
honoring the spec's "a lagging observer can never miss a denial" (FR-003): synchronous
ordering, not transactional coupling.

**Alternatives considered**: Failing the run when a deny-record write fails — rejected:
inverts bodh's documented posture and adds no safety (the action was already denied).

## R5 — Durable pause/resume mechanics

**Decision**: `RequestPort` bound between diagnostician and care_plan; runs execute
under `inproc.Default.WithCheckpointing(checkpoint.NewJSONManager(ckptpg.Store))`;
`cmd/slice -resume <session>` restarts via the manager's resume path, which republishes
pending external requests. Stale/duplicate review responses are detected by request ID
and rejected with a `review_decision` audit event carrying reason `stale_response`.

**Rationale**: All verified against `workflow/requestport.go`, `workflow/checkpoint/`
(public `Store[json.RawMessage]` = 3 methods: CreateCheckpoint / RetrieveCheckpoint /
RetrieveIndex) and the checkpoint-and-rehydrate example. Postgres schema:
`checkpoints(id uuid pk, session_id text, parent_id uuid null, data jsonb, created_at)`
— append-only per bodh persistence discipline; `data` is PHI-bearing → never logged,
same access tier as clinical tables.

**Alternatives considered**: JSON file store (ships with MAF) — kept for unit tests,
rejected for the exit criterion: SC-003 must prove a *real* durable backend.

## R6 — LLM diagnostician binding

**Decision**: `-diagnostician=rule|llm-anthropic|llm-ollama`. LLM variants are MAF
agents (`anthropicprovider.NewAgent`; `openaiprovider.NewChatCompletionsAgent` with
`option.WithBaseURL($OLLAMA_HOST/v1)`), run with `WithStructuredOutput(&DiagnosisResult{})`,
hosted in the graph via `workflow/agentworkflow` so the node's protocol (consumes
Workup, sends DiagnosisResult/DiagnosticQuery) is identical to the rule executor's.
Tests inject a fake provider through `agent.ProviderConfig{Run: fakeRunFunc}`.

**Rationale**: Verified provider surfaces (`provider/anthropicprovider/agent.go`,
`provider/openaiprovider/chat.go`, custom-provider contract `agent/agent.go`).
Structured output makes "identical typed output" (FR-010) enforceable at the
type level. A `pkg/llm.Client` adapter was considered and rejected for the slice —
recorded in plan.md Complexity Tracking.

**Malformed-output handling (FR-011)**: unmarshal failure or missing fields → the hop
returns an error → `agent_error` audit event; no DiagnosisResult is fabricated.

## R7 — Eval record mapping

**Decision**: `pkg/evalhook` observes the run's terminal `OutputEvent` and writes ONE
`eval.InteractionRecord{Scenario: "mara-slice/" + caseToken, Success: !Forced && approved,
Metrics: {confidence, rounds, forced(0|1)}, Metadata: {review_decision, diagnosis_kind}}`
via `bodh/pkg/eval.Store.Save`.

**Rationale**: Field-for-field consistent with the monolith's `recordCaseEval` intent
(closing bodh `pkg/runtime/doc.go`'s gap via its option (b): a per-deployment terminal
subscriber). `InteractionRecord`/`Store` shapes verified in `bodh/pkg/eval/eval.go`.

## R8 — Framework pin & upgrade policy

**Decision**: Stay pinned to `v0.0.0-20260710122455-75d0a647f739` for the whole
feature; upgrades are dedicated PRs run against the full suite.

**Rationale**: MAF-go is public preview; the slice's value is evidence, which requires
a stable baseline.

# Session log — 2026-07-11

## Context

Working session on `feat/agent-decoupling-phase0`. Two threads:

1. `/init` — refresh CLAUDE.md against the current tree (**done**)
2. **Evaluate reimplementing bodh on `microsoft/agent-framework-go` (MAF-go)** — in progress

---

## 1. CLAUDE.md updates (committed to working tree, not yet git-committed)

- Added `pkg/runtime` as the 4th agent-decoupling substrate package (per-agent message
  loop, landed in `f99bade`), including its **eval-mirror fidelity gap**: decoupling
  `diagnostic_supervisor` before closing it breaks `GET /explain/{case_id}` case-status
  reporting (documented in `pkg/runtime/doc.go` with two closure options).
- Repository layout: `cmd/` now notes ~36 binaries incl. lesson binaries (lesson-01…13);
  `pkg/` subset now lists `comm`, `gateway`, `governance`, `graduation`, `runtime`.
- Noted relicense Apache-2.0 → PolyForm Noncommercial 1.0.0.

## 2. MAF-go evaluation — findings so far

Repo cloned at scratchpad `agent-framework-go/` (HEAD `75d0a64`, 2026-07-10).
Module `github.com/microsoft/agent-framework-go`, Go 1.25. **Public preview**
(.NET/Python hit GA 1.0 April 2026; Go trails).

### Key facts (verified first-hand, not from README)

- **Anthropic provider EXISTS** (`provider/anthropicprovider`, `anthropic-sdk-go` dep) —
  README undersold it. Also Gemini, OpenAI (incl. Azure), Foundry, Copilot, A2A, AGUI.
- **No Ollama provider**; custom providers are built via `agent.ProviderConfig` (RunFunc
  contract) — an adapter over bodh's `pkg/llm.Client` looks feasible.
- **Middleware**: `agent.Middleware` / `MiddlewareFunc` run-pipeline interception;
  harness middlewares for tool auto-call + tool approval (standing rules, auto-approval
  heuristics). Candidate host for bodh's `gateway.Pipeline.Admit` (fail-closed governance,
  fail-open audit).
- **Workflows**: graph builder (`AddEdge`, fan-out, fan-in barrier, `AddSwitch`,
  conditional edges), sequential/concurrent/**group-chat** builders (→ virtual physician
  panel), subworkflows, workflow-as-agent. **Handoff orchestration not implemented.**
- **HITL**: `RequestPort` / `ExternalRequest`/`ExternalResponse` — workflow pauses for
  external input, pending-request republish on resume (→ bodh `pkg/hitl`).
- **Checkpointing**: in-memory + JSON/file stores, custom `checkpoint.Store[json.RawMessage]`
  interface (→ a Postgres store is possible). **In-process only — no durable/distributed
  execution in Go** (durable is .NET-only). Relevant to specs/002 decoupling ambitions.
- **Gaps vs .NET** (from `docs/dotnet-go-sdk-feature-comparison.md`, 2026-07-02):
  **no evaluation package** (bodh has heavy evals — pkg/eval, pkg/evals, cmd/bench),
  **no RAG** (bodh pkg/knowledge), no declarative agents/workflows, no DevUI,
  no durable execution, no Cosmos/Postgres stores built in.

### Strategic observation

MAF middleware + checkpointing + HITL + group chat covers much of what
`feat/agent-decoupling-phase0` is hand-building (`gateway.Pipeline.Admit` ≈ middleware;
durable bus ≈ checkpointing; hitl ≈ request ports). Adopting MAF could supersede parts
of specs/002 — but MAF-go is in-process only, so it does NOT give per-agent processes;
A2A hosting is its inter-process story.

Bodh's compliance layer (audit chain-hash, PHI de-id, governance policies, graduation
ladder) has NO MAF equivalent and ports over regardless of strategy.

### Open scope questions (asked, user deferred — wants full repo review first)

1. Strategy: new sibling repo w/ vertical slice (MARA 4-agent pipeline) vs in-place
   adoption vs full greenfield.
2. Fate of in-flight decoupling work (specs/002, gateway/runtime/graduation).
3. Provider strategy: pkg/llm adapter vs native MAF providers vs OpenAI-compat only.

## 3. Deep-read results (3 parallel agents, all complete)

### Provider layer (verified APIs)
- Custom provider = `agent.ProviderConfig{ProviderName, Run RunFunc, ...}` where
  `RunFunc = func(ctx, []*message.Message, ...Option) iter.Seq2[*ResponseUpdate, error]`.
  Wrapping bodh's `pkg/llm.Client` (or an Ollama HTTP client) is straightforward.
- `anthropicprovider.NewAgent(anthropic.Client, AgentConfig{Model, Instructions})` —
  streaming + tool calling + cache-token accounting. **Anthropic is native.**
- `openaiprovider` accepts a pre-built `openai.Client` → `option.WithBaseURL(...)`
  covers **Ollama via its OpenAI-compatible endpoint**. Azure OpenAI same path.
- Tools: `functool.New[In,Out]` auto-generates JSON schema from struct tags;
  `mcptool.Connect/ListTools/AddTool`; `agenttool.New` (agent-as-tool);
  `tool.ApprovalRequiredFunc` marks tools needing human approval.

### Agent core / middleware (the admission-control mapping)
- `agent.Middleware.Run(next RunFunc, ctx, messages, options) iter.Seq2[...]` —
  chain wraps the provider call; middleware **can short-circuit/deny** (yield error,
  never call `next`) → **fail-closed governance admission is implementable**.
- Ordering: agent middlewares → history provider → context providers → provider
  middlewares → LLM call. Injected messages get source attribution
  (`SourceTypeMiddleware` etc.).
- `agent/harness/toolapproval` = HITL tool approval middleware w/ standing rules
  persisted in Session; `toolautocall` = the tool loop; also `agentmode`, `loop`, `todo`.
- Structured output via provider `Format`/`Unmarshal` hooks + `WithStructuredOutput(v)`.

### Workflow engine (the orchestration mapping)
- Graph: `workflow.NewBuilder(start).AddEdge/AddDirectEdge(cond)/AddFanOutEdge/
  AddFanInBarrierEdge/AddSwitch(...).WithOutputFrom(...).Build()`. Conditional edges
  give the diagnostician→test-planner **loop-back** (condition on confidence) directly.
- **Rule-based agents are first-class**: any pure-Go func/struct is an executor via
  `workflow.NewExecutor(id, handler)` — no LLM/provider needed. Bodh's rule
  diagnostician ports as a plain executor.
- Checkpointing: pluggable `checkpoint.Store[json.RawMessage]`
  (CreateCheckpoint/RetrieveCheckpoint/RetrieveIndex) → **a Postgres store is a small,
  clean implementation**. Checkpoints form a DAG → time-travel/resume.
- HITL: `RequestPort{ID, Request, Response}` bound as an executor; run emits
  `RequestInfoEvent`, caller answers via `run.SendResponse` → maps to pkg/hitl.
- Events: `ExecutorInvoked/Completed/Failed`, `SuperStepCompleted` (w/ checkpoint info),
  `OutputEvent`, plus **custom domain events via `ctx.AddEvent`** → audit-trail feed.
  OTel spans: workflow.session / executor.process / message.send, gen_ai.* attrs;
  message content only if `EnableSensitiveData=true` (default off → PHI-safe posture).
- Execution: **in-process only** (goroutines, Pregel-style super-steps). No distributed
  runner. Inter-process story = A2A hosting (agent as A2A server + a2aprovider client).

### Verdict shape (for the assessment)
- MAF replaces: pkg/orchestration bus, agent plumbing, tool loop, HITL transport,
  hand-rolled checkpoint/session state, parts of gateway admission (as middleware),
  MCP/A2A clients, agent OTel.
- Bodh keeps (no MAF-go equivalent): audit chain-hash, PHI de-id, governance policy
  stack, graduation ladder, **all evals** (.NET-only), **RAG/knowledge** (.NET-only),
  FHIR/HL7, cmd/care web+SPA, Postgres persistence.
- Decoupling implication: MAF is in-process → it supersedes the *substrate* of
  specs/002 but answers ADR-0007 by NOT distributing (or distributing via A2A),
  not via 24 NATS services.

## 4. Final assessment (delivered 2026-07-11)

**Verdict:** the fit is real — MAF-go replaces bodh's *orchestration substrate*, not
bodh itself. It is in-process only, which REFRAMES the decoupling plan rather than
completing it.

Direct mappings verified:
- MARA pipeline → workflow graph; diagnostician loop-back = `AddDirectEdge` with a
  confidence condition; virtual physician panel → group-chat builder.
- Rule diagnostician → pure-Go executor (no provider needed).
- `gateway.Pipeline.Admit` → outermost `agent.Middleware` (short-circuit = fail-closed).
  Caveat: agent middleware guards *agent runs*; per-message uniformity for pure-Go
  executors needs a thin executor-wrapper shim.
- `pkg/hitl` → `RequestPort` + `toolapproval` harness middleware.
- Audit feed → workflow event stream (`ctx.AddEvent` for domain events);
  `EnableSensitiveData=false` default matches the never-copy-PHI rule.
- Durable state → implement `checkpoint.Store[json.RawMessage]` over Postgres (small).
- `pkg/mcp` / `pkg/a2a` / agent OTel → `mcptool` / `a2aprovider` / `otelprovider`.

Bodh keeps (no MAF-go equivalent): evals (all of it — .NET-only there), RAG/knowledge,
audit chain-hash, PHI de-id, governance policy stack, graduation ladder, FHIR/HL7,
care API + SPA, Postgres persistence.

Risks: public preview (API churn — pin a commit), in-process only (A2A is the
inter-process story), per-message boundary uniformity shim.

### Recommendations (presented, AWAITING USER DECISION)

1. **Strategy** → new sibling repo, vertical slice first: 4-agent MARA pipeline on MAF
   workflows; rule diagnostician as executor; one LLM agent via anthropicprovider;
   governance as fail-closed middleware; audit from event stream; Postgres
   checkpoint.Store; one RequestPort for physician review. Import bodh's
   pkg/audit + pkg/phi + pkg/governance as libraries. ~1 week; answers boundary/eval/
   durability questions before any platform bet.
2. **specs/002** → pause; let the slice decide its fate (aligns with ADR-0007).
3. **Providers** → native anthropicprovider + openaiprovider w/ base URL → Ollama;
   rule path needs no provider.

### DECIDED 2026-07-11: user said "plan next work and start the work" → recommendations adopted

`../bodh-maf/` scaffolded (own git repo, initial commit `0134c39`):
- go.mod pinned to MAF-go `v0.0.0-20260710122455-75d0a647f739`, `replace ../bodh`
- README.md (scope, non-goals, 5 exit criteria)
- architecture.md (DRAFT — **awaiting user design approval before any Go code**):
  Mermaid component + sequence diagrams; admit-wrapper design (fail-closed gov /
  fail-open audit, uniform across pure-Go executors AND llm agent middleware);
  audit bridge (sync deny writes, async telemetry); Postgres checkpoint.Store;
  RequestPort physician gate w/ kill/resume test; eval recorder closing the
  pkg/runtime eval-mirror gap (option b); 5-step PR delivery plan.

specs/002 in bodh: PAUSED pending slice results (step 5 writes verdict back here).

### Housekeeping notes

- CLAUDE.md edits + session.md are uncommitted in the working tree on
  `feat/agent-decoupling-phase0`.
- MAF-go clone lives in the session scratchpad (ephemeral — re-clone if needed:
  `github.com/microsoft/agent-framework-go`, reviewed at HEAD `75d0a64`).
- To resume this CLI session later: `claude --continue --dangerously-skip-permissions`
  (from the shell, not inside the session).

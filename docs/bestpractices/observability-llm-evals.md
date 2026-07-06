# Observability, LLM & Evals Conventions

Patterns for tracing, the LLM abstraction, the model pool, the eval harnesses, RAG, and
external protocols — with `file:line` evidence. See [README.md](README.md) for context.

---

## 1. Tracing / observability

**RULE: LLM tracing is a hook-based side channel. Agents wrap `llm.Client` in a
`TracingClient` that builds one flat `LLMTrace` per call; a global hook forwards it to
OTLP/Laminar only when `BODH_OTEL_ENABLED=1` AND a hook is installed AND the binary was built
`-tags otel`. The traced call's response/error is returned unchanged.**

- The single hook seam (`pkg/observability/llm_trace.go:57-67`): `RecordLLMCall` logs one
  `[llm-trace]` JSON line, always fires Prometheus (no tag), and forwards to `otelHook` only
  when `otelHook != nil && os.Getenv("BODH_OTEL_ENABLED") == "1"`. Default `otelHook` is nil.
- Transparent wrapper (`pkg/observability/llm_client.go:98-126`): times the inner call, builds
  `LLMTrace` from `Response` (tokens/stop reason/`len(ToolCalls)`), and returns the
  response/error **unchanged** (errors are recorded in `LLMTrace.Error` *and* propagated).
- **PHI-safe attributes:** `CaseID` is hashed (see
  [security-and-compliance.md](security-and-compliance.md) §1); prompt/response content is
  never attached — only token counts, provider, model, stop_reason, tool_calls.
- Every `otel`-gated file has a `!otel` stub with identical API that no-ops `Record`/
  `Shutdown` and errors from the constructor, so the default build never links the OTel SDK
  (`otlp_stub.go`, `laminar_stub.go`).
- Laminar specifics (`laminar.go`): OTLP/HTTP to `api.lmnr.ai:443` path `/v1/traces`,
  `Authorization: Bearer <LMNR_PROJECT_API_KEY>`, the recognition marker
  `lmnr.span.type=LLM`, and `gen_ai.*` semconv attributes. `otlp.go:92-96` also sets the
  process-global TracerProvider + W3C propagators so `otelhttp` handlers share it.
- **`AgentCallbacks`** are per-lesson optional structs of nil-able func fields
  (`OnStart`/`OnToolCall`/`OnFinish`) — a copy-per-lesson convention mirroring the TypeScript
  `AgentCallbacks`, NOT a shared observability interface (`pkg/agent/toolcalling/types.go:28-44`).
- **Always-on Prometheus** (`pkg/observability/prom.go`): namespace `bodh`, metrics
  `bodh_llm_calls_total{provider,model,agent_id}`, `bodh_llm_tokens_total{...,direction}`,
  `bodh_llm_duration_seconds{provider,model}`.

---

## 2. LLM abstraction

**RULE: `llm.Client` is a 3-method provider-agnostic interface; providers are selected at
startup and everything (tracing, pooling) composes by *wrapping* it.** See
[go-conventions.md](go-conventions.md) §2 for the interface + provider-add procedure.

- `Request`/`Response` are flat structs (`pkg/llm/client.go:26-176`) supporting `System`,
  `Messages`, `MaxTokens`, `Temperature`, `JSONSchema` (structured output), `Tools`,
  `CacheSystem` (prompt-caching hint), and `Grammar` (sampler-level, kronk/llama.cpp only).
- Adapters normalize provider quirks: Anthropic wires `JSONSchema` → forced `tool_use`
  (`anthropic.go:177-190`) and `CacheSystem` → `cache_control: ephemeral` (`:166-171`);
  `Grammar` on Anthropic warns once and proceeds (`:51-53`).

**RULE: Model selection is `envOr("BODH_<PROVIDER>_MODEL", "<default>")` per binary; defaults
are `claude-sonnet-4-6` / `gpt-4o` / `llama3.2`.** Canonical: `cmd/planning/main.go:134-152`.
`Client.Model()` is descriptive only and never validated (`client.go:16`).

**RULE: `pkg/llm/pool` is a concurrent, refcounted, TTL-evicted cache of `llm.Client` keyed by
`ModelSpec{Provider,Model,Tag}` — built for local-GPU (kronk) model residency; cloud providers
pass a no-op Closer and unbounded `MaxResident`.**

- `ModelSpec.Key()` = `provider/model@tag`; `Factory func(ctx, ModelSpec) (llm.Client,
  io.Closer, error)` — the Closer is what eviction calls (GPU unload for kronk, no-op for
  cloud). `Get` builds the client **outside** the lock so slow GPU loads don't block other
  specs; concurrent double-construct is de-duped. `MaxResident` admission via LRU eviction or
  block-on-cond. TTL sweeper goroutine runs only when `TTL > 0`. (`pkg/llm/pool/pool.go`.)

**Caveat — ADR-0011 (aspirational):** models and prompts are NOT pinned/validated today. The
only pinning is per-judge (`pkg/eval/judges/types.go:40` — `ModelID` + `CalibratedAt`).
ADR-0011 proposes a constructor-boundary model allowlist and versioned/hashed prompts (7+
prompts are currently bare `const`s). Treat as target state.

---

## 3. Evals (three harnesses, three incompatible "pass" scales)

**Caveat up front (ADR-0010):** these harnesses do NOT share types, scales, or thresholds, and
**none gates deploy today.** Document them as capabilities, not gates. See
[testing.md](testing.md) §6.

- **Single-turn clinical grading = LLM-as-judge**, fixed 4-dimension rubric
  (correctness/specificity/safety/differential_quality), `Temperature: 0.0`, structured JSON,
  and **`Pass` recomputed as `score >= 0.6`** — never the model's self-reported pass
  (`pkg/eval/judge.go:15-50,77,157-162`). Coarser exact-match layer: `ScoreCase` →
  `DiagnosisMatch` via `strings.EqualFold`, `Success = DiagnosisMatch && UnderBudget`
  (`pkg/eval/metrics.go`).
- **Multi-turn eval** runs the agent loop against **mocked tools** (deterministic), scores
  with three evaluators (tool-order prefix-match, forbidden-tools, LLM-judge), averages
  unweighted, passes at Overall ≥ 0.6; a dataset is a JSON array of `{data, target}`
  (`pkg/evals/multiturn/`).
- **`pkg/evals/laminar`** is a faithful Go port of Python `lmnr.evaluate()` —
  `Config{Data, Executor, Evaluators}`, run locally, upload in two HTTP calls (create run →
  batch datapoints).
- **Calibrated judge:** measure TPR/TNR on a human-labeled set, then Rogan-Gladen–correct the
  observed pass rate with a bootstrap 95% CI (deterministic PCG seed `42,0`)
  (`pkg/eval/judges/calibration.go`).
- **Governance/research evals** each have their own package + `cmd/*` + Makefile target +
  threshold: redteam (`ShouldRefuse`/`ShouldNotContainPHI`), equity (stratified accuracy +
  Wilson CI, `BiasAlert` at 10pp disparity — AIGP threshold), drift
  (`warning`>threshold / `critical`>2×). Targets: `make redteam-eval`, `equity-eval`,
  `drift-monitor`, `evals`, `laminar-eval`.

---

## 4. Knowledge / RAG

**RULE: Retrieval is hybrid — vector cosine + Okapi BM25 fused via Reciprocal Rank Fusion
(RRF, k=60); the result is deterministic and the RRF order is the fallback for everything
downstream.** `pkg/knowledge/hybrid.go` (`bm25K1=1.5, bm25B=0.75, rrfK=60`;
`fused[i] += 1/(rank+rrfK)`, stable sort desc, top-k `Citation`s with 200-char snippets).
`Document`/`Citation` carry `Source`+`Metadata` for provenance.

**RULE: Reranking is an OPTIONAL, fail-open post-stage — `nil` reranker = pass-through; ANY
rerank error, cardinality mismatch, or unknown ID silently degrades to the upstream RRF order.
The agent NEVER observes a rerank failure (AGENTS.md invariant #3).**
`pkg/knowledge/rerank_hook.go:31-88`; interface contract at
`pkg/knowledge/rerank/rerank.go:42-48` ("the deterministic rule-based result is the
fallback"). Opt in via `NewInMemoryStoreWithReranker(...)`.

**RULE: The real cross-encoder reranker (`bge-reranker-v2-m3-Q8_0`) is kronk-gated and
env-selected via `BODH_RERANKER=kronk`; the default build links a stub whose `Rerank` returns
`ErrNotBuilt`, so the store falls back to RRF order.** Same kronk-isolation rationale as
observability/LLM (`pkg/knowledge/rerank/kronk/`).

---

## 5. MCP / A2A

**RULE: The MCP client is an interface backed by an `HTTPClient` (JSON-RPC 2.0 over HTTP+SSE)
configured with functional `Option`s; every `CallTool` emits exactly ONE audit `CallEvent`
recording only endpoint/tool/duration/outcome — NEVER arguments or response content.**
`pkg/mcp/client.go`: `NewHTTPClient(endpoint, ...Option)` with `WithAuth`/`WithRecorder`/
`WithTimeout` (default 30s)/`WithTenantID`/`WithCaseID`/`WithTraceID`. `recordCall`
sanitizes the endpoint and classifies outcome (timeout vs error via
`context.DeadlineExceeded/Canceled`).

**RULE: The A2A server is a minimal Google Agent-to-Agent HTTP server — agent card at
`/.well-known/agent.json`, async submit `POST /tasks`, poll `GET /tasks/{id}`, SSE at
`GET /tasks/{id}/stream`; tasks run in a background goroutine driven by a single user-supplied
`Handler`.** `pkg/a2a/server.go`: state machine Submitted→Working→Completed/Failed, SSE polls
every 100ms and closes on terminal state. Task store is an in-memory `sync.Map` with no
persistence/eviction — consistent with the "minimal" framing.

# Go Engineering Conventions

Observed patterns for backend Go code, with `file:line` evidence. See
[README.md](README.md) for the five meta-conventions these expand on.

---

## 1. Build-tag + stub pattern

**RULE: Every optional/heavyweight feature is one file behind `//go:build <tag>` plus a
sibling `_stub.go` behind `//go:build !<tag>` that exports the identical type/constructor
surface — so the default build always compiles and links no extra deps.**

Naming: real file is `<feature>.go` (or `<feature>_<tag>.go`), fallback is
`<feature>_stub.go` carrying the negated tag. The `//go:build` line sits *after* the
file's doc-comment block, separated by a blank line, then `package`.

```go
//go:build !otel

package observability
```

Concrete pairs:

| Real file (tag) | Stub (negated tag) |
|---|---|
| `pkg/observability/otlp.go` (`//go:build otel`) | `pkg/observability/otlp_stub.go` (`//go:build !otel`) |
| `pkg/comm/nats.go` (`nats`) | `pkg/comm/nats_stub.go` (`!nats`) |
| `pkg/llm/kronk/kronk.go` (`kronk`) | `pkg/llm/kronk/kronk_stub.go` (`!kronk`) |
| `pkg/llm/pool/kronk_factory.go` (`kronk`) | `pkg/llm/pool/kronk_factory_stub.go` (`!kronk`) |

Two sub-conventions:

- **A stub returns a clear sentinel error, it does not silently no-op**, when the feature
  is genuinely unavailable: `pkg/llm/pool/kronk_factory_stub.go:28` yields `ErrKronkNotBuilt`;
  `pkg/observability/otlp_stub.go:24` returns `errors.New("...requires the 'otel' build tag")`.
  (Pure telemetry sinks like `Record`/`Shutdown` *do* no-op — the distinction is
  "constructor/behavior a caller might depend on" vs. "fire-and-forget side channel".)
- **API that must exist under BOTH tags lives in an untagged `_common.go` file.**
  `pkg/llm/pool/kronk_common.go:4-16` holds `KronkOptions` + `ErrKronkNotBuilt` so only the
  `Factory` *constructor* differs by tag. This is what keeps `cmd/care` tag-agnostic.

**Caveat:** tag-gated code is invisible to default `go test ./...` / `go build ./...` and
rots silently. `make build-tags` (`Makefile:288-307`) compile-checks default + `otel`;
`kronk`/`nats` are still not built in open CI (ADR-0011). Under `-tags kronk` the real
kronk SDK is still a placeholder import (`kronk_factory.go` — "integration track #21 pending").

---

## 2. Interface-at-the-boundary design

**RULE: One narrow interface per external concern, defined in the *consuming* package;
concrete providers are plain constructors returning the interface; selection is a `switch`
on a string flag at `main()` — never a registry or `init()` side effect.**

The canonical example, `llm.Client`, has exactly three methods (`pkg/llm/client.go:10-23`):

```go
type Client interface {
	Provider() string
	Model() string
	Complete(ctx context.Context, req Request) (*Response, error)
}
```

- Constructors return the **interface**, not the concrete type — this is the load-bearing
  choice that lets agents accept `llm.Client` and stay provider-blind:
  `NewAnthropic(apiKey, model) Client` (`pkg/llm/anthropic.go:30`), `NewOpenAI`, `NewOllama`.
- Selection is a string `switch` at the boundary: `cmd/care/main.go:545-566`
  (`switch spec.Provider` over `kronk|anthropic|openai|ollama|default`), each branch checking
  its env key and returning `(llm.Client, io.Closer, error)`.
- **Adding a provider:** (1) add `pkg/llm/<name>.go` with `New<Name>(...) Client`; (2) add a
  `case` in each `switch` (`cmd/intro/main.go:114`, `cmd/care/main.go:546`); (3) extend the
  flag help string listing valid values (`cmd/care/main.go:44`). No global registration.

Same shape recurs for `comm.Bus` (Subscribe/Publish, `pkg/comm/bus.go:55`),
`gateway.Middleware` (`middleware.go:31`), `governance.Policy`, `audit.Recorder`,
`knowledge.Reranker` — all injected into `NewOrchestrator(reg, bus, policy, env)`
(`pkg/orchestration/orchestrator.go:137`).

---

## 3. Error handling idioms

**RULE 3a — Wrap with `fmt.Errorf("<pkg-or-op>: <what>: %w", err)`:** a package/operation
prefix, a human phrase, `%w` for the cause.

```go
fmt.Errorf("multiturn: open dataset %q: %w", path, err)   // pkg/evals/multiturn/runner.go:61
fmt.Errorf("risk_calculator: decode arguments: %w", err)  // pkg/tools/risk_calculator.go:108
```

`"decode arguments: %w"` is the recurring sub-phrase for tool-arg JSON unmarshal failures.

**RULE 3b — Package-level sentinels are exported `Err*` vars via
`errors.New("<pkg>: <msg>")`, matched with `errors.Is`.** `pkg/llm/client.go:180`
(`ErrNoMessages`), `pkg/auth/auth.go:47-59` (a whole block). Consumers branch with
`errors.Is(err, auth.ErrForbidden)` (`pkg/mid/authorize.go:78`),
`errors.Is(err, pgx.ErrNoRows)` (`pkg/persistence/postgres/store.go:348`). Messages are
namespaced with the package name.

**RULE 3c (the important one) — Fail CLOSED on the safety/authorization path, fail OPEN on
the observability/audit path, and annotate the asymmetry inline at each site.**

- Governance error → deny: `pkg/gateway/pipeline.go:63-67` ("Fail closed … return false");
  middleware error → block (`middleware.go:22`).
- Audit sink error → logged, swallowed: `pkg/gateway/pipeline.go` `record()` ("audit is
  fail-open: never block message flow on audit downtime").
- **Deny is always audited; allow is audited only for clinical message types** (keeps the
  HIPAA log signal-rich): `pipeline.go:71-78`, `clinicalMessageTypes` map at
  `orchestrator.go:30`. That map is *intentionally duplicated* across `orchestrator`,
  `governance/medical_policy.go`, and `gateway` with a comment telling you to update all
  copies — an accepted trade-off to avoid a shared import.

See [security-and-compliance.md](security-and-compliance.md) §4 for the one deliberate
exception: break-glass audit fails *closed*.

---

## 4. CLI binary structure

**RULE: `cmd/<name>/main.go` opens with a package doc comment (usage + env-var table), then
`main()` declares the flag quartet `-provider / -model / -message / -verbose`, each
defaulting through `envDefault("<NAME>_<VAR>", fallback)`; then resolves a per-provider model
default, builds the client via a local `buildClient` switch, and runs.**

`cmd/intro/main.go` and `cmd/toolcalling/main.go` are byte-for-byte the same shape:

```go
providerFlag := flag.String("provider", envDefault("INTRO_PROVIDER", "ollama"), `…`)
modelFlag    := flag.String("model",    envDefault("INTRO_MODEL", ""),          "…")
messageFlag  := flag.String("message", "What is an AI agent?", "…")
verboseFlag  := flag.Bool("verbose", false, "…")
flag.Parse()
```

- Model default resolved per-provider **after** parse (`cmd/intro/main.go:70-77`).
- `buildClient(provider, model, host) (llm.Client, error)` + `envDefault(key, fallback)`
  helpers at file bottom (`cmd/intro/main.go:110-134`); fatal errors via
  `log.Fatalf("<name>: %v", err)`.
- Env naming: `<BINARYNAME>_PROVIDER` / `<BINARYNAME>_MODEL`; shared infra vars
  (`OLLAMA_HOST`, `ANTHROPIC_API_KEY`) stay unprefixed.
- Bigger binaries keep the shape but rename the provider flag for its role:
  `cmd/care/main.go:42-45` uses `-diagnostician` whose help enumerates valid values.

---

## 5. Package layout / naming

- **RULE 5a — Every package has a dedicated `doc.go`** (45 of them) explaining *why the
  abstraction exists* and *what it deliberately does NOT do* (`pkg/llm/doc.go:39` — "What
  this package does NOT do": retries, backoff). Some files additionally pair with a
  `<file>_doc.go` documenting that file's surface separately.
- **RULE 5b — Agent lesson packages split `types.go` (structs/config/callbacks) + `run.go`
  (the `RunAgent` entrypoint) + optional `tools.go`/`prompt.go`.** Uniform across
  `pkg/agent/*` (`intro/`, `toolcalling/`, `planning/`, `debate/`, …). This is the pattern
  CLAUDE.md's "Adding a new lesson" section codifies.
- **RULE 5c — Proprietary/heavy provider impls go in a named leaf subpackage, one per
  integration.** The `kronk/` subpackage name recurs across `pkg/llm/kronk/`,
  `pkg/audio/kronk/`, `pkg/knowledge/rerank/kronk/` — "proprietary provider lives in
  `<concern>/kronk/`". `pool/` isolates multi-model residency from the base `llm` interface.

---

## 6. Context, concurrency, message-bus idioms

The orchestrator (`pkg/orchestration/orchestrator.go`) is the reference for cross-cutting
control flow. Study it before adding anything to the message path.

- **RULE 6a — Typed messages flow through `comm.Bus` (two verbs only); agents never
  `Publish` directly, only the orchestrator does.** `pkg/comm/bus.go:7,55`. Routing is by
  `msg.To`.
- **RULE 6b — `context.Context` is the first parameter everywhere, threaded unbroken** from
  `Start(ctx)` into the subscribe handler and on into `HandleMessage(c, msg, env)`.
- **RULE 6c — One central closure is the single instrumentation point.** The Start closure
  runs a fixed 3-step sequence — (1) governance deny/allow + selective audit, (2) `recordAudit`
  *before* invoking, (3) `HandleMessage` then re-publish returned messages. Agents contain no
  cross-cutting logic. Documented as five properties in the Start doc comment.
- **RULE 6d — Guard the Go for-range closure-capture pitfall** by copying the loop var to a
  local before the closure: `handler := a // capture for closure` (named "the classic Go
  for-range capture pitfall" inline).
- **RULE 6e — `sync.Once` guards one-shot lifecycle; deps captured at Subscribe-time are
  immutable thereafter.** `startOnce sync.Once` makes `Start` idempotent (double-subscription
  would deliver every message twice). `SetAuditRecorder`/`SetEvalStore` MUST be called
  *before* `Start` ("CRITICAL ORDERING") because they're captured into the per-agent closure.
  Panics in handlers are recovered upstream by `pkg/comm.safeHandle`, so the orchestrator has
  no `recover` of its own.

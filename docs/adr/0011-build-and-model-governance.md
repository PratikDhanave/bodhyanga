# ADR 0011: Every Build Config in CI; Pin and Eval-Gate Models and Prompts

**Status:** Accepted

**Date:** 2026-06-30

**Deciders:** project owner (Pratik Dhanave)

> Authored from the principal-engineer architecture review (2026). Claims are
> grounded in the codebase with `file:line` references that can be checked
> directly. See docs/adr/README.md for how this set relates to ADRs 0001–0005.

---

## Context

Two coupled governance gaps surfaced in the architecture review, both rooted in things that ship but are not exercised by CI.

GAP 1 — Build tags hide shipping code from default CI.
Bodh uses `//go:build` tags to gate optional features (ADR-0003): `otel`, `kronk`, and `nats`. The default `go build ./...` / `go test ./...` compiles none of them; each tagged file has a `_stub.go` counterpart so the default build stays clean. Tagged files: `pkg/comm/nats.go` (`//go:build nats`), `pkg/llm/kronk/`, `pkg/audio/kronk/`, `pkg/knowledge/rerank/kronk/` (`//go:build kronk`), `pkg/observability/otlp.go`+`laminar.go`+`trace_handler.go` (`//go:build otel`).

The failure mode this ADR exists to prevent already happened: the NATS JetStream bus silently stopped compiling because its dependency was never committed, and nobody noticed — no CI lane built it. This is documented in the Makefile itself (Makefile:290-302): "the `nats` JetStream bus silently stopped compiling ... and nobody noticed because no CI lane built it. Build tags hide code from the default `make build`/`make test`; anything that ships must be compiled somewhere in CI or it WILL rot."

The partial remediation is a `make build-tags` target (Makefile:303-307) that compile-checks default + otel only: `go build $(PKG)`, `go build -tags otel $(PKG)`, `go vet -tags otel $(PKG)`. Three holes remain:
1. CI does not call `make build-tags`. The actual workflow (.github/workflows/ci.yml) runs untagged `go build ./...` (line 26) and `go vet ./...` (line 29), plus a narrow otel slice scoped to ONE package: `go test -tags otel ./pkg/observability/...` (line 47) and `go vet -tags otel ./pkg/observability/...` (line 50). It never compiles the full tree under `-tags otel`, and never touches `kronk` or `nats` at all (grep for "kronk\|nats\|build-tags" in ci.yml: NONE).
2. `nats` is excluded from `build-tags` "until its dependency lands" (Makefile:297-298) — but the dependency HAS landed. `go.mod:101` now carries `github.com/nats-io/nats.go v1.52.0` (indirect), and `go build -tags nats ./pkg/comm/` compiles successfully today. The Makefile comment is stale; the target should be re-extended to `-tags nats`.
3. `kronk` needs the proprietary `github.com/ardanlabs/kronk` module and genuinely cannot compile in open-source CI (Makefile:299-300). It is the one config that legitimately needs a separate enterprise lane.

GAP 2 — Models and prompts are under-governed in a regulated clinical context.
Model selection flows through `BODH_*_MODEL` env vars with hard-coded fallbacks, repeated across every binary. Examples: `cmd/planning/main.go:134` (`envOr("BODH_ANTHROPIC_MODEL", "claude-sonnet-4-6")`), `:145` (`"BODH_OPENAI_MODEL", "gpt-4o"`), `:152` (`"BODH_OLLAMA_MODEL", "llama3.2"`); identical patterns in `cmd/scribe/main.go:126-130`, `cmd/streaming/main.go:152-165`, `cmd/filesystemtools/main.go:204-222`, `cmd/agentloop/main.go:114`. The constructors `llm.NewAnthropic(apiKey, model)` (pkg/llm/anthropic.go:30) take an opaque model string and never validate it — `Client.Model()` (pkg/llm/client.go:16) is purely descriptive. For the shipping care server, the multi-model pool factory (cmd/care/main.go:544-565) builds clients straight from `spec.Model` with no allowlist. Net effect: an operator can change the model the entire clinical pipeline runs on by editing an env var, with zero CI signal and no record. In a regulated setting, "the model changed" is itself a safety + compliance event — the validation that justified clinical use was performed against a specific model version.

Prompts are bare Go `const` string literals scattered across agent files with no version, no hash, no validation linkage — at least 7 system prompts: `agents/questioner_llm.go:150` (questionerSystemPrompt), `agents/diagnostician_llm.go:264` (diagnosticianSystemPrompt) and `:281` (diagnosticianSystemPromptRAG), `agents/diagnostician_debate.go:280`, `agents/diagnostician_reflexion.go:238`, `agents/reasoning_verifier_llm.go:162`, `agents/inbox_triage_llm.go:194`. Editing any of these changes clinical behavior with no gate. The codebase already understands this risk in ONE place: `pkg/eval/judges/types.go:40` pins `ModelID` per judge with a `CalibratedAt` timestamp precisely so re-calibration can be detected "after a model update" (types.go:30-33) — but that discipline is local to the LLM-as-judge layer and is not applied to the production agents.

An eval gate already EXISTS but is not wired to model/prompt changes. SD-Bench (cmd/bench/main.go) supports per-provider baseline regression gating: `-fail-on-regression` (cmd/bench/main.go:143) compares against `data/cases/baseline.json` via `checkRegressions` (cmd/bench/main.go:494), with accuracy as a floor and mean cost as a ceiling (cmd/bench/main.go:520-527, tolerances accuracyToleranceP=1.0pp, costToleranceUSD=$0.01). But CI runs only a 5-second smoke — `go run ./cmd/bench -timeout=5s` (ci.yml:56) — WITHOUT `-fail-on-regression`. The gate is built and unused. This is the ADR-0005 (eval-gating) tie-in: the machinery to make a model/prompt change prove itself already exists; it just isn't on the change path.

## Decision drivers

- Regulatory: in a HIPAA-grade clinical decision-support system, a model or prompt change is a safety + compliance event that must be recorded and re-validated, not a silent env-var edit
- Rot prevention: the NATS bus already rotted to uncompilable undetected because no CI lane built it (Makefile:290-302) — anything that ships must compile somewhere in CI
- Reuse existing machinery: the eval gate (cmd/bench -fail-on-regression, baseline.json) and the pin-and-calibrate pattern (pkg/eval/judges/types.go:40) already exist and are proven; the fix is mostly wiring, not new infrastructure
- Open-source constraint: kronk depends on a proprietary module (github.com/ardanlabs/kronk) and cannot compile in public CI — any solution must not force proprietary checksums into the default go.sum (preserves ADR-0003)
- Cost and CI latency: a full eval suite per PR is expensive (real LLM calls, dollars, minutes); the gating design must balance coverage against PR turnaround
- Determinism/auditability: model strings and prompts must become validated artifacts (pinned, hashed, versioned) so an auditor can answer 'what model and prompt produced this clinical output?' from the repo

## Options considered

### A. Status quo — narrow inline otel check + unused eval gate

- **Pros:** Zero work. Default `go build ./...` and the observability-scoped `-tags otel` test (ci.yml:47-50) already catch the most common breakages. The eval gate and judge-pinning patterns exist if anyone chooses to run them.
- **Cons:** Directly leaves the documented rot vector open: kronk and the full otel tree are never compiled in CI, and nats — though it compiles again today (go.mod:101) — is still outside any lane, so it will silently rot again. Model/prompt changes remain ungoverned: an env-var edit (cmd/planning/main.go:134 et al.) silently re-points the clinical pipeline with no CI signal and no audit record — unacceptable for a regulated system. The eval gate stays decorative (ci.yml:56 runs bench without -fail-on-regression).

### B. Decision (recommended) — every shipping config compiled in CI; pin models; version prompts; eval-gate model/prompt changes

- **Pros:** Closes the rot vector: CI compiles default + otel (full tree) + nats, and an enterprise lane compiles kronk. Makes the Makefile build-tags target honest by re-adding nats (the dep landed, go.mod:101) and calls it from CI. Turns model strings and prompts into validated artifacts: model allowlist enforced at the constructor/pool boundary (pkg/llm/anthropic.go:30, cmd/care/main.go:544-565), prompts moved from bare consts to versioned+hashed artifacts (generalizing pkg/eval/judges/types.go:40). Activates the eval gate (cmd/bench -fail-on-regression against baseline.json) on any model/prompt diff — reusing existing machinery, satisfying ADR-0005. Produces an auditable answer to 'what model/prompt ran?'
- **Cons:** Real cost: an eval lane makes real LLM calls (dollars + minutes per PR); needs careful scoping (gate only on model/prompt-touching diffs, or nightly) to avoid throttling every PR. Kronk still needs a separate enterprise CI with the proprietary module — public CI cannot fully cover it. Prompt-versioning is a non-trivial refactor across 7+ agent files. Allowlist adds a small friction step when intentionally adopting a new model.

### C. Compile-coverage only — fix Gap 1, defer model/prompt governance

- **Pros:** Cheap and fast: extend `make build-tags` to nats and call it from CI, add an enterprise kronk lane. Fully closes the documented rot vector with no LLM-call cost and no refactor. Buys time on the harder governance question.
- **Cons:** Leaves the larger clinical-safety gap open: model and prompt drift remain silent and ungoverned (the env-var swap and bare-const prompts are untouched). In a regulated context this is the more serious of the two gaps, so fixing only the cheaper half is a partial answer that an auditor would flag.

### D. Hard-pin everything in code; delete BODH_*_MODEL env overrides

- **Pros:** Maximally deterministic and auditable: the model is a compiled constant, diffs are visible in git, no runtime surface to drift. Simplest possible 'what model ran' answer.
- **Cons:** Breaks legitimate operational flexibility the env vars exist to serve — air-gapped clinics swapping to a local Ollama model, enterprise routing to kronk, per-deployment model choice (cmd/care/main.go:540-561 pool wiring assumes runtime provider/model selection). Forces a recompile + redeploy for every model change, which in practice pushes operators to fork and patch — worse for auditability than a governed allowlist. Throws away the multi-model pool design rather than governing it.

## Decision

Adopt Option B, sequenced so the cheap rot-prevention half lands first and the governance half follows.

Gap 1 — compile every shipping config in CI:
1. Make the `build-tags` target honest. Re-add the nats lane that Makefile:297-298 deferred — the dependency has landed (`github.com/nats-io/nats.go v1.52.0`, go.mod:101) and `go build -tags nats ./pkg/comm/` already succeeds. Target should compile-check default, `-tags otel` (full tree, not just `pkg/observability`), and `-tags nats`.
2. Call `make build-tags` from .github/workflows/ci.yml so every non-proprietary config is compiled on every PR. This replaces the narrow inline `-tags otel ./pkg/observability/...` checks (ci.yml:47-50) with full-tree coverage.
3. Add a separate enterprise CI lane that compiles `-tags kronk` (and `-tags "otel kronk nats"`) in an environment with the proprietary `github.com/ardanlabs/kronk` module. This is the one config public CI legitimately cannot cover (Makefile:299-300); it must not pull proprietary checksums into the default go.sum (preserves ADR-0003).

Gap 2 — pin and eval-gate models and prompts:
4. Pin models as validated artifacts. Introduce a model allowlist enforced at the construction boundary — the `llm.New*` constructors (pkg/llm/anthropic.go:30, openai.go, ollama.go) and the care pool factory (cmd/care/main.go:544-565) — so an unrecognized model string fails fast rather than silently re-pointing the pipeline. `BODH_*_MODEL` overrides survive but may only select an allowlisted, validated model; adding a model is a reviewed change to the allowlist.
5. Version prompts as artifacts. Generalize the judge-pinning pattern (pkg/eval/judges/types.go:40, ModelID + CalibratedAt) to the production agent prompts currently held as bare consts (agents/questioner_llm.go:150, diagnostician_llm.go:264/281, diagnostician_debate.go:280, diagnostician_reflexion.go:238, reasoning_verifier_llm.go:162, inbox_triage_llm.go:194). Each prompt carries a version/hash; the hash is recorded on the audit trail with each clinical action.
6. Eval-gate model/prompt changes. Wire the existing SD-Bench gate (cmd/bench -fail-on-regression against data/cases/baseline.json, cmd/bench/main.go:143/494) into the change path: any diff touching an allowlisted model, a pinned prompt, or the baseline must pass the gate. Because real LLM calls cost money and time, scope the gate to model/prompt-touching diffs (path filter) and/or a nightly lane rather than every PR. This is the ADR-0005 eval-gating tie-in and reuses machinery that already exists but is currently unused (ci.yml:56 runs bench without the gate flag).

## Consequences

### Positive

- The documented rot vector is closed: nats, full-tree otel, and (in the enterprise lane) kronk are all compiled in CI, so the NATS-style silent breakage (Makefile:290-302) cannot recur undetected.
- The Makefile build-tags target stops lying: its nats exclusion (Makefile:297-298) is removed now that go.mod:101 carries the dependency and the tag compiles.
- Model and prompt changes become auditable, validated artifacts: an allowlist at pkg/llm constructors + the care pool (cmd/care/main.go:544-565) and versioned/hashed prompts mean an auditor can answer 'what model and prompt produced this output' from the repo and the audit log — closing a real HIPAA-grade compliance gap.
- The eval gate stops being decorative: cmd/bench -fail-on-regression (cmd/bench/main.go:143/494) finally runs on the change path, so a model or prompt swap must prove it does not regress accuracy or blow the cost ceiling before merge.
- Reuses proven in-repo patterns rather than inventing infrastructure: the judge pinning model (pkg/eval/judges/types.go:40) and SD-Bench baseline gating already exist and are battle-tested.

### Negative

- CI gets slower and costs money: a real-LLM eval lane spends dollars and minutes; without path-scoping or a nightly cadence it would throttle every PR.
- Prompt-versioning is a real refactor touching 7+ agent files (questioner_llm.go, diagnostician_llm.go, diagnostician_debate.go, diagnostician_reflexion.go, reasoning_verifier_llm.go, inbox_triage_llm.go) plus the audit-recording path.
- Public CI still cannot fully cover kronk — coverage depends on a separate enterprise lane with the proprietary module, so a kronk-only breakage is caught later than default/otel/nats breakages.
- Adopting a new model now requires an allowlist change and a passing eval run, adding friction to what was a one-line env-var edit (cmd/planning/main.go:134 et al.) — intended, but a workflow cost.

### Risks

- Eval-gate flakiness: LLM nondeterminism plus tight tolerances (accuracyToleranceP=1.0pp, costToleranceUSD=$0.01, cmd/bench/main.go:131-132) can produce false-positive regressions that erode trust in the gate; tolerances and sample size need tuning.
- Allowlist as a bypass magnet: if the allowlist lives only in code and the env override can still name an off-list model, teams under deadline pressure may widen it without an eval run, reintroducing silent drift — the constructor-boundary enforcement must be the single chokepoint.
- Stale baseline: data/cases/baseline.json must be re-blessed deliberately on every intended model/prompt change (the -update-baseline path, cmd/bench/main.go:144); a careless `-update-baseline` silently launders a regression into the new baseline.
- Enterprise-lane divergence: if the kronk CI lane is not kept in lockstep with default CI, kronk-tagged code can still rot between enterprise runs, a smaller version of the original problem.
- Cost-driven scoping holes: if the gate is narrowed to 'diffs touching prompts/models' via path filters, an indirect change (e.g. a refactor of buildPayload in pkg/llm/anthropic.go) that alters effective behavior without touching a pinned artifact could slip the gate.

## References

- Makefile:290-307
- cmd/bench/main.go:143
- cmd/bench/main.go:494
- cmd/bench/main.go:520
- cmd/care/main.go:544
- pkg/llm/anthropic.go:30
- pkg/llm/client.go:16
- pkg/eval/judges/types.go:40
- agents/questioner_llm.go:150
- agents/diagnostician_llm.go:264
- agents/diagnostician_debate.go:280
- cmd/planning/main.go:134
- cmd/scribe/main.go:126
- cmd/streaming/main.go:152
- go.mod:101
- pkg/comm/nats.go:1
- docs/adr/0003-build-tags-optional-features.md
- .github/workflows/ci.yml:46
- data/cases/baseline.json

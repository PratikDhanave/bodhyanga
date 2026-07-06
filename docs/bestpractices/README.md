# Bodh Best Practices

This directory codifies the engineering conventions that **already exist in the Bodh
codebase**, with `file:line` evidence for each rule. It is descriptive, not aspirational:
every rule here is something the code does today. Where a pattern is designed but *not yet
in force*, it is flagged with the ADR that tracks the gap — do not cite those as guarantees.

> **How to read a rule.** Each rule is stated as a one-line imperative a developer can
> follow, followed by the proof (`pkg/…:line`) and, where relevant, a **Caveat** noting an
> ADR that says the guarantee is not actually wired into `cmd/care` yet.

## The five meta-conventions

These recur everywhere; internalize them first.

1. **Deny / hash / redact by default; make every exception audited.** The safety path
   (governance, auth) fails *closed*; the observability path (audit, tracing) fails *open*.
   PHI is hashed or redacted at every seam. Break-glass is the one audited exception — and
   even its audit fails *closed*.

2. **One optional feature = one build-tagged file + one `_stub.go` twin.** Heavyweight or
   proprietary code (`otel`, `kronk`, `nats`) lives behind `//go:build <tag>`, paired with a
   `//go:build !<tag>` stub exposing the identical API. The default build links no extra
   deps. Tagged code is invisible to default CI, so it rots silently — `make build-tags`
   guards it (this is why ADR-0011 exists; the `nats` bus once stopped compiling unnoticed).

3. **Interfaces are narrow and defined in the consumer; providers are plain constructors
   selected by a `switch` at `main()`.** No global registries, no `init()` side effects.
   `llm.Client` (3 methods) is the canonical example; `comm.Bus`, `governance.Policy`,
   `audit.Recorder`, `gateway.Middleware`, `knowledge.Reranker` all follow the shape.

4. **Rules live next to the code as doc comments that state the *trade-off*, not just the
   behavior.** Every package has a `doc.go` explaining *why the abstraction exists* and
   *what it deliberately does NOT do*. Non-obvious decisions are annotated inline with the
   reason and often the PR that introduced them. Match that density.

5. **Available ≠ in force.** The most valuable habit in this repo: distinguish a pattern
   that is *implemented and tested* from one that is *wired into the production path*. The
   tamper-evident audit chain, the graduation gate, and model pinning are all in the former
   bucket. Check before you rely.

## Topic guides

| Guide | Covers |
|---|---|
| [go-conventions.md](go-conventions.md) | Build-tag+stub pattern, interface boundaries, error idioms, CLI shape, package layout, orchestration concurrency |
| [security-and-compliance.md](security-and-compliance.md) | PHI handling, audit chain-hash, governance policy stack, fail-closed/open, auth (OPA+JWT), break-glass, autopilot fence |
| [observability-llm-evals.md](observability-llm-evals.md) | Tracing hooks (OTLP/Laminar), `llm.Client` abstraction, model pool, the three eval harnesses, RAG hybrid retrieval + rerank, MCP/A2A |
| [testing.md](testing.md) | `dbtest` testcontainers, build-tag test stubs, table-driven + fixtures, frontend Vitest/Playwright, the actual enforced CI gates |
| [ci-ops-frontend.md](ci-ops-frontend.md) | Makefile structure, CI workflows, DB migrations, OPA/Rego, infra (compose/k8s), React 18 + i18n + safety-UX conventions |

## Relationship to other docs

- **`CLAUDE.md`** (repo root) — the quick-start orientation for Claude Code. This directory
  is the deep version.
- **`docs/adr/`** — *why* the architecture is shaped this way. ADR-0006 (research-platform
  vs. clinical-product identity) gates how 0007–0012 resolve; read it first. The caveats in
  these guides point back to specific ADRs.
- **`AGENTS.md`** (repo root) — the numbered PHI/audit invariants several of these rules cite.

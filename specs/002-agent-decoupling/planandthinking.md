# Bodh Agent Decoupling — Plan & Thinking (External Review Edition)

> **Companion to** `specs/002-agent-decoupling/plan.md`. The plan states *what* the
> target architecture is. This document adds the *thinking* the plan omits: the
> steelmanned alternatives, the code-grounded evidence (including corrections where the
> plan is wrong), and a simulated adversarial review across operational,
> distributed-systems, and HIPAA lenses. It is **self-contained** — an external model can
> review the architecture from this file alone, without the codebase.

---

## 0. How to review this document

### What Bodh is

Bodh is a **HIPAA-grade medical-AI orchestration platform written in Go**. It implements
the Multi-Agent Reference Architecture (MARA) for clinical decision support. The clinical
pipeline is a sequential-diagnosis state machine:

```
intake → test-planning → diagnosis → care-plan
```

driven by a `DiagnosticSupervisor` that routes typed messages between specialized agents
(questioner, test-planner, cost-guardian, diagnostician, reasoning-verifier, specialists,
human-review, plus chronic-care/transition-of-care agents). Today **everything runs in one
process**: `cmd/care/main.go` registers ~19 agents, an in-process orchestrator
(`pkg/orchestration`) wires governance + audit + agent-execution + eval + fan-out into one
closure, and an in-memory message bus (`pkg/comm` `InMemoryBus`) carries messages. The
project wants to split this monolith into **per-agent binaries + a zero-agent backend**,
communicating over **NATS JetStream**.

### The hard constraints (these are invariants, not preferences)

1. **Zero agent code in the backend.** The new backend binary `cmd/api/` must compile
   without importing `github.com/PratikDhanave/bodh/agents`, `pkg/agent`, or
   `pkg/orchestration`. This is meant to be enforced at the *module* boundary, not by a
   lint rule. The security argument for the whole rearchitecture rests on this: a
   compromised public HTTP surface must not contain the diagnostician's LLM client or the
   symbol that reads `ANTHROPIC_API_KEY`.
2. **API keys live only in `.env`** (and, in production, per-agent Kubernetes Secrets).
   They never appear in tracked files or code. Per-agent binaries mean the intake binary's
   process env must not carry the diagnostician's key.
3. **PHI rule: `bodh.case_id = sha256(caseID)[:8]` (hex) on all spans.** Raw MRNs / raw
   `caseID` must never appear in collectors, logs, or telemetry. `msg.Content` (chief
   complaint, HPI, diagnosis narrative, care-plan JSON) is raw PHI and must never be copied
   into the audit log.

### Exactly what feedback is wanted

You are one of several external models (Grok / GPT / Gemini) reviewing this independently.
We want **adversarial, code-grounded** critique, specifically:

- **Correctness of the distributed-systems design.** The plan claims NATS JetStream gives
  durability "already production-ready, no code changes needed." Section 3 (evidence) and
  Section 5 (adversarial) show this is **false against the current code**. Tell us what the
  real delivery semantics must be and what must be built.
- **Whether the four decisions** (Section 2) survive scrutiny, and whether the steelmanned
  alternatives we rejected were rejected for the right reasons.
- **HIPAA-specific regressions** introduced by decoupling: distributed audit, kill-switch,
  PHI-on-the-wire, fail-open multiplication.
- **Concrete answers to the open questions in Section 6.** Those are the highest-value
  items. If you can only address one section, address that one.

Be exhaustive, precise, and honest about weaknesses. Cite the `file:line` references inline
(they are checkable against the repo) and challenge any claim you find unsupported.

---

## 1. Problem & goal

`cmd/care/main.go` is a monolith: ~19 agents, one process, one container, one module
boundary, one in-memory bus. The plan's stated motivations (plan §1):

| Monolith pain | Consequence |
|---|---|
| Diagnostician update forces redeploying questioner | No independent deploy cadence |
| LLM agents starve rule-based agents for CPU | No resource isolation |
| One agent panic can kill the whole process | No fault isolation |
| All agents share the same process env / API keys | No secret isolation; large blast radius |
| Scaling means scaling everything | No independent autoscaling |
| Cannot run agents in different clouds/regions/GPUs | No placement flexibility |
| Backend is import-coupled to agent implementations | Backend binary contains all agent code |

**Goal**: decompose into a 3-tier system — a thin **backend** (`cmd/api/`, HTTP + auth +
Postgres + HITL + NATS publish, *zero agent code*), **per-agent binaries** (each agent =
one Go binary, one image, one Deployment, one Secret, one NetworkPolicy), and a **message
bus** (NATS JetStream) — while keeping the existing monolith (`cmd/care/`) alive verbatim
as the local-dev / CI / demo mode.

**Non-goal**: a storage-paradigm change. State stays in Postgres (current-state rows, not
an event log). The decoupling is about *binary and deployment isolation*, not about how
data is persisted.

---

## 2. The four decisions

Four architectural decisions are user-approved. Each is presented with the chosen option,
a steelmanned alternatives table, trade-offs accepted, and residual risks. **Honesty note:**
two of these decisions rest on plan claims that the code contradicts — those corrections
are surfaced here and detailed in Section 3.

### 2.1 Decision A — Process topology: per-agent binary

**Chosen.** Each distinct agent bus-ID gets its own ~20-line `main()` that calls
`runtime.Run(&agents.XAgent{})`, compiled to its own static binary, its own distroless
image, its own K8s Deployment + Secret + NetworkPolicy. Same-bus-ID variants collapse
behind a `--provider` flag (the 6 LLM diagnosticians; rule/LLM inbox-triage). The 8
stateless specialists collapse into one `specialists` binary behind a `--specialty` flag —
a degenerate, principled application of affinity (nothing to isolate between byte-identical
units). Net inventory: **~21 agent binaries + `cmd/api` + `cmd/eval-mirror` + the retained
`cmd/care` monolith** (the plan states "24 images" in §3 but "23 images" / "22 Dockerfiles"
in §10/§14 — the inventory is not yet pinned; see risk below).

| Option | Pros | Cons | Why not chosen |
|---|---|---|---|
| **A. Per-agent binary** *(chosen)* | Binary-level isolation: intake's artifact literally does not contain the diagnostician's LLM client or the `ANTHROPIC_API_KEY`-reading symbol. Independent deploy/scale/secrets/network per agent. One agent panic/OOM kills one pod; NATS redelivers. Marginal cost of binary N+1 is near zero (20-line `main`, shared `pkg/runtime`, shared Docker builder layer). | 24-image sprawl (build/scan/sign/store/patch ×24). Config dispersed across 21 `main()`s + 21 Deployment env blocks. Operational fan-out (Deployments, HPAs, NetworkPolicies, dashboards, alerts). More NATS hops per case. Two assembly paths (monolith + per-agent) that can drift. | — (chosen) |
| **B. Generic runtime + YAML manifest** (one universal binary, agent selected at runtime) | Exactly ONE image to scan/sign/SBOM — a single CVE/compliance artifact. Zero per-binary boilerplate. K8s-native: one image as N Deployments differentiated by env var. Declarative, reviewable agent config. **Retains** B's availability + independent-scaling wins (separate pods). | The single binary statically contains **every** agent's code and is wired to read **every** secret. Isolation degrades from a `go build` guarantee to a runtime `if`-statement. Every agent's CVE is every pod's CVE; can't patch one agent's dep without redeploying all. A maximally valuable supply-chain target. | The one decisive axis — binary-content isolation — is structurally impossible here, and it is exactly the property a HIPAA clinical platform exists to guarantee (plan §4). Its real wins (one image to scan) are operational conveniences paid in the cheapest currency; the cost is a weakened security boundary on the most sensitive system. **Strongest counter to A** — and the right call for a non-PHI fleet. |
| **C. Grouped-by-affinity** (a few binaries, several agents each) | Image sprawl drops to ~6–8. Groups by trust tier (all no-secret rule agents together; isolate LLM/secret agents) preserving *most* of the security argument. Groups by resource profile (5 tiny stateless agents share one HPA). Fewer intra-group NATS hops → lower latency on supervisor↔questioner ping-pong. ~7 containers compose-up far more comfortably than 24. | Re-introduces shared blast radius *within* each group (one panic kills the group). Blurs independent deploy (changing supervisor redeploys questioner/intake too). Grouping boundaries are a judgment call that drifts (inbox_triage already has rule+LLM under one bus-ID). Secret isolation only as good as the boundary. | The plan engineered A's marginal cost to near-zero, so the thing grouping *buys* (fewer artifacts) is cheap to forgo, while what it *costs* (intra-group coupling, partial deploy coupling) is exactly what the rearchitecture is paying to remove. **The natural fallback** if 24-image ops proves too heavy; re-grouping later is cheaper than splitting. |

**Trade-offs accepted (eyes open):** (1) image sprawl — a base-image CVE rebuilds 24 images
not 1, mitigated by a shared builder layer + Makefile-templated Dockerfiles; (2) config
dispersion into 21 `main()`s + env blocks; (3) operational fan-out; (4) more network hops
(every hand-off is a NATS round-trip + JSON serialize where the monolith used an in-process
call); (5) dev ergonomics bought back by retaining `cmd/care` as dev mode — at the cost of
maintaining two assembly paths that must stay behaviorally consistent; (6) the
separate-module / `go.work` complexity (Decision B).

**Risks:** two assembly paths drift (a dep correct in monolith mode missing in a per-agent
binary, uncaught by monolith CI); secret isolation is only as strong as the K8s substrate
(a broadly-mounted Secret or permissive RBAC undoes the compile-time guarantee); 24-image
ops may prove too heavy; at-least-once delivery (Section 5) requires every agent to be
idempotent — false for side-effecting agents; **isolation assumes independent failures**
but NATS / Postgres / the LLM endpoint are common-mode dependencies, so the win protects
against agent-local faults (panic, OOM, RCE), not shared-substrate outages.

**Why the decisive axis is binary-content isolation (the steelman judgment).** Bodh is
HIPAA-grade, and its agents have radically heterogeneous trust, blast-radius, and resource
profiles. Today cmd/care holds *both* `ANTHROPIC_API_KEY` and `OPENAI_API_KEY` in one process
and runs LLM agents next to rule agents. The threat model for a clinical LLM platform is
prompt-injection / model-driven RCE / dependency compromise in the agent that touches
untrusted model output and egresses to `api.anthropic.com`. Per-agent binaries make that blast
radius **physical, not policy-based**: the intake binary's compiled artifact literally does not
contain the diagnostician's LLM client, prompt-assembly code, or the symbol that reads
`ANTHROPIC_API_KEY`. An attacker who lands RCE in intake finds no API key in the process env, no
LLM egress allowed by its NetworkPolicy, and no diagnostician code to pivot through. Option B's
universal binary degrades this from a compile-time/kernel boundary to a runtime `if`-statement —
exactly the boundary an injection attack tries to subvert. For a system whose security posture
is its core selling point, a guarantee enforced by `go build` (the symbol isn't there) dominates
one enforced by configuration (the symbol is there but we promise not to call it). The honest
counter, conceded above: Option B keeps **most** of A's operational wins (separate pods ⇒
availability isolation + independent scaling) — only binary-content isolation is exclusive to A.
That is the entire crux of the decision.

### 2.2 Decision B — Compile-time boundary: separate Go module for `agents/`

**Chosen.** Split `agents/` into its own module `github.com/PratikDhanave/bodh/agents`
(with its own `go.mod` that `require`s `bodh`), joined to the main module by a `go.work`
workspace for local dev, with `cmd/care` relocated into the agents module to break the
otherwise-circular module dependency.

| Option | Pros | Cons | Why not chosen |
|---|---|---|---|
| **Separate module + `go.work`** *(chosen)* | True **compile-time** one-way enforcement: `go build ./cmd/api` cannot resolve `.../agents` because the symbol isn't in the main module's import graph. The day someone adds `agents.IntakeAgent{}` to a backend handler, the backend stops compiling. `go.work` restores single-checkout dev ergonomics (cross-module symbols resolve against local source; atomic cross-module commits preserved). | Two version lines + a two-phase release-tag ritual (`vX.Y.Z` for root, `agents/vX.Y.Z` for agents). `go.work` + `go.work.sum` to maintain. Workspace can **mask version skew** — `make test` green in workspace mode while a clean isolated build fails; needs a `GOWORK=off` CI lane (not in the plan). Relocating `cmd/care` (and, missed by the plan, `cmd/bench`, `cmd/caredemo`, `cmd/demo`, which also import the full agent stack). | — (chosen). **Caveat:** see correction in §3.4 — the plan's "strictly one-way" claim is *false today* because `pkg/shadow` (main-module library) imports concrete agents; that must be refactored first. |
| **Single module + lint rule** (depguard / go-arch-lint banning agent imports in `cmd/api`) | Zero module-versioning overhead. No `cmd/care` relocation; Phase 2 disappears. Can express finer policy (e.g. also ban `pkg/agent`, which the module split does *not* catch). Trivially reversible. | Advisory only: bypassed by `//nolint`, a missing linter, a CI misconfig, or a plain local `go build`. Must enumerate banned packages and keep current; transitive violations slip unless every intermediary is covered. | The platform's compliance narrative treats "backend contains zero agent code" as a hard guarantee; a lint rule makes it a maintained convention. **Note:** it would still require the `pkg/shadow` cleanup, so it saves nothing there — it only forgoes the permanent guarantee. |
| **`internal/` package boundary** | Compiler-enforced, one module, no `go.work`/dual versioning. Go's `internal/` is a first-class feature. | `internal/` controls *who-can-import* (external modules), not directionality. `cmd/api` shares the repo root, so it would **not** be blocked from importing `internal/agents`. Wrong tool for an intra-repo split. | Enforces a repo boundary, not the backend-vs-agents boundary needed. |
| **Separate Git repos (multi-repo)** | Strongest isolation: independent CI, release cadence, access control. | Every cross-cutting change (e.g. the Phase 0 interface extractions) becomes a two-repo, two-PR, tag-and-bump dance with no atomic commit. No workspace escape hatch. | Overkill; the two-module-one-repo layout gets the same compile guarantee while keeping atomic commits and one clone. |
| **Status quo (no boundary)** | Zero cost; ship features faster; runtime decoupling could still proceed (binary isolation ≠ module isolation). | Nothing stops backend re-importing agents; the boundary degrades back to the monolith on the first convenient import. | The entire spec exists to establish the boundary. |

**Trade-offs accepted:** two version lines + the release-tag ritual (largely dormant while
everything ships as monorepo-built containers, painful only on external `go get`); a
`go.work` + `go.work.sum` to maintain plus an owed `GOWORK=off` CI lane per module;
relocation of `cmd/care` **and** `cmd/bench`/`cmd/caredemo`/`cmd/demo` (more than the plan's
"~15 min" Phase 2 estimate); a residual gap — `pkg/agent` stays in the main module (it is
just `type Message = protocol.Message`), so `cmd/api` *can still import `pkg/agent`* after
the split; the module boundary enforces "no concrete `agents/`" but **not** "no
`pkg/agent`," which still needs a lint guard.

**Risks (highest first):** **feasibility-blocking** — `pkg/shadow/shadow.go` (non-test, main
module) instantiates `agents.NewDiagnostician`, `&agents.IntakeAgent{}`, `&agents.DiagnosticSupervisor{}`
and 5 more (shadow.go:352–365, **verified in repo**), so the split cannot compile until
`pkg/shadow` is moved/inverted/deleted — the plan's §3 "strictly one-way" claim is wrong
and this Phase-0 work is omitted; migration scope understated (3 extra binaries); workspace
masks version skew without `GOWORK=off`; incomplete boundary (`pkg/agent` still importable);
release-coordination tax if external consumption is introduced; reversibility is moderate
but non-trivial churn (the decision is sticky).

**`go.work` / `go.work.sum` / versioning subtleties an external reviewer should weigh.** The
`go.work` file (`use .` + `use ./agents`) restores single-checkout ergonomics: `make test` and
editor tooling resolve cross-module symbols against local source, so the split is invisible
day-to-day — edit a `pkg/` type and the agents module sees it immediately, no `replace`
directive, no version bump. `go.work` is a **local-development** construct: it is committed here
(idiomatic for a single-repo two-module layout) but ignored by downstream `go install`/`go get`.
`go.work.sum` is the workspace checksum file (`go work sync`) and must be committed alongside.
**The trap:** `go.work` papers over version skew between the two `go.mod`s during development —
`make test` can pass in workspace mode while a clean module-isolated build (`cd agents && go
build ./...` against the *published* `bodh` version) fails because `agents/go.mod` pins a `bodh`
version lacking a symbol present in local source. CI must therefore run at least one
`GOWORK=off` build per module — which the plan does not mention. **Release tax:** two modules =
two version lines; the convention is tag prefix `agents/vX.Y.Z` for the nested module and
`vX.Y.Z` for the root. Cutting a release where agents depend on a new backend `pkg/` change is a
two-phase dance (tag root → bump `agents/go.mod` require → `go mod tidy` → tag `agents/...`);
isolated agent releases lag the backend by one tag cycle. This tax is **largely theoretical for
internal monorepo-HEAD container builds** (the Dockerfiles build from source with `go.work`
present) and bites only on external independent `go get`.

### 2.3 Decision C — Supervisor stays an agent container (eval-mirror split out)

**Chosen.** `DiagnosticSupervisor` remains a standalone binary subscribing to
`bodh.{tenant}.diagnostic_supervisor` and routing by `msg.Type`, exactly like every other
agent. The eval-mirror side-channel (previously hardcoded inside the orchestrator closure)
becomes its own standalone NATS subscriber (`cmd/eval-mirror/`). The backend imports zero
agent code.

| Option | Pros | Cons | Why not chosen |
|---|---|---|---|
| **Supervisor = agent container** *(chosen)* | The supervisor's `switch msg.Type` **is** the clinical workflow (ask→test→verify→diagnose, the cost-rework loop, terminal finalize), reviewable/versioned by clinical owners. Keeping it an agent preserves the backend-purity invariant. It is a zero-value `struct{}` → ~20-line `main`, 50m-CPU/64Mi pod, no secrets, no DB — nearly free. Eval-mirror as its own subscriber turns a hidden hot-path `if` into an explicit, independently-failing observer. | One extra always-on container. One NATS round-trip per supervisor hop (~4/case). A new `cmd/eval-mirror` service to operate. The terminal-case contract is now cross-service convention, not a single closure. | — (chosen). **Caveat:** see §3.5 — "zero eval dependency" is inaccurate (supervisor already imports `pkg/eval` for a local log); and the eval-mirror cannot observe `len(out)==0` from outside (Section 5, distributed lens). |
| **Fold routing into `cmd/api` (inline switch)** | Eliminates one binary + one NATS hop on every case. Backend already publishes agent-addressed messages, so it reuses an existing bus handle. Removes a per-message dependency on a supervisor pod. | Either imports `agents.DiagnosticSupervisor` (literal ban) or re-implements the diagnostic state machine inline (semantic violation, **worse**) — relocating clinical workflow into the JWT/OPA/PHI tier. A flow change now redeploys the auth-and-PHI surface; the cost-rework loop is non-trivial clinical state mutation behind an HTTP handler. | Trades a near-zero infra saving (one featherweight pod) for a structural breach of the central invariant. The supervisor is stateless and horizontally scalable, so it is not a true SPOF. |
| **Generic runtime (supervisor in shared binary)** | Single artifact; supervisor "deploys" as a manifest entry. Keeps routing in the agent layer (satisfies backend purity). | Defeats binary isolation (the supervisor — zero-secret — co-resident with agents holding every key). No independent scaling; no fault isolation; forces union of network egress. | The supervisor is the *least*-harmed agent to co-locate, but the fleet decision must be uniform; carving an exception buys nothing (it's already trivial to run standalone) while undermining the security story. |
| **Keep eval-mirror inside the supervisor binary** | One fewer service; scoring happens in the same process as the terminal diagnosis; no second subscription. | Forces the supervisor to import `pkg/eval` and hold an `eval.Store` (Postgres) handle, destroying the zero-dependency property; couples clinical routing availability to eval-store availability. | Re-couples safety-critical routing to non-critical observability for a trivial saving. |
| **Workflow engine / subject-routing config (no agent code)** | Could express linear hops declaratively; flow changes become config. | The supervisor is **not** a pure linear pipe: the `tests_revise` cost-rework loop (decode state, drop costliest test, recompute, re-submit, bounded termination) and terminal finalize are conditional clinical control flow a static subject map cannot express. Adds a new dependency, harder to audit. | Over-engineering; moves complexity rather than removing it. A 200-line commented Go state machine is more auditable for HIPAA than a generic engine. |

**Trade-offs accepted:** one extra featherweight container; ~4 NATS round-trips/case (vs
in-process in the monolith); a new ~80-LOC `cmd/eval-mirror` + its durable consumer; the
terminal-diagnosis contract is now a cross-service convention that must be documented and
tested; distributed-systems complexity (at-least-once, idempotency) pushed onto the
supervisor — tolerable because it computes purely from `msg.Type`+`msg.Content` with no
cross-case memory.

**Risks:** **eval-mirror split-brain** — the monolith's terminal predicate is
`len(out)==0 && agentID=="diagnostic_supervisor" && msg.Type=="diagnosis"` (orchestrator.go:340),
but a standalone subscriber sees diagnosis messages *arriving* and cannot observe that the
supervisor emitted zero follow-ups → double-count / mis-score; **at-least-once
double-scoring** (eval `Save` must be idempotent on `case_id`); the supervisor is a workflow
chokepoint (a poison message it repeatedly NAKs stalls all in-flight cases); routing-contract
drift across the module boundary (a rename or not-yet-deployed target = silently dropped
message); loss of the orchestrator's single governance/audit chokepoint (moves into each
binary's gateway — a misconfigured pipeline can bypass audit); eval-mirror is an unmonitored
silent failure (cases complete normally while scoring quietly stops).

### 2.4 Decision D — Shared clinical state: shared Postgres + store interfaces + RLS

**Chosen.** Extract `PatientStore` / `TaskStore` / `ReviewStore` Go interfaces (in
`pkg/medical`, `pkg/tasks`, `pkg/hitl`); back them with Postgres adapters; every agent
binary and `cmd/api` connect to the same tables, scoped per request by `SET LOCAL
app.tenant_id` inside a transaction so the existing `FORCE ROW LEVEL SECURITY` policies
isolate tenants.

| Option | Pros | Cons | Why not chosen |
|---|---|---|---|
| **Shared Postgres + store interfaces + RLS** *(chosen)* | The interface is the real seam (an *import* fix), identical regardless of storage topology — choosing A doesn't foreclose B/C. Most substrate already exists: `PatientRegistryAdapter` satisfies `PatientStore` (store.go:467–484); RLS is real (migration 0005: `FORCE ROW LEVEL SECURITY`, `NOBYPASSRLS` roles, `current_setting('app.tenant_id')`). HIPAA tamper-evidence already solved by the audit hash-chain (`pkg/audit/immutable.go`), so event-sourcing's main selling point is redundant. ACID + read-your-writes is exactly right for HITL gates. | Agents needing state hold `DATABASE_URL` + a connection pool (larger secret surface than a mediating service). Agents coupled to physical schema via adapters. Postgres becomes a central availability/scaling pinch point. No built-in temporal replay. | — (chosen) |
| **Dedicated state microservice** | Single chokepoint for tenant scoping; agents never hold DB credentials (smaller blast radius). Decouples agents from physical schema; natural home for caching, rate-limit, field redaction; versioned contract for non-Go / external A2A consumers; connection-pool consolidation. | Adds a network hop on the clinical hot path + a new availability dependency (state service down = all agents down). A *second* distributed-systems problem on top of NATS. Tends to **weaken** isolation: today RLS is FORCE-d at the row level by Postgres (kernel-enforced); a state service typically connects as one privileged role and re-implements tenant scoping in app code — one missing scope check = cross-tenant PHI leak. Transactional HITL becomes multi-RPC partial-failure handling. | Pays a permanent latency/availability/complexity tax now for schema-decoupling and credential-isolation the project doesn't yet need, and moves the isolation guarantee from a strong layer to a weaker one. The interface seam keeps this a cheap **later** swap. |
| **Full event-sourcing** | Complete ordered immutable history (fits §164.312(b)); temporal/point-in-time replay; no update anomalies; pairs with the event-like NATS flow; rebuild new read models without backfill. | Large complexity on the hot path: event versioning/upcasters, projections, projection-lag, snapshotting, eventual-consistency reasoning. **Read-your-writes is no longer free** — a HITL "is this still pending?" may read a stale projection, unacceptable for a human safety gate. Most compliance benefit is **already delivered** by the audit hash-chain. | Highest complexity; its differentiator is redundant here; it trades away the exact property (transactional read-your-writes for HITL) the clinical workflow most needs. Can be added later as a downstream consumer of the NATS/audit stream. |

**Why the interface, not the storage topology, is the real decision.** The genuine coupling
this solves is an *import* problem: cmd/api must not import `agents`/`pkg/agent`, and agents in
separate containers can no longer share `*medical.PatientRegistry` / `*tasks.Queue` /
`*hitl.Queue` in-process memory. The minimal fix is to replace three concrete field types with
three interfaces (~75 LOC). Crucially, that interface boundary is **identical regardless of
whether the adapter behind it talks to local Postgres, a remote state service, or an event
store** — an agent holds a `PatientStore`, and a `pgAdapter` can later be swapped for a
`stateServiceClient` or an `eventSourcedProjection` without touching agent code. So Decision A
(shared Postgres) does not foreclose B or C; the question is narrowly "what is the cheapest
correct adapter to ship first?" — which makes A the default and puts the burden of proof on the
alternatives. The HIPAA-specific reason A wins outright: event-sourcing's main compliance
selling point (an immutable tamper-evident history for §164.312(b)) is **already delivered** by
the existing `pkg/audit/immutable.go` Merkle/SHA-256 hash-chain, so C would add
replay/projection/versioning machinery to the clinical hot path for marginal incremental value
while trading away the transactional read-your-writes consistency a human-in-the-loop safety
gate most needs.

**Trade-offs accepted:** state-dependent agents hold `DATABASE_URL` + a pool (larger
surface, bounded by per-agent least-privilege roles + NetworkPolicy); schema coupling via
adapters (mitigated: agents depend on the Go interface, not DDL; additive migrations don't
break consumers); Postgres as a central pinch point (addressable later with PgBouncer / read
replicas); forgo temporal replay (retain tamper-evident history via the audit hash-chain);
last-write-wins current-state model (concurrent writers rely on transactional discipline +
optimistic-concurrency columns where needed).

**Risks:** **tenant context must be set on every transaction in every binary** — RLS is
fail-closed only if `app.tenant_id` is set correctly; a forgotten `set_config` sees no rows,
or (if a BYPASSRLS/owner role is ever used) leaks cross-tenant PHI; connection-pool
exhaustion as agents scale (N pools vs one Postgres); distributing DB credentials widens the
credential blast radius; migration coordination across many binaries; shared mutable
current-state under concurrent writers → lost updates without explicit concurrency control;
**the audit hash-chain is currently file/JSONL** — migration 0005 provisions
`bodh_audit_app` but notes the Postgres audit sink is "pending a future PR," so the
§164.312(b) guarantee leaned on to reject event-sourcing is weaker than assumed until that
is wired; the interface seam only keeps B/C cheap if adapters don't leak Postgres-specific
semantics (`*pgx` types, SQL errors, RLS assumptions).

---

## 3. Code-grounded evidence

This section is what lets an external reviewer **trust the plan**. Each claim is graded
*confirmed / partial / refuted* against the actual source, with `file:line` + snippet.
**Corrections are stated explicitly** — where evidence shows the plan is wrong, the
corrected fact is what you should review against, not the plan's original wording.

### 3.1 Agent inventory & dependency categories

| Claim | Verdict | Evidence |
|---|---|---|
| 38 `ID()`-bearing agent structs (plan:43) | **confirmed** | 38 distinct structs implement `func (...) ID() string` across `agents/*.go` (excluding `_test.go`/`_doc.go`). Verified `AudioTranscriberAgent` … `ToolUsingDiagnostician` = exactly 38. |
| 29 unique bus IDs | **confirmed** | De-duplicating every `ID()` return (incl. const `AudioTranscriberAgentID="audio_transcriber"`, audio_transcriber.go:96) → exactly 29: intake, diagnostic_supervisor, questioner, test_planner, cost_guardian, diagnostician, reasoning_verifier, cardiologist, pharmacist, neurologist, nephrologist, oncologist, ophthalmologist, pulmonologist, infectious_disease, radiologist, rpm_monitor, trial_screener, inbox_triage, cdm_planner, tcm_coordinator, panel_manager, pre_visit_prep, readmission_tracker, refill_manager, prior_auth, human_review, engagement, audio_transcriber. |
| Categories partition 38 structs as 18 (A) + 9 (B) + 11 (C) | **confirmed** | Union of plan's three lists (plan:45–102) = 18+9+11 = 38, no overlaps/gaps. Cat A verified zero-value: 17 are literal `struct{}`; `ReasoningVerifierAgent` has only `HITL bool` (a primitive — fair). |
| All 6 diagnostician LLM variants share bus ID `diagnostician` | **confirmed** | 7 structs return `"diagnostician"`: rule `DiagnosticianAgent` (Cat A) + 6 LLM variants. Cat B total = 6 diag + LLMQuestioner + LLMReasoningVerifier + LLMInboxTriage = 9. |
| `readmission_tracker` is Cat C with `PatientRegistry` (Get/Upsert), registered in cmd/care | **confirmed** | readmission_tracker.go:21–24 `Registry *medical.PatientRegistry; HITL bool`; :39 `Registry.Get(`, :46 `Registry.Upsert(`. cmd/care/main.go:114 `&agents.ReadmissionTrackerAgent{Registry: patients, HITL: true}` in the register loop (97–119). |
| `radiologist` exists but is NOT registered in cmd/care | **confirmed** | radiologist.go:23 `func (a *RadiologistAgent) ID() string { return "radiologist" }`, `type RadiologistAgent struct{}`. grep `radiolog` over cmd/care/*.go = 0 matches. |
| engagement & audio_transcriber already use interfaces | **partial** | `engagement.Notifier` is an interface (notifier.go:38); `audio.Transcriber` is an interface (audio.go:27). **But** the plan's Cat C row (plan:100) lists audio_transcriber deps as "Transcriber, Recorder" — the actual 2nd field is `Audit audit.Recorder` (audio_transcriber.go:53), an **audit-log** recorder, not an audio capture "Recorder." Core "already interfaces" claim holds; the "Recorder" label is a mischaracterization. |

**Correction (misleading framing, not a count error):** Plan:64 + Open Question 5 single out
`RadiologistAgent`'s absence from cmd/care as possibly intentional. This **understates** the
situation: **none of the 10 Category-A specialist/clinical agents are registered in
cmd/care** — cardiologist, pharmacist, neurologist, nephrologist, oncologist,
ophthalmologist, pulmonologist, infectious_disease, trial_screener are *also* all absent
(grep of their struct names in cmd/care/main.go = 0). The cmd/care loop wires only **19**
agents. Radiologist's omission is **not special** — it is one of many specialists not wired
into the demo server.

**Correction (audio_transcriber dependency label):** The 2nd field is `audit.Recorder`
(PHI-free event recorder), not an audio Recorder/capture device. The plan conflates the two.
Minor, but the label is inaccurate.

### 3.2 Shared-state coupling & interface extraction

| Claim | Verdict | Evidence |
|---|---|---|
| `cdm_planner` holds `*medical.PatientRegistry`, calls Get+Upsert | **confirmed** | cdm_planner.go:21 field; :67 `a.Registry.Get(`; :70 `a.Registry.Upsert(rec)`. |
| `tcm_coordinator` holds `*tasks.Queue` + `*medical.PatientRegistry`, calls Get/Upsert/Create | **confirmed** | tcm_coordinator.go:21–22 fields; :51 Get, :55 Upsert, :60 & :70 `a.Tasks.Create(...)`. |
| `panel_manager` holds `*medical.PatientRegistry`, methods "Get, Upsert, List" | **refuted** | Field confirmed (panel_manager.go:22), but actual calls are **FilterByCondition + List**, NOT Get/Upsert/List. :59 `FilterByCondition(req.Condition)`; :61 `List()`. No Get/Upsert exists. |
| `pre_visit_prep` holds `*medical.PatientRegistry`, calls only Get | **confirmed** | pre_visit_prep.go:17 field; :46 `a.Registry.Get(...)` is the only call. |
| `refill_manager` holds `*tasks.Queue` + `*medical.PatientRegistry`, calls Get/Upsert/Create | **partial** | refill_manager.go:20–21 fields; :48 `Registry.Get`; :59 `Tasks.Create`. **No `Registry.Upsert` call exists.** Actual = Get + Create. |
| `inbox_triage` holds `*tasks.Queue`, calls only Create | **confirmed** | inbox_triage.go:19 field; :65 & :72 `a.Tasks.Create(...)`. |
| `prior_auth` holds `*tasks.Queue`, calls only Create | **confirmed** | prior_auth.go:17 field; :51 `a.Tasks.Create(...)`. |
| `human_review` holds `*hitl.Queue`, calls Submit+Decide | **confirmed** | human_review.go:80 field; :174 `Queue.Submit(...)`; :235 `Queue.Decide(...)`. |
| `PatientRegistryAdapter` satisfies PatientStore surface | **confirmed** | store.go:460 type; :467 Upsert, :472 Get, :478 List, :484 FilterByCondition — mirror `medical.PatientRegistry` (registry.go:42/49/57/68). |
| `tasks.Queue` and `hitl.Queue` have NO Postgres adapter yet | **confirmed** | grep `TaskStoreAdapter|ReviewStoreAdapter` over pkg/,cmd/ = none. Plan lists them as NEW (plan:899–900). |
| Proposed `TaskStore{Create,List}` satisfied by `*tasks.Queue` | **confirmed** | tasks.go:51 `Create`, :81 `List` match plan:123–126. |
| Proposed `ReviewStore{Submit,Decide,Get,List,Pending}` satisfied by `*hitl.Queue` | **confirmed** | queue.go:111/181/218/239/263 match plan:131–136. |

**Correction (panel_manager):** plan:92 method column is **wrong**. Correct: `FilterByCondition, List`.

**Correction (refill_manager):** plan:95 **overstates** Registry usage. Correct: `Get, Create`
(no `Registry.Upsert`).

**Correction (missed migration site):** The Cat C table lists `inbox_triage` once, but **two**
source files hold `*tasks.Queue` for that family: inbox_triage.go:19 and
**inbox_triage_llm.go:33** (LLM variant, calls Create at :104,:115). Both must migrate to the
`TaskStore` interface. Additionally, `diagnostician_factory.go:464`
`func NewInboxTriage(provider string, taskQueue *tasks.Queue)` references the concrete type
as a **constructor parameter** whose signature must change when extracting `TaskStore`. The
plan's file-change list should include both.

### 3.3 HITL resume coupling

| Claim | Verdict | Evidence |
|---|---|---|
| The HTTP `/reviews/{id}/decide` handler calls `reviewAgent.ApplyDecision`, importing `agents` | **confirmed** | cmd/care/main.go:19 `"...agents"`; :87 `reviewAgent := &agents.HumanReviewAgent{Queue: reviews}`; :311 handler; :322–323 `req, resume, ok := reviewAgent.ApplyDecision(r.PathValue("id"), body.ReviewerID, body.Decision, body.Reason, body.Modification)`; :328–330 `for _, m := range resume { bus.Publish(...) }`. This is exactly the coupling to break. |
| Plan quotes the call at line 323 | **partial** | The call **begins at :322** (args spill to :323); publish loop at :328–330, not immediately after. Semantics correct; line cite off by one. |
| `ApplyDecision` = Queue.Decide + build resume from `ReviewRequest.{NextTo,NextType,Payload,Meta}` | **confirmed** | human_review.go:235 Decide; reject path returns early (:237,:247); approve picks payload (:254–257); copies meta + breadcrumbs (:264–273); :278 `out := agent.NewMessage(a.ID(), r.NextTo, agent.RoleAgent, r.NextType, payload, meta)`. `agent.NewMessage` wraps `protocol.NewMessage` (types.go:84–86); `agent.Message = protocol.Message` (types.go:44) — resume logic depends only on the protocol type, so it **can** be rebuilt in the backend without importing agents. |
| Proposed `BuildResumeMessage` snippet faithfully reproduces approve-path | **partial** | Payload selection matches :255; meta breadcrumbs match :265–273. **But** snippet hardcodes `"human_review"` where real code uses `a.ID()` (returns "human_review", :87) — equivalent; snippet omits the belt-and-braces `meta["hitl_kind"]=""` write before `delete` (:272) — end state identical. Paraphrase, not verbatim. |
| `medical.ReviewRequest` already has NextTo/NextType/Payload/Meta | **confirmed** | review.go:32–35. (Real struct has 20 fields, :24–42; plan's excerpt uses `...`.) |
| `explainHandler` takes concrete `*hitl.Queue` (must become `ReviewStore`) | **confirmed** | explain.go:263 `func explainHandler(tenantID string, rec audit.Recorder, store eval.Store, queue *hitl.Queue)`; called main.go:362 with `reviews := hitl.NewQueue()`. Handler only calls `queue.List()` (:421) + reads `medical.ReviewRequest` fields (:426–441) — satisfied by `ReviewStore`. |
| `*hitl.Queue` already satisfies the full ReviewStore surface | **confirmed** | queue.go:111/181/218/239/263 (signatures match plan). |
| `pkg/hitl/store.go` and `resume.go` are NEW (don't exist yet) | **confirmed** | Dir listing of pkg/hitl/ shows neither file. The coupling is real and unresolved. |
| `ApplyDecision` has a 2nd caller in cmd/caredemo (must keep working) | **confirmed** | cmd/caredemo/main.go:102 `_, resume, ok := reviewAgent.ApplyDecision(r.ID, "clinician-demo", "approve", "demo auto-approval", "")`. The plan's §0.4 doesn't mention it; caredemo stays in the agents-importing layer so it isn't blocked — but any extraction into pkg/hitl must keep caredemo compiling. |

### 3.4 Module boundary & the circular-dependency claim

| Claim | Verdict | Evidence |
|---|---|---|
| cmd/care imports both `agents` and `pkg/agent` | **confirmed** | main.go:19 `agents`, :20 `pkg/agent`. Both used: :97 `for _, a := range []agent.Agent{`; :98–114 instantiate `&agents.IntakeAgent{}` etc. |
| `protocol.Message` is canonical; `agent.Message` is a type alias of it | **confirmed** | types.go:44 `type Message = protocol.Message` (true alias). Struct defined message.go:71. `pkg/protocol/*.go` has **zero** internal-bodh imports → a safe leaf dependency a zero-agent backend can import alone. |
| The circular-dependency reasoning (agents-module + cmd/care-stays → cycle) | **confirmed** | Sound and grounded: if `agents/` becomes a module requiring `bodh`, keeping cmd/care in `bodh` forces `bodh` to require `bodh/agents`, while `bodh/agents` requires `bodh` → cycle (Go forbids). Moving cmd/care into the agents module yields one-way `bodh/agents → bodh`. **Forward-looking**: no `agents/go.mod`, no `go.work` exist today; go.mod declares the single module `github.com/PratikDhanave/bodh` (go 1.26). |
| Plan §3 "becomes strictly one-way: `bodh/agents → bodh`" | **REFUTED — feasibility-blocking** | `pkg/shadow/shadow.go` (non-test, **main module**) imports and instantiates agents: **verified** shadow.go:352 `agents.NewDiagnostician(provider)`, :358 `&agents.IntakeAgent{}`, :359 `&agents.RPMMonitorAgent{}`, :360 `&agents.DiagnosticSupervisor{}`, :361 `&agents.QuestionerAgent{}`, :362 `&agents.TestPlannerAgent{}`, :363 `&agents.CostGuardianAgent{}`, :365 `&agents.ReasoningVerifierAgent{}`. A main-module library importing agents means the desired direction is **violated today**. The split cannot compile until `pkg/shadow` is moved into the agents module, inverted to an injected agent-registry interface, or deleted. The plan omits this entirely. |

**Correction (the single most important one):** Plan §3's "strictly one-way" is false as the
code stands. Before the module split can even compile, `pkg/shadow` (and the test-only
`pkg/orchestration/integration_test.go` that imports agents) must be refactored. This is real
Phase-0 work the plan does not budget. **Migration scope is also understated:** beyond
cmd/care, `cmd/bench`, `cmd/caredemo`, and `cmd/demo` all import the full agent stack and must
also relocate into the agents module or be stubbed — so Phase 2's "~15 min" is an undercount.

**Correction (abbreviated import list):** Plan §0.1 ends the cmd/care import list with "… plus
hitl, tasks, medical, governance, llm, knowledge." The real block (main.go:19–38) is broader
(audio, audit, comm, engagement, eval, fhir, hl7v2, llm/pool, observability,
persistence/postgres, registry). The snippet is explicitly partial — not a contradiction — but
the coupling surface is larger than implied.

### 3.5 Orchestrator decomposition & NATS readiness

| Claim | Verdict | Evidence |
|---|---|---|
| The per-agent subscription closure begins at orchestrator.go:244 | **confirmed** | :244 `o.bus.Subscribe(agentID, func(c context.Context, msg agent.Message) {` (body ~244–359). |
| The closure does 5 things (governance, audit, agent-exec, eval-mirror, fan-out) | **confirmed** | In-line labels: :245 `Step 1: Governance` (Evaluate :258), :289 `Step 2: Record the agent invocation` (:299), :301 `Step 3: Delegate to the agent` (:315), :326/:340 `Step 4: Eval mirror`, :344 `Step 5: Fan-out` (:356–358). |
| Eval mirror hardcoded to `agentID=="diagnostic_supervisor" && msg.Type=="diagnosis"` (+`len(out)==0`) at ~line 340 | **confirmed** | :340 `if len(out) == 0 && agentID == "diagnostic_supervisor" && msg.Type == "diagnosis" {` → :341 `o.recordCaseEval(msg)`. |
| pkg/comm/nats.go: durable JetStream consumers, AckExplicit, per-agent names, subject `bodh.{t}.{id}`, broadcast subject | **confirmed** | ConsumerConfig :205–211 (`Durable`, `FilterSubject`, `AckExplicitPolicy`, `DeliverNewPolicy`); `subject()` :165–172 = `bodh.{tenant}.{id}`; empty id → `bodh.{tenant}.broadcast`; stream `bodh.>` :149. |
| NATSBus serializes `protocol.Message` to JSON | **confirmed** | :307 `json.Marshal(msg)`; :256–257 `json.Unmarshal(...)`. |
| Each process calls `comm.NewNATSBus()` independently, no shared state | **confirmed** | nats.go:101 `func NewNATSBus() (*NATSBus, error)` — **no args**, env-driven (`NATS_URL` :102, `BODH_TENANT_ID` :123); own conn + JetStream ctx; idempotent `CreateOrUpdateStream`. |
| nats.go ~355 lines | **confirmed** | `wc -l` = **355** (verified). |
| Orchestrator NOT modified/deleted; only decoupled mode skips it | **partial** | Forward-looking; can't fully verify against present code. Verifiable: the orchestrator closure is today's sole governance+audit+eval+fan-out point. `pkg/runtime`, `pkg/gateway`, `cmd/eval-mirror`, `cmd/api` **do not exist yet**. |
| Eval-mirror keeps supervisor "completely clean (zero eval dependency)" | **REFUTED** | supervisor.go:253 `metrics := eval.ScoreCase(state, ...)` — finalize() **already imports and calls `pkg/eval`** for a local metrics log (:254–255). The supervisor is NOT zero-eval-dependency today. Durable persistence (`eval.Store.Save`) is orchestrator-side only (orchestrator.go:426–465); supervisor holds no `eval.Store` field. |
| nats.go is gated behind the `nats` build tag with a stub | **confirmed** | nats.go:39 `//go:build nats` (verified); nats_stub.go `//go:build !nats`, `NewNATSBus` returns an error. |

**Correction (supervisor eval dependency):** "zero eval dependency" is inaccurate —
supervisor.go:253 already calls `eval.ScoreCase` for a local log. Correct framing: supervisor
has no `eval.Store` field and does no durable persistence, **but it does import `pkg/eval`**.
To make it truly eval-free, the local `ScoreCase` log (supervisor.go:253–255) must also be
removed — which the plan does not mention.

**Correction (NATS build-tag gating + dependency not in graph):** Plan §0.6 presents nats.go
as "already production-ready / no NATSBus code changes needed" without noting it compiles only
under `//go:build nats` (nats.go:39, **verified**). The default build links `nats_stub.go`
whose `NewNATSBus` returns an error, and the jetstream dependency is **not yet in the module
graph** (header comment says it must be added via `go get`). Decoupled deployment requires
building every agent binary with `-tags nats` AND adding the NATS client dependency — a real
prerequisite the plan omits.

**Correction (NewNATSBus signature mismatch):** §0.6 correctly says agents call
`comm.NewNATSBus()` (no args, env-driven). But the pkg/runtime example in §5 (plan:465) calls
`comm.NewNATSBus(cfg.NATSUrl, cfg.TenantID)` with two args. The real signature is
`func NewNATSBus() (*NATSBus, error)` — **zero parameters**. The §5 illustrative code would not
compile against the current API; either `NewNATSBus` changes to accept args, or the runtime
sets env vars before calling it.

**Correction (NATS reliability claims — see Section 5):** The most consequential discrepancy.
nats.go **acks at :264 BEFORE dispatching the handler in a detached goroutine at :267**
(verified). That is **at-most-once**, contradicting plan §13's "ack AFTER outputs published,"
"redelivered after AckWait timeout (30s)," "nak → redelivery," and "DLQ." `ConsumerConfig`
(:205–211) sets no `AckWait`, no `MaxDeliver`, no dead-letter subject. The stream is
`Replicas: 1`, `FileStorage`, `MaxAge 24h` (**verified** at nats.go:151–153). The plan's entire
durability story requires a NATSBus rewrite that is not budgeted in any phase.

### 3.6 The load-bearing code (verbatim, so this can be reviewed without the repo)

External reviewers cannot open the repo. The four snippets below are the facts the entire
critique pivots on; they are reproduced (lightly elided) so the claims are self-checkable.

**(a) The at-most-once delivery loop** — `pkg/comm/nats.go`, the fetch loop body. The ack on
the success path precedes the detached goroutine; the malformed-message path acks-and-drops:

```go
// nats.go ~:257–267 (the consumer fetch loop)
var pm protocol.Message
if err := json.Unmarshal(msg.Data(), &pm); err != nil {
    // Malformed message: ack it to prevent a redelivery loop.
    _ = msg.Ack()                              // :264-equivalent — poison dropped, no DLQ, no alert
    fmt.Fprintf(os.Stderr, "comm.NATSBus: unmarshal msg for agentID=%q: %v\n", agentID, err)
    continue
}
_ = msg.Ack()                                  // :264 — ACK BEFORE the handler runs
// Deliver in a goroutine to match InMemoryBus async semantics
// and avoid blocking the fetch loop.
go safeHandle(ctx, pm, h)                       // :267 — handler runs AFTER the ack, detached
```

Consequences (all in Section 5): at-most-once (crash after ack ⇒ lost message); no ordering
barrier (goroutine-per-message); poison messages silently dropped; the plan's "ack after
outputs published / nak on error / 30s redelivery / DLQ" cannot be layered on top of this
without rewriting `Subscribe`.

**(b) The single-replica file-backed stream** — `pkg/comm/nats.go` `StreamConfig` /
`ConsumerConfig`:

```go
// StreamConfig (~:149–153)
Subjects: []string{"bodh.>"},
MaxAge:   24 * time.Hour,
Storage:  jetstream.FileStorage,
Replicas: 1,                         // ← SPOF: one node loss ⇒ whole-fleet outage + data loss window

// ConsumerConfig (~:205–211)
Name:          durableName,
Durable:       durableName,
FilterSubject: subj,                 // bodh.{tenant}.{agentID}
AckPolicy:     jetstream.AckExplicitPolicy,
DeliverPolicy: jetstream.DeliverNewPolicy,   // ← gap messages before consumer-creation are never delivered
// NOTE: no AckWait, no MaxDeliver, no DeadLetter subject configured
```

**(c) The supervisor state machine + cost-rework loop** — `agents/supervisor.go`. This is the
clinical workflow Decision C keeps as an agent; it is also why the eval-mirror cannot infer
terminality from outside (finalize emits nothing) and why concurrent delivery corrupts state:

```go
// supervisor.go (elided) — DiagnosticSupervisor is a zero-value struct{}; routing is by msg.Type
switch msg.Type {
case "case_state":         out = forwardTo("questioner", ...)
case "questions_complete": out = forwardTo("test_planner", ...)
case "tests_complete":     out = forwardTo("diagnostician", ...)
case "reasoning_verified": out = forwardTo("diagnostician", ...)   // non-terminal re-diagnose
case "tests_revise":       out = reviseTests(state)                // :152–160 — DROP costliest test,
                                                                   //   delete its result, recompute
                                                                   //   TotalCostUSD, re-encode, re-submit
case "diagnosis":          finalize(state); out = nil             // :259 — TERMINAL, emits NOTHING
                           // :253 metrics := eval.ScoreCase(state, ...)  ← local eval import (NOT zero-dep)
default:                   out = nil                               // :107 — unknown type dropped
}
```

The terminal signal in the monolith is `len(out)==0 && agentID=="diagnostic_supervisor" &&
msg.Type=="diagnosis"` (orchestrator.go:340) — a predicate over the supervisor's *output*, which
a standalone NATS subscriber on the supervisor's *input* subject cannot evaluate.

**(d) The audit minimum-necessary allowlist** — the one chokepoint Decision-driven distribution
endangers. The orchestrator records audit via `audit.FromMessage()`, which copies only a 4-key
allowlist and **never** `msg.Content`:

```go
// conceptually: pickStrings() over msg.Metadata, allowlist only
fields := pickStrings(msg.Metadata, "patient_id", "case_id", "trace_id", "tenant_id")
rec.Record(ctx, audit.Event{Kind: ..., Fields: fields})   // msg.Content (raw PHI) is never copied
```

When this moves into a per-agent middleware replicated across 24 binaries, nothing structurally
prevents one binary's audit middleware from serializing `msg.Content` (chief complaint / HPI /
diagnosis narrative) — the HIPAA regression in Section 5.3.

---

## 4. The architecture

### 4.1 The 3-tier target

```
┌──────────────┐   HTTP    ┌─────────────────────┐   NATS   ┌──────────────────────────┐
│  Frontend    │──────────▶│   API Service        │─────────▶│  NATS JetStream          │
│  (React SPA) │           │   cmd/api/           │          │                          │
│  Static/CDN  │           │  - HTTP endpoints    │          │  bodh.{t}.intake         │
│              │           │  - Auth (JWT+OPA)    │          │  bodh.{t}.diagnostic_... │
│              │           │  - Postgres reads    │          │  bodh.{t}.diagnostician  │
│              │           │  - HITL management   │          │  bodh.{t}.human_review   │
│              │           │  - Publish to NATS   │          │  bodh.{t}.broadcast      │
│              │◀── SSE ───│  - broadcast sub     │          │  ... 29 subjects total   │
│              │           │                      │          │                          │
│              │           │  ❌ NO agents/        │          └────────────┬─────────────┘
│              │           │  ❌ NO pkg/agent       │                       │
│              │           │  ❌ NO orchestration  │           Each agent = own binary/process:
└──────────────┘           └─────────────────────┘           ┌──────────┐ ┌──────────────┐
                                                              │  intake  │ │  supervisor  │
                                                              └──────────┘ └──────────────┘
                                                              ┌──────────┐ ┌──────────────┐
                                                              │  dx-llm  │ │  cdm-planner │
                                                              │ (+ HPA,  │ │ (+ Postgres) │
                                                              │  +secret)│ └──────────────┘
                                                              └──────────┘ ┌──────────────┐
                                                              ┌──────────┐ │ eval-mirror  │
                                                              │human-rev │ │ (NATS sub →  │
                                                              │(+ PG)    │ │  eval.Store) │
                                                              └──────────┘ └──────────────┘
```

### 4.2 Binary inventory

**~21 per-agent binaries** (`agents/cmd/agents/<name>/main.go`):
intake, supervisor, questioner, test-planner, cost-guardian, diagnostician (`--provider`),
reasoning-verifier, cdm-planner, tcm-coordinator, panel-manager, pre-visit-prep, inbox-triage
(`--provider`), refill-manager, prior-auth, human-review, engagement, audio-transcriber,
rpm-monitor, readmission-tracker, trial-screener, specialists (`--specialty`: cardiologist /
pharmacist / neurologist / nephrologist / oncologist / ophthalmologist / pulmonologist /
infectious_disease; radiologist pending).

**Plus**: `cmd/api` (backend), `cmd/eval-mirror` (standalone eval subscriber), and the
retained `cmd/care` monolith. The plan states "24 binaries total" (§3) but "23 images" /
"22 Dockerfiles" elsewhere (§10/§14) — **inventory not yet pinned; reconcile before CI.**

### 4.3 Module split & the circular-dependency reasoning

```
github.com/PratikDhanave/bodh           ← MAIN MODULE (SDK + backend)
  go.mod                                  module github.com/PratikDhanave/bodh
  pkg/protocol  ← wire type (Message), zero internal-bodh imports (safe leaf)
  pkg/comm      ← Bus interface + NATSBus (//go:build nats; stub otherwise)
  pkg/agent     ← Agent, Environment; Message = protocol.Message (alias)  [agents module only]
  pkg/medical   ← CaseState, PatientStore (NEW)
  pkg/hitl      ← ReviewStore (NEW) + BuildResumeMessage (NEW)
  pkg/tasks     ← TaskStore (NEW)
  pkg/runtime   ← NEW agent message loop
  pkg/gateway   ← NEW pipeline builder
  cmd/api, cmd/eval-mirror   ← NEW (zero agent imports)

github.com/PratikDhanave/bodh/agents    ← AGENTS MODULE (separate go.mod, requires bodh)
  agents/*.go                             ← all agent impls (fields → interfaces)
  cmd/agents/<name>/main.go               ← 21 per-agent binaries
  cmd/care/                               ← MONOLITH (moved here to break the cycle)

go.work  →  use .  +  use ./agents       (local-dev workspace; commit go.work + go.work.sum)
```

**Why cmd/care moves:** cmd/care imports both `agents` and many `pkg/*`. If it stayed in the
main module while `agents/` became a module requiring `bodh`, the main module would need to
`require bodh/agents`, while `bodh/agents` already requires `bodh` → **circular module
dependency** (forbidden). Relocating cmd/care into the agents module makes the graph one-way
(`bodh/agents → bodh`).

> **Blocking caveat (verified):** This is only achievable after `pkg/shadow` (main-module
> library, shadow.go:352–365) and the test `pkg/orchestration/integration_test.go` — both of
> which import concrete agents — are refactored. Until then the "strictly one-way" graph does
> **not** hold and the split won't compile. The boundary also does **not** stop `cmd/api`
> from importing `pkg/agent` (same module, just a type alias); that half needs a lint guard.

### 4.4 `pkg/runtime` message loop (intended)

`runtime.Run(agent)` is the shared loop every per-agent binary calls. **Intended** behavior
(plan §5) — note the **gap** vs current `pkg/comm`:

```
Subscribe on bodh.{tenant}.{agent.ID()}:
  1. pipeline.Pre(ctx, msg)      → governance + audit + safety; deny ⇒ stop (already audited)
  2. out, err := a.HandleMessage(ctx, msg, env)
       err ⇒ pipeline.RecordError; nak → redelivery
  3. pipeline.Post(ctx, out)     → output validation
  4. for m := range out { bus.Publish(ctx, m) }
  5. Ack — AFTER all outputs published
+ health probe :8080, graceful SIGTERM shutdown, 60s message timeout, panic recovery
```

**Reality check:** step 5 ("ack after publish") + "nak on error" are **impossible on top of
the current `bus.Subscribe()`**, which owns the ack and **acks first** (nats.go:264) then runs
the handler in a detached goroutine (:267). `runtime.Run` can never control ack ordering
without rewriting `NATSBus.Subscribe` to surface the raw JetStream message (pull/fetch model,
ack-last, join the handler goroutine). This rewrite is not in any phase.

### 4.5 Gateway pipeline

`pkg/gateway` mirrors PlatformGRC's `create_gateway_pipeline()`. Middleware interface:
`Process(ctx, msg) (msg, allow bool, err)`. Available middleware: `audit` (wraps `pkg/audit`),
`policy_gate` (wraps `pkg/governance` CompositePolicy), `phi_guard` (wraps `pkg/phi`),
`circuit_breaker`, `content_safety`, `output_validator`. **Platform guarantees** (claimed
non-negotiable, but configured by env var): Audit(minimal) every message; 60s message timeout;
panic recovery; `sha256(caseID)[:8]` PHI logging rule. Config via `RUNTIME_AUDIT_LEVEL`,
`RUNTIME_POLICY_MAX_CONTENT`, `RUNTIME_CIRCUIT_BREAKER`, etc. (See Section 5 HIPAA lens: making
these env-configurable across 21 manifests is itself a regression — a dropped var silently
relaxes governance/audit on one agent.)

### 4.6 End-to-end message flow (including HITL pause/resume)

```
User → React SPA → POST /api/cases → cmd/api
  └ protocol.NewMessage("api","intake",RoleUser,"presentation",...) → NATS bodh.{t}.intake
     [intake] → bodh.{t}.rpm_monitor → [rpm-monitor] → bodh.{t}.diagnostic_supervisor
       [supervisor] switch msg.Type:
         "case_state"        → questioner    → (back to supervisor)
         "questions_complete"→ test_planner  → cost_guardian → (back to supervisor)
         "tests_complete"    → diagnostician (LLM) → reasoning_verifier
              reasoning_verifier:
                ├ HITL needed → bodh.{t}.human_review
                │   [human-review] HumanReviewAgent.HandleMessage → ReviewStore.Submit() → Postgres
                │   cmd/api broadcast sub → SSE → clinician UI
                │   clinician Approve → POST /api/reviews/{id}/decide → cmd/api:
                │      (a) reviewStore.Decide(...)              [Postgres commit]
                │      (b) hitl.BuildResumeMessage(r,...)       [pure data, NO agents import]
                │      (c) bus.Publish(resume → r.NextTo)       [NATS]
                │      → bodh.{t}.diagnostic_supervisor  (resume)
                └ pass        → bodh.{t}.diagnostic_supervisor  ("diagnosis", terminal)
                                 [supervisor] finalize, emits nothing
                                 [eval-mirror] ScoreCase → eval.Store (Postgres)

cmd/api (broadcast subscriber) ← every message mirrored → SSE → user sees diagnosis
```

**Two correctness hazards baked into this flow** (detailed in Section 5): (1) the HITL resume
splits a previously-atomic in-process operation into a **non-transactional saga** — a crash
between (a) and (c) leaves the review "decided" (so `Decide` retry returns `ok=false`) but
never resumed → permanently stuck case; (2) the eval-mirror cannot observe the supervisor's
`len(out)==0` from outside, so terminality must become an **explicit** marker, not inferred
from `msg.Type=="diagnosis"`.

---

## 5. Adversarial review (simulated)

Three reviewer lenses. Each challenge cites code; "how the plan responds" states whether the
plan addresses it (mostly: not addressed / contradicted).

### 5.1 Operational / SRE lens

| Challenge | Severity | How the plan responds |
|---|---|---|
| **Reliability foundation contradicted by code.** nats.go acks at :264 **before** the handler runs in a detached goroutine (:267). Crash/err after ack ⇒ message gone — the exact InMemoryBus failure NATS supposedly fixes. No `MaxDeliver`, `DeadLetter`, nak-on-error, or backoff anywhere. The "24-binary decoupling buys durability" justification rests on a loop that must be rewritten first — not in any phase. | high | §0.6 asserts "no NATSBus code changes needed"; §13 describes ack-after-publish as if implemented. **Factually wrong** against nats.go:264–267. The §5 runtime pseudocode shows the right pattern but sits on `bus.Subscribe()`, which acks first — `runtime.Run` cannot control ack ordering without rewriting `Subscribe`. Gap unacknowledged, no LOC budgeted. |
| **NATS is a hard SPOF.** `Replicas: 1` + FileStorage (nats.go:152–153). One NATS pod loss takes down delivery for **all** 21 agents — decoupling raises NATS-dependence from ~zero (monolith InMemoryBus, no broker) to 21, with no HA. `MaxAge 24h` + R1 means a node failure can lose in-flight + recently-persisted messages. | high | Open Q1 only covers message *size*, never availability/replication. §14 lists a `nats/` dir but no StatefulSet, clustering (R3), PDB, or anti-affinity. §13 sells JetStream durability while the stream is R1. No RPO/RTO stated. |
| **Broadcast mirror doubles broker load on the SPOF.** Every non-broadcast message is published twice (target subject + `broadcast`, nats.go:306–334). With FileStorage R1 that's 2× write IOPS + 2× storage on one disk; ~21 agents × ~10 hops/case ⇒ ~20 durable disk writes/case for the happy path through one un-replicated store. | medium | Treated as free (§7/§9). No capacity model, no IOPS/storage estimate, no alternative (wildcard consumer / dedicated observability stream). |
| **Rollout choreography for 24 binaries hand-waved.** `protocol.Message` is JSON with **no version field**; it's the shared contract across 21 agents + cmd/api. Any `CaseState`/`ReviewRequest` shape change needs an ordered rollout across up to 24 independently-versioned deployments. An old consumer that can't unmarshal a new message **acks-and-drops** it (nats.go:258). | high | No schema versioning, compatibility policy, rollout ordering, or canary/blue-green. §13's "durable consumer persists messages" doesn't protect against producer/consumer schema skew. |
| **Config drift across per-agent env vars.** All config moves into env vars across 24 manifests with no central schema/validation. "Audit(minimal) cannot be disabled" is contradicted by making `RUNTIME_AUDIT_LEVEL` an env var — a typo/omission on one of 21 manifests silently degrades the HIPAA audit guarantee on that agent; nothing fails closed. | high | §4 frames env-only config as a 12-factor virtue. No schema, no startup validation, no fail-closed, no single source of truth. |
| **Observability across 24 services asserted, not designed.** The `sha256(caseID)[:8]` PHI rule must now be enforced in 21 binaries' logging + cmd/api + eval-mirror; the plan names a "logger wrapper" but no package. No trace-context field on `protocol.Message`, so a case's ~10-hop path can't be reconstructed as one trace. | high | §6 cites a "runtime's logger wrapper" with no package/code; no dashboards/SLOs/alerts; the Google/MS comparison tables substitute for an observability design. |
| **Cost vs monolith never quantified.** Diagnostician 500m/512Mi; every pure agent 50m/64Mi; 21 agents at `minReplicas:1` ⇒ ~1.5 vCPU / ~2Gi of *idle* requests 24/7, + NATS + Postgres + cmd/api + eval-mirror. The monolith is ONE pod. | medium | Not addressed; §15 "Hybrid" mode gestures at it with no grouping design, cost model, or break-even volume. |
| **Local-dev/CI friction underestimated & self-contradicting.** Open Q2 pushes devs to the monolith — but the decoupled stack is the **only** thing exercising NATS routing, ack semantics, HITL-over-NATS, eval-mirror (the highest-risk code). NATS is **not** in the existing compose stack (postgres/ollama/care/prometheus/grafana/tempo/loki only), so the "production-ready" bus has never run in dev/CI; every CI run uses InMemoryBus via the monolith. | high | Open Q2 resolves heaviness by steering to the monolith, structurally guaranteeing the decoupled path is under-exercised. Phase 5 integration tests aren't wired into default `make test` (no tags). NATS-absent-from-compose not noted. |
| **HITL split-brain (writer ↔ writer).** cmd/api (`Decide`) and human-review (`Submit`) both write `review_requests`; cmd/api publishes the resume after `Decide`, but `Decide` + NATS publish are **not** one transaction — commit-then-publish-fails (broker down) ⇒ decided-but-never-resumed, no outbox/retry. | medium | §12 describes only the happy path; §13's "compute from message content" idempotency doesn't cover HITL state in Postgres. |
| **"True binary isolation" weaker operationally.** All agents share one `go.mod` requiring `bodh`, so a CVE in any shared dep (pgx, nats.go, otel, LLM SDK) rebuilds/redeploys **all 24** images. Per-agent secret isolation is also achievable with one binary + per-workload ServiceAccounts + scoped Secrets. | low | §4/§11 argue binary isolation but don't address the inverse 24× patch fan-out, and overstate that secret isolation requires per-agent binaries. |
| **LOC/effort estimates understate operational deliverables.** Phase 2 "15 min"; Phase 4 "~2200 lines of YAML." The repo has exactly ONE production k8s deployment today. None of NATS HA, the ack rewrite, schema versioning, trace propagation, config validation, per-agent dashboards/SLOs, rollout ordering, or CI for 24 images appears as a line item. | medium | §17 totals ~5,900 LOC and frames the dangerous parts as cheap. The hard SRE work is absent from the phase tables. |

**Reviewer questions (operational):**
1. Which ack model is true *today* — and where is the NATSBus rewrite (ack-after-work,
   nak-on-error, `MaxDeliver`, DLQ) budgeted in Phases 0–5?
2. What are the RPO/RTO with `Replicas:1` FileStorage 24h, and where is the NATS HA design
   (R3, StatefulSet, PDB, anti-affinity, quorum sizing)? How is a NATS SPOF an improvement
   over "one panic kills the process"?
3. What is the message schema-versioning + producer/consumer compatibility policy, and the
   rollout ordering when `CaseState`/`ReviewRequest` change shape? Why isn't acks-and-drop
   (nats.go:258) silent data loss during a rollout?
4. `RUNTIME_AUDIT_LEVEL` is an env var yet Audit(minimal) is "non-negotiable." Is startup
   fail-closed if a manifest omits/mistypes it? Where is the central config schema/validation?
5. Where is the distributed-tracing design (trace-context on `protocol.Message`), and which
   shared package enforces `sha256(caseID)[:8]` in all 21 binaries?
6. What is the aggregate always-on cost vs the single monolith pod, and the case/day
   break-even? What concretely is the "Hybrid" grouping and how is it chosen?
7. How does the 24-binary NATS path get continuous coverage given NATS is absent from compose
   and Open Q2 steers devs back to the monolith?
8. Where is the transactional outbox / resume retry / reconciliation for the
   `Decide()`-then-`Publish()` gap?
9. What is the patch/rollout SLA for a shared-dependency CVE across 24 pipelines, and why
   isn't per-agent secret isolation achievable with one binary + per-workload SAs?
10. Once NATS HA, the ack rewrite, schema versioning, trace propagation, dashboards/SLOs,
    rollout choreography, and 24-image CI are added, what does the estimate become?

### 5.2 Distributed-systems correctness lens

| Challenge | Severity | How the plan responds |
|---|---|---|
| **At-most-once, not at-least-once.** nats.go acks at :264 before dispatching the handler in a new goroutine at :267. Crash/panic/err after ack ⇒ message lost — exactly the failure the plan says NATS fixes. §13's "ack AFTER outputs published" and runtime step 5 describe a system that doesn't exist. | high | Actively contradicted. §0.6 calls nats.go production-ready, "no changes needed," while the code does the opposite. Decoupled mode is **less** reliable than the monolith for any side-effecting agent until nats.go is rewritten (pull/fetch, ack-last, join the goroutine). |
| **No DLQ, no redelivery config.** ConsumerConfig (:205–211) sets only Name/Durable/FilterSubject/AckExplicit/DeliverNew — no `AckWait`, `MaxDeliver`, dead-letter subject, or advisory subscription. With ack-before-handle there's nothing to redeliver. §13's "30s redelivery" and "DLQ" rows are fiction. | high | Treated as existing infra. Net-new: `MaxDeliver` + terminal subject + DLQ consumer + poison-classification. Unscoped. Silently dropping a poison diagnosis is a safety incident. |
| **DeliverNew loses gap messages.** `DeliverNewPolicy` (:210): a durable consumer only receives messages published after it first existed. Unsubscribe **deletes** the durable (:285). On cold start / first deploy / consumer-deleted, gap messages are never delivered. + `Replicas:1` (no replication) ⇒ node loss loses the stream. | high | Not addressed. Production needs DeliverAll/ByStartSequence, R3+, and a consumer lifecycle that doesn't delete the durable on graceful shutdown. |
| **Eval-mirror terminality is unobservable from outside.** Monolith signal = `len(out)==0 && agentID=="diagnostic_supervisor" && msg.Type=="diagnosis"` (orchestrator.go:340); supervisor.finalize() emits nothing (supervisor.go:259). A standalone subscriber sees only the inbound diagnosis message and **cannot** observe `len(out)==0`. Worse, the inbound diagnosis is also the message on the non-terminal verifier→re-diagnose path → the mirror scores non-terminal cases. | high | Hand-waved "watches for terminal diagnosis messages" (line 650). Terminality must be made **explicit** (e.g. supervisor emits `case_finalized` to a dedicated subject) — an agent code change the plan claims to avoid ("zero eval dependency, zero agent change"). |
| **Per-subject ordering destroyed.** JetStream delivers in sequence, but :267 dispatches every message via `go safeHandle(...)` — a fresh goroutine, no ordering barrier. The supervisor is a fan-in on one subject (questions_complete, tests_complete, reasoning_verified, tests_revise, diagnosis); concurrent goroutines let a later message be handled before an earlier one. | high | Never discussed. With ack-before-handle + goroutine-per-message, two messages for one case run concurrently and `reviseTests` (supervisor.go:152–160) can interleave / lost-update. Needs `MaxAckPending=1` per consumer or partition-by-case_id — which fights the "HPA to 10 replicas" goal. |
| **CaseState passed by value & mutated mid-pipeline → stale-read / lost-update.** `reviseTests` decodes state, drops the costliest test, deletes its result, recomputes `TotalCostUSD`, re-encodes (supervisor.go:152–160). The whole case travels in `Content`. Under redelivery / concurrent specialist consult, an older snapshot overwrites newer fields. No version, vector clock, or merge. | high | Open Q1 dismisses this as a **size** concern only ("~5KB, no issue"). The real problem is **staleness + concurrent mutation** under at-least-once. "Pure function of message content" is exactly what makes a stale snapshot deterministically produce a stale output. A state-pointer + optimistic-concurrency pattern is arguably **required**, not optional. |
| **"Pure function" ≠ idempotent for side-effecting agents.** `tasks.Queue.Create` (inbox_triage, prior_auth), `hitl.Queue.Submit` (human_review + gating agents), `engagement.Notifier` (SMS/email), and every LLM call are external WRITE side effects with no dedup key. Crash-after-side-effect-before-ack ⇒ duplicate tasks/reviews/SMS/double-billed LLM. `Create()` appends unconditionally. | high | Blanket "current agents are idempotent" is **false** for all of Category C + engagement/LLM. At-least-once requires an idempotency key (e.g. `msg.ID`) enforced at each store (upsert) + notifier dedup + LLM request-cache. Unscoped, spanning tasks/hitl/persistence/engagement. |
| **HITL resume: lost-resume / double-resume across cmd/api↔NATS.** `Decide()` (Postgres) then `BuildResumeMessage()` then `Publish()` are not atomic. Crash between (a) and (c) ⇒ decided-but-never-published, **permanently stuck** (`Decide` retry returns `ok=false`). Two concurrent/double-clicked Approves can both pass `Decide` if the adapter's status-check+flip isn't transactionally guarded ⇒ two resume messages. | high | The plan **splits** a single in-process op into a non-transactional saga with no outbox, no idempotent resume (NextTo agent has no dedup), no stuck-case detector. The "Decide is single-shot" property that makes lossless-on-reject safe becomes a permanent-stall liability. |
| **Non-atomic double-publish (target + broadcast).** nats.go:318 (target) and :329 (broadcast) are independent. Crash between ⇒ the agent gets the message but the SSE/audit-observability stream doesn't (or vice-versa); cmd/api's SSE feed diverges from what agents processed — an audit-completeness gap. | medium | Relies on the broadcast mirror for SSE + observability; never notes the two publishes can partially fail. |
| **Poison messages acked-and-dropped silently.** nats.go:257–262: on unmarshal failure, `Ack()` + continue. A schema-incompatible `CaseState` (guaranteed under independent per-agent rollouts) is silently swallowed — case stalls, no DLQ, no alert, no audit error. | medium | Independent deploy makes schema skew normal, not exceptional. No version field, no compatibility policy, no DLQ. A correctness regression specific to decoupling. |
| **Pre-existing routing bug becomes fatal.** cardiologist.go:118 emits to `"supervisor"`, but the supervisor's ID is `"diagnostic_supervisor"` (supervisor.go:57). In-process this is a silent no-op; in NATS it publishes to `bodh.{t}.supervisor` with no consumer → retained 24h then discarded. Any specialist→supervisor consult is silently broken. | medium | Not detected. The §0.2 audit lists 29 IDs but never validates every emitted `msg.To` ∈ registered IDs. A static routing-graph check must be a build gate. |
| **Partial pipeline failure: no completion guarantee.** If a mid-pipeline agent crashes (message lost per the ack bug) or errors (outputs dropped), the case never reaches finalize. No per-case watchdog, timeout-to-terminal, or stuck-case detector; the SSE stream waits forever. | medium | Monolith has the same gap, but decoupling multiplies the surface (24 processes + network + NATS). The 60s message timeout bounds one `HandleMessage`, not the case. |
| **Delivery guarantee never declared.** §13 borrows the favorable properties of both at-least-once (durability) and the unbuilt ack-after-work model, while the code is at-most-once. | medium | Must pick **at-least-once + consumer idempotency** (the only tractable NATS choice) and enumerate the per-store idempotency mechanism. NATS `Nats-Msg-Id` dedup covers producer retries, not consumer-side reprocessing after a crash. |

**Reviewer questions (distributed-systems):**
1. Reconcile §0.6 "production-ready, zero changes" with ack-at-:264-before-dispatch-at-:267.
   Show the exact nats.go diff for ack-after-publish (fetch loop holds the message in-flight,
   joins the handler goroutine).
2. Where are `AckWait`, `MaxDeliver`, and the dead-letter subject configured? What classifies
   poison vs transient, and what inspects/replays the DLQ for a stalled clinical case?
3. Exactly which message, subject, and discriminating field will the eval-mirror key on, given
   the supervisor emits nothing on finalize? Does that require a new terminal message
   (contradicting "zero eval dependency, zero agent change")?
4. Under at-least-once + HPA-scaled concurrent consumers, what prevents `CaseState` lost
   updates — a version field, per-case serialization (`MaxAckPending=1` partitioned by
   case_id), or the deferred state-pointer + optimistic-concurrency pattern?
5. How do you prevent duplicate review requests, nurse tasks, patient SMS, and double-billed
   LLM calls on crash-after-side-effect-before-ack?
6. Where is the transactional outbox for `Decide()`-then-`Publish()`, and how does the NextTo
   agent dedup a twice-published resume?
7. What is the schema-compatibility policy, where does the version live, and why is a poison
   `CaseState` dropped instead of DLQ'd + alerted?
8. What build-time check validates every `NewMessage(to=...)` target is a registered consumer
   subject (the cardiologist→"supervisor" bug)?
9. What is the declared end-to-end delivery guarantee, and where is it documented?
10. What bounds case-level liveness — a per-case deadline, saga-state table, or reconciliation
    sweep that drives stuck cases to terminal and notifies the waiting SSE client?

### 5.3 Healthcare-compliance / HIPAA lens

| Challenge | Severity | How the plan responds |
|---|---|---|
| **Audit PHI guarantee depends on ONE chokepoint; decoupling distributes it without preserving the invariant.** Today "`msg.Content` is never copied into audit" is enforced once, in the orchestrator via `audit.FromMessage()` + `pickStrings()` 4-key allowlist (patient_id, case_id, trace_id, tenant_id). Moving audit into a per-agent middleware replicated across 24 binaries (`gateway.BuildFromEnv()`) means one team adding a custom `audit.go` that logs `msg.Content` (chief complaint / HPI / diagnosis narrative) silently breaks minimum-necessary (45 CFR 164.502(b)) for that agent — and the single-review safety net is gone. | high | §6 lists "audit | wraps pkg/audit" and "Audit(minimal) cannot be disabled" — about *presence*, not field-selection. No statement that audit middleware MUST use `FromMessage()`/`pickStrings()` and is forbidden from serializing `Content`; no compile-time / cross-binary contract test; no mechanism preventing a per-agent override. **Highest-severity compliance regression.** |
| **`sha256(caseID)[:8]` already inconsistent today; decoupling multiplies 24×.** CLAUDE.md mandates `[:8]`, but `pkg/observability/otlp.go` and `laminar.go` use `[:16]`. The cited enforcement seam `pkg/phi.LoggerWrapper` is a best-effort **regex** redactor (no name detection, MRN only with literal "MRN" prefix, **no caseID hashing at all**). No code computes `sha256(caseID)[:8]`; the rule lives only in prose. | high | §6 claims the runtime logger wrapper enforces it but points at no implementation; doesn't reconcile `[:16]` vs `[:8]`; provides no hashing function or failing test. Aspirational. |
| **The "production-ready" NATS bus is compiled out, ships PHI in cleartext, no transport security/auth.** nats.go is `//go:build nats`; without the tag, the stub no-ops. The plan's Dockerfiles build with **no `-tags nats`** → binaries link the stub and silently fail to communicate. `Publish()` json-marshals the **full** `protocol.Message`, and `Content` (raw PHI) goes on the wire. nats.go connects via `nats.Connect(url)` — **no TLS, no creds/nkey/token**. Decoupling moves PHI from in-process memory onto a plaintext, unauthenticated network bus → 164.312(e)(1) transmission-security failure. | high | §0.6 "no NATSBus code changes needed, already supports this" — false on three counts (build tag omitted, no TLS/auth, PHI-in-Content cleartext). Open Q1 treats Content-on-the-wire as a non-issue (size only). The NetworkPolicy YAML restricts egress targets but adds no encryption/mutual auth. |
| **Kill switch becomes non-functional when distributed.** `AgentMeshGate` holds an **in-process, in-memory** `KillSwitchRegistry`. cmd/care's `POST /admin/kill-switch` flips it and (one process) halts the fleet. Decoupled: each of 24 binaries constructs its **own** private registry; hitting `/admin/kill-switch` on cmd/api flips only the API process — the 24 running agents never see it and keep processing. The immediate-halt patient-safety control is silently lost. | high | §7 lists `AgentMeshGate (kill switch)` as a cmd/api import as if one API-side gate controls the fleet. Never recognizes the registry is per-process in-memory; proposes no shared store (Postgres/NATS KV/etcd) or broadcast halt. |
| **Fail-closed governance downgraded to fail-OPEN-per-agent; wired safety policies dropped.** Today the orchestrator is the single authorization point (policy error ⇒ DecisionDeny). cmd/care wires a `CompositePolicy` (MaxContentLength{65536}, RequireCaseID, MaxDiagnosticCost, ResearchDisclaimer, AgentMeshGate incl. prompt-injection scan). Decoupled: each agent's `BuildFromEnv()` must independently reconstruct this exact stack. (1) The §6 table lists a generic "policy_gate" with no enumerated policies/order/config; (2) a missing/mistyped env var on one Deployment silently relaxes governance for that agent; (3) an agent built without the prompt-injection scanner ingests unscanned content. | high | §6 promises per-agent governance but doesn't specify the policy SET/ORDER/config parity; no central distribution; no test asserting decoupled policy == monolith policy; never names RequireCaseID, MaxDiagnosticCost, or the prompt-injection scanner. |
| **Right-to-explanation becomes structurally incomplete.** `GET /explain/{case_id}` reconstructs the chain by scanning ONE recorder's `List()` for a case_id and silently drops unknown `audit.Kind`s. With audit written by 24 fail-open middlewares (recorder logs-and-continues on write failure) + HITL decision events split between cmd/api and the human-review binary, there is no unified ordered store the handler reads; any lagging/dropping forwarder ⇒ the regulatory artifact silently understates the automated processing. | high | §8/Phase 0 move audit emission into per-agent middleware and split HITL recording across two writers, but never specify a unified ordered complete audit store, nor how completeness is guaranteed under the fail-open recorder. Task 0.9 only changes `explainHandler`'s store type. |
| **BAA surface multiplied, never analyzed.** §1/§11 tout "different clouds, regions, GPUs." Every LLM agent (9 variants) sends `msg.Content` (raw clinical narrative) to an external provider. The monolith is one process / one BAA scope; decoupling spreads PHI-bearing egress across many binaries in potentially different clouds/regions — each a distinct BAA-requiring flow (Anthropic, OpenAI, Ollama-host, NATS operator, per-region cloud). | medium | "Different clouds, regions" appears only as an unqualified benefit. No BAA inventory, no third-party data-flow map, no acknowledgment that distributing LLM egress widens the covered-entity surface. |
| **Data residency impossible as specified.** Open Q3 mandates ONE shared binary per agent across ALL tenants (NATS subject routing + RLS). Residency is per-tenant/per-jurisdiction: an EU tenant's PHI must not be processed in a US region. One shared diagnostician deployment in one region processes **every** tenant's Content in memory and sends it to an LLM. RLS governs Postgres rows, not where the binary executes. | medium | Open Q3 optimizes container count and asserts "NATS + RLS already handles this." Residency never mentioned as a constraint. |
| **DB-level minimum-necessary ignored.** §11 celebrates that intake can't read the diagnostician's API key — true. But 6 state agents get a `DATABASE_URL` to the full PatientRegistry/TaskQueue/ReviewQueue. A compromised cdm-planner (needs only Get/Upsert on patients) with full `DATABASE_URL` can read the entire tenant patient registry — broader than one API key. The model conflates secret isolation (achieved) with data-access minimization (not addressed). | medium | §11 shows cdm-planner gets `DATABASE_URL` "separate secret" but no per-agent least-privilege DB role/grant scoped to its method surface. |
| **Fail-open recorder × 24 + redelivery duplicates audit.** The recorder is fail-open by design (never halt care on audit downtime) — defensible with one recorder, but 24 raise the silent-loss probability, weakening 164.312(b) (which right-to-explanation and the 21 USC 360j(o)(1) CDS criterion-4 carve-out depend on). Audit middleware runs **before** `HandleMessage`; crash-after-audit-before-ack ⇒ reprocessing ⇒ duplicate `KindMessage` rows. `explain.go` dedups only HumanReviews (by timestamp), so chains show phantom duplicate hops. | medium | Inherits the fail-open recorder without acknowledging N=24 multiplies silent loss; §13's at-least-once + "audit before HandleMessage" creates duplicates with no dedup story. No idempotency key for audit rows. |
| **Broadcast→SSE fans raw PHI to browsers.** `Publish` mirrors every message to `broadcast` with full serialized `Message`; cmd/api subscribes and `sseHub.Send(case_id, msg)` forwards the whole message (Content included) to clients keyed only by case_id — no per-recipient authorization that the client is entitled to that case's PHI, no redaction. A minimum-necessary + access-control (164.312(a)/(e)) gap baked into the reference flow. | medium | §7 shows the broadcast→SSE code forwarding the full msg with no field stripping and no per-recipient check beyond case_id keying. |
| **Dormant stricter policies (42 CFR Part 2 etc.) not wired, and decoupling makes uniform wiring harder.** `pkg/governance` ships `part2_policy.go` (42 CFR Part 2 substance-use — stricter than HIPAA), `break_glass_policy.go`, `smart_scope_policy.go`, `rate_limit_policy.go`, `redact_narrative_policy.go` — none in cmd/care's `NewComposite`. Decoupling scatters policy assembly into 24 `BuildFromEnv()` calls, removing the single composite seam where these would be switched on once. | medium | §§6,16 map middleware names to files but never enumerate the policy set or mention Part 2 / break-glass / narrative-redaction, and provide no central registry. |

**Reviewer questions (HIPAA):**
1. Where exactly is `sha256(caseID)[:8]` computed in code today? (CLAUDE.md says `[:8]`;
   otlp.go/laminar.go use `[:16]`; `pkg/phi` never hashes caseID.) What shared mandatory
   primitive guarantees identical hashing across 24 binaries, and what test fails on a raw or
   `[:16]` caseID on a span?
2. When audit becomes a per-agent middleware across 24 binaries, what compile-time /
   cross-binary contract test PROVES no agent's audit row ever serializes `msg.Content`, and
   what prevents a custom `audit.go` from logging the chief complaint?
3. Was the `-tags nats` omission from the Dockerfiles intentional? Once corrected: where are
   TLS, NATS client auth (nkey/creds/token), and encryption-in-transit for `msg.Content`
   (164.312(e)(1))? Today `nats.Connect(url)` is plaintext, no auth.
4. The kill switch is an in-memory per-process `KillSwitchRegistry`. In 24 separate binaries,
   how does an operator's halt reach the running agents? What shared/broadcast halt mechanism
   replaces it, and what is the max emergency-halt propagation latency?
5. Enumerate the EXACT policy set/order/config each agent's `BuildFromEnv()` enforces, and show
   it equals cmd/care's `NewComposite(...)`. If a Deployment omits `RUNTIME_POLICY_*` or
   RequireCaseID, what prevents that agent from processing caseless, un-auditable, un-governed
   messages, and what test asserts decoupled policy == monolith policy?
6. With audit written by 24 fail-open middlewares + HITL events split across cmd/api and the
   human-review binary, what guarantees `GET /explain/{case_id}` is COMPLETE and correctly
   ordered? How do you detect/prevent missing hops when a forwarder lags/drops?
7. Audit runs before `HandleMessage` and NATS redelivers at-least-once ⇒ duplicate
   `KindMessage` rows; `explain.go` dedups only HumanReviews. What idempotency key dedups audit
   events so the artifact doesn't show phantom duplicate steps?
8. Provide the BAA inventory + PHI data-flow map: which of the 9 LLM agents send `Content` to
   which third party, plus the NATS operator and any cross-cloud/region placement. How many NEW
   BAA boundaries vs the monolith?
9. How is per-tenant data residency enforced when one shared diagnostician deployment in one
   region processes every tenant's Content and sends it to an LLM (RLS controls rows, not
   execution location), without the per-tenant topology Open Q3 rejected?
10. Will each state agent get a least-privilege Postgres role scoped to exactly its
    tables/columns/operations, or a shared full-PHI credential? How is minimum-necessary
    (164.502(b)) enforced at the DB grant level?
11. What per-recipient authorization confirms an SSE client is entitled to a case's PHI, and is
    `Content` stripped/redacted before broadcast fan-out?
12. How will 42 CFR Part 2 substance-use redaction + break-glass logging be uniformly applied
    to every agent that could touch protected data, when there is no longer a central composite
    to register a policy once?

---

## 6. Open questions for YOU (the external model)

Synthesized, deduped, and sharpened from all three lenses. Answer as many as you can; the
top five are the highest-value.

1. **Delivery semantics & the ack rewrite.** The code is at-most-once (ack at nats.go:264
   *before* a detached handler goroutine at :267); the plan promises at-least-once durability
   (ack-after-publish, nak-on-error, 30s redelivery, DLQ) it never builds. What is the correct
   target guarantee, and what is the minimal, concrete `NATSBus.Subscribe` rewrite (pull/fetch,
   in-flight hold, ack-last, join goroutine, `AckWait`/`MaxDeliver`/DLQ) — and is it tractable
   as a Phase-0 prerequisite rather than an unscoped assumption?

2. **Consumer idempotency for side-effecting agents.** Under at-least-once, `tasks.Create`,
   `hitl.Submit`, `engagement` SMS, and LLM calls all double on redelivery. Design the
   idempotency-key scheme (keyed on `msg.ID`? on a content hash?) and where it is enforced
   (DB upsert, notifier dedup, LLM request-cache) with the least code spread across
   tasks/hitl/persistence/engagement.

3. **CaseState consistency under concurrency.** With value-passed, mid-pipeline-mutated
   `CaseState`, at-least-once redelivery, and HPA-scaled concurrent consumers, is per-case
   serialization (`MaxAckPending=1` partitioned by case_id) sufficient, or is a state-pointer +
   optimistic-concurrency (`state_id` + Postgres + version column) **required**? How do you
   keep horizontal scaling while guaranteeing no lost updates to a single case?

4. **HITL resume as a transactional saga.** `Decide()` (Postgres commit) then `Publish()`
   (NATS) are not atomic, and `Decide` is single-shot — a crash between them permanently stalls
   the case. Specify the outbox / retry / reconciliation design, plus how the resumed (NextTo)
   agent dedups a twice-published resume, without re-importing agent code into the backend.

5. **Distributed audit completeness vs. the single-chokepoint invariant.** How do you preserve,
   across 24 fail-open per-agent audit middlewares, both (a) the "`Content` never enters the
   audit log" minimum-necessary guarantee (enforced today by one `FromMessage()`+`pickStrings()`
   path) and (b) the completeness + ordering that `GET /explain/{case_id}` needs — given the
   recorder is fail-open and audit runs before redeliverable `HandleMessage`? Propose the
   compile-time/contract test and the unified ordered store.

6. **Distributed kill switch.** Replace the in-memory per-process `KillSwitchRegistry` with a
   fleet-wide emergency-halt mechanism (NATS KV? Postgres flag + watch? a halt subject every
   runtime subscribes to?) and state the worst-case propagation latency a patient-safety control
   can tolerate.

7. **PHI on the wire + transport security.** Given `msg.Content` is raw PHI and NATS today is
   plaintext + unauthenticated, what is the minimal correct posture (mTLS + per-agent creds?
   payload encryption? a state-pointer so Content never transits?) to satisfy 164.312(e)(1)
   without re-architecting the message model?

8. **Module split feasibility ordering.** Given `pkg/shadow` (main module) instantiates concrete
   agents (verified shadow.go:352–365), order the prerequisite refactors so the
   `bodh/agents → bodh` graph actually becomes one-way and compiles, and decide whether
   `pkg/shadow` should move, invert to an injected interface, or be deleted.

9. **Schema versioning across independent rollouts.** With JSON `protocol.Message`/`CaseState`
   and 24 independently-deployed binaries, define the version field, the producer/consumer
   compatibility policy, the rollout ordering, and the DLQ-not-drop behavior for incompatible
   messages — so a shape change is not silent data loss.

10. **NATS HA & the SPOF.** Specify the production NATS topology (R3 cluster, StatefulSet, PDB,
    anti-affinity, quorum, `DeliverAll`/`ByStartSequence`, consumer-lifecycle that doesn't delete
    durables on shutdown) and the resulting RPO/RTO — then judge whether per-agent fault
    isolation is a net availability win over the monolith once NATS is the common-mode dependency.

11. **Governance parity & fail-closed config.** How do you guarantee every agent's
    `BuildFromEnv()` reconstructs the exact monolith `CompositePolicy` (set, order, config) and
    fails **closed** (not open) if an env var is missing — including switching on the currently
    dormant 42 CFR Part 2 / break-glass / narrative-redaction policies uniformly?

12. **Is per-agent the right topology at this scale?** Given the near-zero marginal binary cost
    but the real 24× ops fan-out, the un-quantified idle-cost floor, and that affinity grouping
    is the cheap-to-merge fallback — would you ship pure per-agent now, or start at
    grouped-by-affinity (Decision A, Option C) and split later? Justify against the
    HIPAA-isolation argument.

---

## 7. Implementation phases

Compact view of the plan's phased rollout (plan §17). **The "Δ vs reality" column flags work
the phase tables omit but Section 3/5 prove is required** — an external reviewer should weight
estimates accordingly.

| Phase | Scope | Key tasks | Plan estimate | Δ vs reality (gaps from this review) |
|---|---|---|---|---|
| **0. Interface extraction** | Inside existing packages; `cmd/care` unchanged; `make test` green after each task | `PatientStore`/`TaskStore`/`ReviewStore` interfaces; `BuildResumeMessage`; change 9 agent struct fields to interfaces; `TaskStoreAdapter` + `ReviewStoreAdapter`; tasks/reviews migrations; `explainHandler` → `ReviewStore` | ~445 LOC, zero behavior change | **Must also** include inbox_triage_llm.go + `NewInboxTriage` param (§3.2); refactor `pkg/shadow` + orchestration integration test before any split (§3.4); the NATSBus ack rewrite belongs here, not "assumed done." |
| **1. Core packages** | New `pkg/` | `pkg/runtime` (Run, ParseConfig, health); `pkg/gateway` (middleware + builder + 5 middlewares); `pkg/graduation` (stage, Wilson) | ~700 LOC | Gateway must encode **policy parity + fail-closed** + the PHI-hash primitive + the `Content`-never-in-audit contract test; runtime needs trace-context propagation. |
| **2. Module split** | `agents/go.mod`, `go.work` + `go.work.sum`, move `cmd/care`, verify workspace test | git mv + tidy | ~15 min | **Undercount.** Also move cmd/bench/caredemo/demo; add a `GOWORK=off` CI lane per module; `pkg/agent`-in-backend still needs a lint guard. |
| **3. Binaries** | `agents/cmd/agents/*/main.go` (21); `cmd/api`; `cmd/eval-mirror` | per-agent mains; backend (zero agent imports); eval subscriber | ~980 LOC | cmd/api needs the HITL outbox/saga; eval-mirror needs an **explicit** terminal contract (supervisor change); a build-time routing-graph check. |
| **4. Containers & orchestration** | Dockerfiles, compose, k8s | 22 Dockerfiles; decoupled compose; k8s for agents + API + NATS | ~2200 LOC (mostly YAML) | Dockerfiles must build with `-tags nats` + add the NATS dep; needs NATS **HA** (R3/StatefulSet/PDB), TLS+auth, per-agent least-privilege DB roles, per-agent dashboards/SLOs/alerts; NATS must be added to compose. |
| **5. Integration tests** | testcontainers | multi-container flow; HITL across containers; eval-mirror e2e | ~550 LOC | Must cover redelivery double-execution, schema-skew/poison-message DLQ, stuck-case liveness, cross-tenant RLS denial, and decoupled-policy==monolith-policy. Wire into CI (not default `make test`). |

**Plan grand total: ~5,900 LOC.** Realistic total is materially higher once the NATSBus
rewrite, NATS HA, schema versioning, trace propagation, config validation/fail-closed,
distributed kill switch, per-agent dashboards/SLOs, rollout choreography, and 24-image CI are
counted — none of which appear in the phase tables.

---

*End of document. The fastest path to high-value feedback is Section 6 (open questions) backed
by the Section 3 evidence and the Section 5 adversarial tables.*

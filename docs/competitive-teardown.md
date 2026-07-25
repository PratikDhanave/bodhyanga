# Architecture Teardown — Parachute · Wedge · Lattice vs. Bodh

> **Companion to `docs/competitive-landscape.md`.** Where the landscape doc surveys ~30 YC
> healthcare-AI companies, this doc goes deep on the three whose products most directly touch
> Bodh's safety-substrate thesis (clinical-AI audit / drift / governance), and compares them
> dimension-by-dimension against Bodh's *actual code*.
>
> **Method asymmetry (deliberate and important):** the three competitors publish no code, no
> docs, no whitepapers — every mechanism below is marketing copy, tagged `[CLAIMED]` /
> `[VERIFIABLE]` / `[UNKNOWN]`. Bodh's side is anchored to `file:line` on
> `feat/agent-decoupling-phase0`. This compares *verified code* against *asserted capability* —
> which is the point of a teardown, but also means a competitor tagged `[UNKNOWN]` may have more
> (or less) than they let on. Snapshot: July 2026.

## Core divergence, one line

> The three competitors **observe and report on AI they mostly don't own, out of the decision
> path. Bodh gates and can halt AI it does own, inside the decision path.** Different layer of the
> stack — closer to complementary than competitive.

---

## The four, at a glance

| | **Parachute** | **Wedge** | **Lattice** | **Bodh** |
|---|---|---|---|---|
| YC / stage | S25, team 2, ~$2.3M | S25, team 2, ~$500K | S26, solo, undisclosed | Open reference arch (Go) |
| One-liner | Vet/deploy/monitor vendor clinical AI | "Trust layer" + own back-office RCM agents | Read-only imaging-AI drift monitor | MARA clinical agents + enforcement primitives |
| Owns clinical models? | No | No (own agents are RCM) | No | **Yes** (its own MARA panel) |
| In the decision path? | No (observes) | No (observes) | No (explicitly read-only) | **Yes** (fail-closed admission) |
| Gates/halts actions? | No | No | No | **Yes** (HITL gate + kill switches) |

---

## Dimension-by-dimension

### 1. Audit ("immutable")
- **Parachute** — `[CLAIMED]` "immutable audit trail hospitals hand to regulators"; **zero disclosed
  mechanism** (no hash-chain / WORM / signing). Integration logos (ServiceNow, OneTrust, Purview)
  imply a records/GRC layer. Unit of record is the *model lifecycle*, not per-patient inference.
- **Wedge** — `[CLAIMED]` "audit-trail management"; no schema, no immutability guarantee disclosed.
- **Lattice** — `[VERIFIABLE]` cryptographically **signed daily PDF**, "evidence packets with
  chain-of-custody," verifiable "three forwards deep" with no Lattice infra required.
- **Bodh** — **real SHA-256 hash chain**: `ChainedEvent{Event, PrevHash, Hash, Seq}`, `Hash =
  SHA256(prevHash + "\n" + json(Event))` (`pkg/audit/immutable.go:151`); `VerifyChain` re-walks with
  constant-time `hmac.Equal`. PHI-minimal Event schema (IDs/kinds/verdicts, never `msg.Content`).
  **Caveat:** `ImmutableRecorder` is wired into **no server** — `cmd/care` defaults to `InMemory`
  (lost on restart); the chain runs only in tests + `cmd/audit-verify`. Audit path is fail-open by
  design. *(This is the ADR-0008 gap.)*

> **Read:** Bodh has the strongest audit *primitive* in the set and the weakest *deployed* audit.
> Two competitors claim "immutable" with no mechanism; Lattice actually signs; Bodh actually
> hash-chains but doesn't turn it on.

### 2. Drift / monitoring
- **Parachute** — `[CLAIMED]` real-time accuracy/drift/bias alerts; **no statistic named**;
  out-of-band observation of vendor telemetry (MLflow-style ingest).
- **Wedge** — `[CLAIMED]` "anomaly / hallucination detection"; no drift methodology; likely
  deployment/uptime monitoring (publishes an uptime status page).
- **Lattice** — `[VERIFIABLE]` **PSI** (Population Stability Index) on model confidence
  distributions + a "silent-update detector" to catch vendors swapping models; daily batch, not
  real-time. Ground truth via one-click radiologist thumbs-up/down rollup (model-level aggregate).
- **Bodh** — `cmd/drift-monitor` is a **threshold delta, no significance test** (drop > threshold →
  warning; > 2× → critical), a CI/offline gate not a service. `pkg/shadow` is a **real dual-run
  shadow mode** (drives the actual orchestrator, compares AI diagnosis to clinician), but
  concordance is **case-insensitive exact-string match** and confidence is synthetic (1/len).
  Ledger append-only but not hash-chained.

> **Read:** Lattice has the most rigorous *disclosed* drift method (named PSI). Bodh's shadow
> harness is architecturally real but its concordance metric is brittle — a genuine quality gap.

### 3. Runtime enforcement / governance
- **Parachute** — observe only; **explicitly assumes no production liability**; "not a certification
  body." Emits alerts; the hospital acts.
- **Wedge** — all "monitor / oversee" verbs; **no deny-path evidence**. Best-supported reading:
  GRC/observability dashboard, not an in-path policy engine.
- **Lattice** — **read-only by charter** ("if Lattice stops, care continues"); its own ADRs
  "separate what is enforced in code from what is procedure" — governance is evidence + procedure.
- **Bodh** — **fail-closed `CompositePolicy`** (first deny short-circuits; child error → deny).
  **Live in `cmd/care`:** `RequireCaseID`, `MaxDiagnosticCost`, `ResearchDisclaimer`,
  `MaxContentLength`, and `AgentMeshGate` — **global + per-agent kill switches**, ring enforcement,
  prompt-injection scan, wired to a kill-switch HTTP endpoint (`cmd/care/main.go:70-77,338`).
  **Caveat:** advanced policies (`OPAPolicy`/Rego, `Part2ConsentPolicy`, `SMARTScopePolicy`,
  `RateLimitPolicy`, `RedactNarrativePolicy`) exist but are **library-only, wired into no binary**;
  the 42 CFR Part 2 consent gate is fail-*open* for non-SUD traffic by design.

> **Read:** Bodh is the only one of the four with a real in-path enforcement boundary that can
> *deny/halt* — and it's partly live (kill switches, base policies) and partly on the shelf (OPA,
> Part 2, SMART scopes). The competitors don't enforce at all, by design.

### 4. Autonomy promotion
- **Parachute / Wedge / Lattice** — **none.** No concept of an agent earning more autonomy;
  Lattice's entire thesis is "watch, never act."
- **Bodh** — **Wilson-score-gated graduation ladder** `development → shadow → copilot → autopilot`
  (`pkg/graduation`). `WilsonLowerBound(successes, total, z)`; `DefaultCriteria{MinObservations:100,
  MinWilsonLower:0.95}`; the **autopilot fence** (`AllowAutopilot`, default false) is checked
  **before** the statistical gate — promoting *into* autopilot requires explicit authorization AND
  clearing the Wilson bar (`graduation.go:90`; enforced by `fence_test.go`). *(ADR-0012.)*
  **Caveat:** a substrate library with **no production caller yet**.

> **Read:** This is genuine whitespace. Not one competitor has autonomy-promotion machinery.
> Bodh's ladder is the only artifact in the landscape that answers "how does AI safely *earn* the
> right to act." If Bodh productizes one thing, this is the candidate.

### 5. Human-in-the-loop action gate
- **Parachute** — alerts; humans act out-of-band. No execution gate.
- **Wedge** — not described.
- **Lattice** — thumbs-up/down is a *sentiment/ground-truth* signal, not a gate.
- **Bodh** — **fail-closed gate**: a gating agent wraps its payload as a `review_request` to
  `human_review` and `HumanReviewAgent.HandleMessage` returns `nil, nil` — **zero downstream
  messages**, the case freezes until a human decides; approve republishes, reject/expiry emits
  nothing ("lossless on reject") (`agents/human_review.go:194`). **Caveats:** the gate is **opt-in
  per agent** (`HITL:true` set at construction for reasoning_verifier, cdm_planner, tcm_coordinator,
  panel_manager, refill_manager, prior_auth, readmission_tracker) — not platform-enforced; and the
  queue is an **in-memory map** (restart loses pending clinical reviews; a JSON checkpointer exists
  but is unused, Postgres path unwired).

> **Read:** Bodh is the only one with a true blocking gate — and its honest limits are that gating
> is a per-agent opt-in, not a platform invariant, and the queue isn't durable.

### 6. Decoupling substrate (gateway / runtime)
- **Competitors** — n/a (monolithic SaaS or services).
- **Bodh** — `pkg/gateway.Pipeline` reuses governance-evaluate + audit-record as a **reusable
  admission-control pipeline** so a decoupled per-agent binary enforces the identical boundary as
  the monolith; `pkg/runtime` reassembles the orchestrator's per-message closure around ONE agent
  plus `/healthz` + `/readyz`. **Caveat:** documented fidelity gap — the monolith's eval-mirror
  (case-status for `GET /explain/{case_id}`) is not reproduced in the per-agent runtime.

---

## Shipped vs. designed — the honest ledger

| Capability | Competitor best | Bodh: designed | Bodh: actually running in `cmd/care` |
|---|---|---|---|
| Immutable audit | Lattice (signed PDFs) | SHA-256 hash chain | ❌ in-memory default; chain unwired |
| Drift statistic | Lattice (PSI) | threshold + shadow dual-run | ⚠️ CLI gate; concordance = string match |
| In-path enforcement | none | fail-closed composite + OPA/Part2/SMART | ⚠️ base policies + kill switches only |
| Autonomy ladder | none | Wilson gate + autopilot fence | ❌ library, no caller |
| HITL action gate | none | fail-closed freeze-until-human | ⚠️ live but opt-in + in-memory queue |

## Strategic conclusion (feeds ADR-0006)

The competitors have **deployed products with undisclosed / shallow internals**; Bodh has **deep,
inspectable internals with shallow deployment**. The market signal from Parachute / Wedge / Lattice
raising money is that hospitals will pay for a *deployed* trust layer. Bodh's moat is architectural
correctness (real hash-chain, real fail-closed enforcement, the only autonomy ladder in the field);
its debt is **wiring** — immutable recorder → `cmd/care`, durable HITL queue, a graduation caller,
and lighting up the shelved OPA/Part2/SMART policies. Closing that wiring gap is precisely what
converts a reference architecture into the product these three are being funded to build.

---

### Sources
Competitor claims: YC company/launch pages, Launch HN (Parachute item 44952246), company sites
(parachute-ai.com, wedge.health, latticehealthai.com), and press as cited in
`docs/competitive-landscape.md`. Bodh facts verified against `feat/agent-decoupling-phase0`:
`pkg/audit/{immutable,recorder}.go`, `pkg/shadow/shadow.go`, `pkg/eval/drift/drift.go`,
`pkg/governance/{policy,agentmesh_gate,*_policy}.go`, `pkg/graduation/{graduation,wilson}.go`,
`agents/human_review.go`, `pkg/hitl/queue.go`, `pkg/gateway/pipeline.go`, `pkg/runtime/`,
`cmd/care/main.go`.

**Caveat:** competitor mechanisms are marketing-level (`[UNKNOWN]` at implementation); Bodh's
shipped-vs-designed gaps are as noted (ADR-0006/0008/0012). Verify before external citation.

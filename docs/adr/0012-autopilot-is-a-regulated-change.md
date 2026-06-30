# ADR 0012: Autonomous clinical operation (autopilot) is a regulated change, fenced

**Status:** Accepted

**Date:** 2026-06-30

**Deciders:** project owner (Pratik Dhanave)

> Authored from the principal-engineer architecture review (2026). Claims are
> grounded in the codebase with `file:line` references that can be checked
> directly. See docs/adr/README.md for how this set relates to ADRs 0001–0005.

---

## Context

Bodh's agent autonomy lifecycle is modeled as a four-rung ladder in `pkg/graduation/stage.go:30-38`: `development → shadow → copilot → autopilot`. The package doc (`pkg/graduation/stage.go:1-21`) defines what each rung means operationally: shadow has ZERO patient impact (it runs in parallel and only records concordance, see `pkg/shadow/shadow.go:1-6`); copilot suggests and "a human approves every action (the HITL gate is mandatory at this stage)" (`stage.go:11-13`); autopilot "acts autonomously within its governed scope" (`stage.go:14`). Autopilot is therefore the single rung where AI clinical decisions reach a patient without a human in the loop — the most regulatorily fraught state in the system.

Promotion is evidence-gated, not time-gated. `pkg/graduation/graduation.go:67-111` (`Evaluate`) is a pure, deterministic function that promotes at most one rung when the Wilson-score lower bound of the agent's success rate clears a threshold over a minimum number of observations. The choice of the Wilson LOWER bound (`pkg/graduation/wilson.go:24-51`) is deliberate: it penalizes small samples, so 5/5=1.0 yields a bound well under 1.0 while 950/1000 yields a bound near its rate (`wilson.go:14-19`). The default bar is a 0.95 Wilson lower bound over ≥100 observations at 95% confidence (`graduation.go:33-35`).

The crux: that statistical machinery answers "is this agent ACCURATE?" — it does not and cannot answer "is this agent legally cleared to act on patients without a human?" Under software-as-a-medical-device (SaMD) regulation, turning on autonomous clinical decisioning is a categorical change in the device's intended use and risk classification, requiring regulatory and clinical sign-off — not a number crossing a line. Conflating the two would let a purely statistical pipeline silently promote an agent into unsupervised patient-facing operation.

The implemented fence lives in `Criteria.AllowAutopilot` (`graduation.go:18-26`), default `false`, and is enforced in `Evaluate` at `graduation.go:90-95` — critically BEFORE the `MinObservations` and `MinWilsonLower` checks (`graduation.go:96-104`), so the audit reason makes the policy nature unmistakable rather than reading like a statistical near-miss. `DefaultCriteria()` deliberately leaves `AllowAutopilot` unset/false (`graduation.go:29-35`), so the out-of-the-box configuration can promote an agent all the way to copilot but never into autopilot. The fence is autopilot-specific: shadow→copilot promotion is unaffected (proven by `pkg/graduation/fence_test.go:46-51`), and even a flawless 1000/1000 record holds at copilot under the default (proven by `fence_test.go:11-22`); the gate opens only with explicit `AllowAutopilot=true` AND the statistical bar still met (`fence_test.go:26-41`).

Scope note grounded in the tree: as of this branch (`feat/agent-decoupling-phase0`), `pkg/graduation` is a Phase 1 library with no production callers — a repo-wide grep for `graduation.Evaluate`, `AllowAutopilot`, `StageAutopilot`, and `graduation.Criteria` outside the package itself returns nothing. The runtime halt counterpart referenced in the ladder doc (`stage.go:18-20`, "operator kill via pkg/governance/agentmesh") does exist as `pkg/governance/agentmesh_gate.go` (global + per-agent kill switches, `agentmesh_gate.go:121,144,162-178`), but it is a separate admission-time mechanism, not a caller of the graduation gate. This ADR documents the design intent of the fence so that the eventual caller cannot wire promotion to a bare statistical signal.

## Decision drivers

- Patient safety: autopilot is the only rung that reaches a patient without a human (stage.go:14); a wrong promotion here has direct clinical consequence, unlike shadow (zero impact) or copilot (HITL-gated)
- Regulatory reality: enabling autonomous clinical decisioning is a SaMD change of intended use requiring regulatory + clinical sign-off, not a statistical event (graduation.go:21-25)
- Separation of concerns: statistical accuracy (Wilson gate) and legal/clinical authorization are orthogonal questions that must not be collapsed into one threshold
- Auditability: every promotion decision must carry a PHI-free, human-readable reason (Decision.Reason, graduation.go:50-57); the fence reason must read as a policy block, not a statistical miss
- Fail-safe default: the default configuration must never grant autonomy on its own (DefaultCriteria leaves AllowAutopilot=false, graduation.go:29-35; Evaluate holds on invalid input, graduation.go:64-66)
- Minimal blast radius: the control must fence ONLY autopilot and leave the lower, lower-risk promotions fully automated (fence_test.go:46-51)

## Options considered

### Bare statistical gate — Wilson bound alone governs every promotion, including into autopilot (no AllowAutopilot field)

- **Pros:** Simplest possible model: one deterministic, auditable function (Evaluate) governs the entire ladder uniformly. No extra configuration surface, no second source of truth. The Wilson lower bound already enforces both high accuracy and adequate sample size (wilson.go:14-19), which feels like a rigorous bar.
- **Cons:** Categorically wrong for the autopilot rung: it asserts that being statistically accurate is sufficient to operate autonomously on patients, which is false under SaMD regulation. A misconfigured or adversarial caller could flip an agent into unsupervised clinical operation with zero human authorization. There is no artifact tying the most consequential state transition in the system to a documented clinical/regulatory sign-off — an audit/compliance gap, not just a code gap.

### Explicit per-promotion authorization fence: Criteria.AllowAutopilot (default false), checked before the statistical gates (IMPLEMENTED)

- **Pros:** Encodes the true semantics: the Wilson gate is necessary but NOT sufficient for autonomy (graduation.go:84-95). Autonomy becomes an explicit, deliberate, audited decision a human must set. Default-false means the statistical machinery can never grant autonomy on its own (graduation.go:29-35), and Evaluate fails safe on bad input. The check precedes MinObservations/MinWilsonLower so the audit reason is unambiguously a policy block citing 'authorization' (graduation.go:90-95, asserted by fence_test.go:19-21). Blast radius is minimal — shadow→copilot is untouched (fence_test.go:46-51). The function stays pure and deterministic, so it remains trivial to unit-test and audit. Cost is one boolean field and one branch.
- **Cons:** The fence is only as strong as the discipline around setting the flag: it is a boolean in a Criteria struct, not a cryptographically or workflow-enforced authorization. Nothing in pkg/graduation verifies that AllowAutopilot=true is backed by a real regulatory/clinical sign-off record — the binding to an actual approval artifact is left to the (not-yet-written) caller (graduation.go:24-25 documents the expectation but cannot enforce it). It adds a second axis (statistical vs. authorization) the caller must understand.

### Remove autopilot from the ladder entirely — cap the system at copilot (HITL always mandatory)

- **Pros:** Eliminates the regulatory risk at its root: if no autonomous rung exists, no agent can ever act on a patient without a human. Simplest possible safety story; nothing to mis-set. Matches a defensible product stance that clinical AI should always keep a human in the loop.
- **Cons:** Forecloses a capability the architecture was explicitly built toward (the ladder names autopilot as its terminal rung, stage.go:34,38; Next() is designed to walk to it, stage.go:56-62). Pushes the autonomy question out of the codebase rather than modeling it — when autopilot is eventually needed, the design work resurfaces with less structure than the fenced model already provides. Throws away the ability to represent and audit a properly authorized autonomous deployment.

### External policy engine: route the copilot→autopilot transition through pkg/governance (OPA/Rego) instead of an in-struct flag

- **Pros:** Centralizes authorization with the rest of Bodh's policy machinery (medical_policy.go, agentmesh kill switches, opa_policy.go) rather than a local boolean. Could bind promotion to richer, externally-managed policy (signed approvals, role checks, break-glass) and reuse the existing admission/audit pipeline.
- **Cons:** Significantly heavier: couples the pure, deterministic graduation.Evaluate to a policy runtime, an evaluation context, and I/O — losing the property that the same inputs always yield the same Decision (graduation.go:60-66) and making the hot-path gate harder to test and reason about. Premature given that graduation has no production caller yet (no external references in the tree). The in-struct fence does not preclude this later: a caller can compute AllowAutopilot FROM a policy decision, getting the best of both. Over-engineering for the current phase.

## Decision

Adopt the explicit per-promotion authorization fence (Option 2), as implemented: `Criteria.AllowAutopilot` defaults to `false` and `Evaluate` rejects any copilot→autopilot promotion lacking it, checking the fence BEFORE the statistical gates (`pkg/graduation/graduation.go:90-95`). This is the right call because it encodes the one fact the statistical machinery cannot express: clearing the Wilson gate proves an agent is accurate, not that it is cleared to operate autonomously on patients, which under SaMD is a regulated change of intended use. The default-false posture (`graduation.go:29-35`) guarantees the statistical pipeline can never, on its own, promote an agent into unsupervised clinical operation, while the fence's narrow scope (`fence_test.go:46-51`) keeps the lower, lower-risk promotions fully automated. The implementation is minimal (one field, one branch), keeps `Evaluate` pure and auditable, and emits a reason that reads unmistakably as a policy block rather than a statistical near-miss.

One thing the future owner must decide before any agent reaches autopilot, because the code cannot decide it: WHAT may set `AllowAutopilot=true`, and what evidence it must be bound to. The flag is a boolean, not a proof of sign-off — `pkg/graduation` documents the expectation that a documented regulatory + clinical authorization backs it (`graduation.go:24-25`) but cannot enforce that binding. When the first caller is written (there is none today), it MUST source `AllowAutopilot` from a recorded, auditable authorization artifact (and ideally route it through `pkg/governance`), never hard-code it true. The fence is the mechanism; the authorization process and its audit trail are the policy the owner still has to define.

## Consequences

### Positive

- The most consequential state transition in the system — into unsupervised patient-facing operation — is now a deliberate, named, default-off control rather than an emergent property of a statistics function (graduation.go:18-26, 90-95).
- Fail-safe by construction: DefaultCriteria() can promote up to copilot but never to autopilot (graduation.go:29-35), and Evaluate holds on terminal/invalid input (graduation.go:75-83), so misconfiguration stays put rather than advancing.
- Audit clarity: because the fence is checked before the statistical gates, a blocked promotion records a PHI-free reason citing 'authorization' and 'regulated change, not a statistical threshold' (graduation.go:91-93), distinguishable in the audit log from an agent that merely lacked evidence — proven by fence_test.go:19-21.
- Minimal blast radius and zero regression for lower rungs: shadow→copilot and development→shadow remain fully automated and statistics-gated (fence_test.go:46-51).
- Evaluate stays pure and deterministic, so the safety-critical gate remains trivially unit-testable and safe on the hot path (graduation.go:60-66).

### Negative

- Adds a second conceptual axis to promotion (statistical evidence vs. authorization); any future caller must understand that the Wilson gate is necessary but not sufficient for the top rung.
- The fence is a boolean in a struct, not an enforced approval workflow — pkg/graduation cannot itself verify that AllowAutopilot=true reflects a real sign-off; correctness depends on caller discipline (graduation.go:24-25).
- Slightly more API surface and one more branch in the hot-path function; the special-casing of StageAutopilot means the ladder is no longer governed by a single uniform rule.

### Risks

- Flag-flip risk: a future caller could set AllowAutopilot=true unconditionally or hard-code it, silently defeating the fence. Mitigation: the eventual caller must bind the flag to a recorded authorization artifact (and route it through pkg/governance), and a test should assert no caller passes a literal true without an approval reference.
- No production enforcement yet: graduation has zero callers in the current tree, so the fence is currently a design guarantee, not a runtime one — the protection only materializes when promotion is wired in. Until then, the ADR plus fence_test.go are the only things preventing a future PR from re-collapsing autonomy into a statistical threshold.
- Decoupling drift: the ladder doc points at pkg/governance/agentmesh for the demotion/kill path (stage.go:18-20), but that registry (agentmesh_gate.go) and the graduation gate are not yet connected; an autopilot agent's promotion gate and its runtime kill switch live in different packages, and the integration that ties promotion authorization to operator override remains to be built.
- Authorization-record gap: there is currently no schema or store for the regulatory/clinical sign-off that AllowAutopilot is supposed to represent, so today an 'authorized' promotion would not be reconstructable from data — the owner must define that artifact before autopilot is enabled.

## References

- pkg/graduation/graduation.go:18
- pkg/graduation/graduation.go:29
- pkg/graduation/graduation.go:84
- pkg/graduation/graduation.go:90
- pkg/graduation/graduation.go:96
- pkg/graduation/stage.go:1
- pkg/graduation/stage.go:30
- pkg/graduation/stage.go:56
- pkg/graduation/wilson.go:14
- pkg/graduation/wilson.go:24
- pkg/graduation/fence_test.go:11
- pkg/graduation/fence_test.go:26
- pkg/graduation/fence_test.go:46
- pkg/shadow/shadow.go:1
- pkg/governance/agentmesh_gate.go:121

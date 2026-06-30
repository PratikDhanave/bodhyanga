# ADR 0010: Evals as the deploy gate; unify eval/shadow/graduation into one performance-and-safety pipeline

**Status:** Needs-Decision

**Date:** 2026-06-30

**Deciders:** project owner (Pratik Dhanave)

> Authored from the principal-engineer architecture review (2026). Claims are
> grounded in the codebase with `file:line` references that can be checked
> directly. See docs/adr/README.md for how this set relates to ADRs 0001–0005.

---

## Context

For a clinical-AI system the eval harness is the product development loop, yet in Bodh that loop is spread across at least six packages that do not share types, scales, thresholds, persistence, or a CI entry point. None of them gates a deploy.

What exists today:

1. Single-case LLM judge — `pkg/eval/judge.go`. A 4-dimension clinical rubric (correctness/specificity/safety/differential_quality), `PassThreshold = 0.6` (judge.go:77), `BatchGrade` with a 4-worker pool (judge.go:203). Pass is derived from score, not from the model's self-reported pass (judge.go:159-163).
2. SD-Bench fixture runner — `cmd/bench/main.go`. Drives fixtures through the full MARA pipeline per provider, scores via `eval.ScoreCase` (cmd/bench/main.go:350), and supports per-provider regression gating: accuracy floor with 1pp tolerance and mean-cost ceiling with 1¢ tolerance (main.go:130-133, 520-529), baselined in `data/cases/baseline.json`. That baseline currently holds ONE provider (`rule`), 7 cases, recorded 2026-05-24 — thin and stale.
3. Exact-match case metrics — `pkg/eval/metrics.go`. `ScoreCase` reduces a case to `DiagnosisMatch` via case-insensitive string equality (metrics.go:43-48) and `Success = DiagnosisMatch && UnderBudget` (metrics.go:62) — a much coarser signal than the judge's rubric.
4. Multi-turn harness — `pkg/evals/multiturn/runner.go`. Its own `RunConfig` and a SEPARATE `PassThreshold = 0.6` default (runner.go:34, 48), LLM-as-judge optional.
5. Shadow concordance — `pkg/shadow/shadow.go`. Runs the AI pipeline beside the clinician and records a `ConcordanceRate` (shadow.go:82) — a RAW proportion with NO confidence interval. `diagnosisMatch` here (shadow.go:287) re-implements the same string compare as metrics.go:43.
6. Graduation Wilson gate — `pkg/graduation`. `Evaluate` promotes one rung when the Wilson lower bound ≥ 0.95 over ≥100 observations (graduation.go:33-35, 67-110; wilson.go:24), with an explicit regulatory fence blocking autopilot without `AllowAutopilot` (graduation.go:90-95).
7. Equity stratification — `pkg/eval/equity/equity.go`. Re-implements Wilson confidence intervals independently of graduation (equity.go P95CI/Wilson) and flags `BiasAlert` at a 10pp disparity (equity.go:47).
8. A large research suite with no gate wiring — `pkg/eval/{drift,redteam,taxonomy,trace,collaborative,federated,perturbation,rag,judges}`, plus the Laminar uploader `pkg/evals/laminar/evaluate.go`.

The load-bearing problems, all verifiable:

- Graduation is dead-on-arrival. `grep` for non-test importers of `pkg/graduation` returns nothing, and `graduation.Evaluate` / `DefaultCriteria` are called from no runtime or CI path. `stage.go:10-11,32` documents the ladder `development→shadow→copilot→autopilot` and points at `pkg/shadow`, but no code feeds shadow's `ConcordanceRate` (or any eval score) into `graduation.Evaluate`. The package is a pure function with NO persistence (no Store/Save/db) — it cannot accumulate the ≥100 observations its own default demands.
- The CI deploy gate has no clinical signal. `.github/workflows/ci.yml` gates on Go coverage ≥90% (lines 37-44) and runs bench ONLY as a smoke test: `go run ./cmd/bench -timeout=5s` (line 56) WITHOUT `-fail-on-regression`. The strings shadow, evals, graduation, concordance, and fail-on-regression appear nowhere in the workflow. A prompt/model change that tanks diagnostic accuracy or safety ships green as long as unit-test coverage holds.
- Three incompatible notions of "good" with three numeric scales: judge/multiturn pass at 0.6 (eval/judge.go:77, evals/multiturn/runner.go:48); bench gates on an accuracy floor + cost ceiling (cmd/bench/main.go:130-133); graduation gates on a Wilson lower bound of 0.95 (graduation.go:34); shadow reports a bare rate; equity alerts on a 10pp gap. There is no single artifact a release manager can read to answer "is this build safe to deploy".
- Duplicated correctness logic and statistics. `diagnosisMatch` exists in both metrics.go:43 and shadow.go:287; Wilson is implemented twice (graduation/wilson.go and eval/equity). Divergence here means the same case can be "concordant" in shadow but "incorrect" in bench.
- The graduation ladder's `shadow` rung (the real-world evidence stage) is exactly the data shadow concordance produces, but the two are not connected. The richest pre-deploy safety signal Bodh has (agreement with real clinicians) flows into a JSONL ledger (shadow.go:99-111) and dies there.

This ADR sits downstream of ADR-0001 (LLM abstraction): if Bodh is on a product path serving clinicians, clinical validation — not SD-Bench string-match accuracy — is the bar, and the gate must enforce it automatically.

## Decision drivers

- Patient safety: a model/prompt change that degrades diagnostic accuracy or safety must be impossible to deploy silently — today it ships if Go coverage stays ≥90% (ci.yml:37-44)
- Single source of truth for 'is this build deployable': release managers should read one verdict, not reconcile a 0.6 judge pass, an accuracy floor, a 0.95 Wilson bound, and a bare concordance rate
- Make the dead graduation machinery real: pkg/graduation has zero non-test importers and no persistence; either wire it to actual evidence or stop carrying it as if it governs anything
- Eliminate divergent correctness/statistics logic: diagnosisMatch (metrics.go:43, shadow.go:287) and Wilson (graduation/wilson.go, eval/equity) must not be reimplemented per package
- Connect shadow concordance to the graduation 'shadow' rung — the highest-value real-clinician safety signal currently dead-ends in a JSONL file
- Regulatory posture: autonomy promotion is a categorical SaMD decision, not a statistical threshold (graduation.go:90-95) — any unification must preserve that fence, not let a green dashboard imply clearance
- Cost/feasibility: LLM-judge and full-pipeline bench runs cost real API tokens and minutes; a per-PR blocking gate must be scoped (small curated set) so CI stays affordable and deterministic
- Preserve the breadth of the existing research suite (drift, redteam, equity, taxonomy, rag, federated) without forcing it all onto the blocking path

## Options considered

### Option A — Status quo: keep the packages separate, gate only on coverage

- **Pros:** Zero engineering cost. The pieces already build and are unit-tested. Bench regression gating already exists (cmd/bench/main.go:520-529) and can be turned on in CI with a one-line flag change if ever wanted. Research-grade subpackages (redteam, drift, equity, taxonomy) stay free to evolve without a release-blocking contract.
- **Cons:** The core defect remains: no clinical-quality signal gates any deploy (ci.yml runs bench as a 5s smoke test without -fail-on-regression). graduation stays dead code with a 0.95 Wilson gate nothing calls and no persistence to feed it. Three incompatible 'pass' definitions and duplicated diagnosisMatch/Wilson persist. For a HIPAA-grade clinical system this is the highest-liability option: the loudest safety signal (shadow concordance with real clinicians) dies in a JSONL ledger.

### Option B — Wire the EXISTING pieces into the gate without unifying types (minimal integration)

- **Pros:** Cheap and fast. Flip CI bench to `-fail-on-regression` (the gate logic already exists, main.go:494-532), add a `cmd/shadow`/`cmd/evals` step, and refresh data/cases/baseline.json beyond the single stale `rule` row. Delivers the headline ask — evals gate the deploy — in days, not weeks. No new abstractions to design or get wrong.
- **Cons:** Does not fix the conceptual fragmentation: the gate becomes a bash AND of several independently-scaled checks (0.6 judge pass AND accuracy floor AND, maybe, a concordance rate), which is brittle and hard to reason about. graduation stays unconnected unless a separate persistence layer is added. Duplicated diagnosisMatch/Wilson remain. Baseline is per-provider point estimates with naive tolerances (1pp/1¢, main.go:130-133), not confidence-aware — noisy small fixture sets will flap. A green gate still won't mean 'clinically validated', only 'not obviously worse than a thin baseline'.

### Option C — Unify into one first-class performance-and-safety pipeline that gates every deploy and drives graduation automatically (the recommended direction)

- **Pros:** One scored artifact (a versioned EvalReport) with a single, confidence-aware verdict that CI blocks on and that feeds graduation.Evaluate directly — closing the dead-code gap (graduation has 0 importers today) and the shadow→graduation gap in one move. Collapses duplicated diagnosisMatch (metrics.go:43 / shadow.go:287) and Wilson (graduation/wilson.go / eval/equity) into shared primitives. Lets the blocking gate carry clinical dimensions (the judge's safety/correctness, shadow concordance, equity disparity) rather than string-match accuracy alone — the real bar if ADR-0001's product path holds. Subpackages (redteam, drift, taxonomy, federated) plug in as evaluators behind one interface instead of one-off binaries.
- **Cons:** Largest effort and the most design risk: defining one schema that spans single-case judging, multi-turn, shadow concordance, and stratified equity is genuinely hard, and a bad abstraction is worse than today's honest mess. Real cost/latency: a blocking LLM-judge + full-pipeline gate spends tokens and minutes on every PR — must be scoped to a small curated set with a deterministic fallback or CI becomes slow and flaky. Risk of false confidence: a single green number can imply 'clinically validated' when the dataset is small or unrepresentative; the autopilot regulatory fence (graduation.go:90-95) must be preserved so unification never auto-grants autonomy. Requires building the persistence graduation lacks so observations actually accumulate to the ≥100 its default demands.

## Decision

This is a Needs-Decision ADR; the owner (Pratik) should choose the depth, and the choice is tightly coupled to ADR-0001's product-vs-research disposition.

Framing for the decision:

If Bodh is on a product path serving clinicians (the premise of this ADR), Option A is off the table — shipping a clinical model change behind only a 90% Go-coverage gate (ci.yml:37-44) is an unacceptable safety posture. The real choice is B vs C, and they are not mutually exclusive in time.

Recommended sequencing:
- Do Option B NOW as a stop-gap, because it removes the most dangerous fact on the ground — that nothing clinical gates a deploy — for almost no cost. Concretely: enable `-fail-on-regression` in the CI bench step (ci.yml:56), rebuild `data/cases/baseline.json` across the providers actually shipped (it holds only `rule`/7-cases/2026-05-24 today), and add a non-blocking shadow + judge report artifact so the team sees clinical drift even before it blocks.
- Commit to Option C as the target state, but gate the investment on one prerequisite decision: is the curated eval dataset large and representative enough that a single blocking verdict means something? If not, building the unified pipeline first produces a confident-looking gate over a weak dataset — false assurance, which in a clinical system is worse than an honest patchwork. So the first deliverable of C is dataset curation + a shared `EvalReport` schema and shared correctness/Wilson primitives, NOT a new blocking threshold.

Two invariants the owner must hold regardless of B or C:
1. graduation must either become real (fed by persisted shadow/eval observations so its ≥100-observation, 0.95-Wilson default is reachable) or be explicitly marked aspirational — it must not sit in the tree implying it governs autonomy when no code calls it.
2. The autopilot fence (graduation.go:90-95) stays categorical. Unifying the score must never let a green pipeline auto-promote into autonomous operation; statistical pass is necessary, never sufficient, for SaMD autonomy.

What I would NOT do: jump straight to C and design the grand schema before turning on the cheap gate and before knowing the dataset is trustworthy.

## Consequences

### Positive

- A clinical-quality signal finally blocks deploys, instead of coverage alone (ci.yml:37-44) — a model/prompt regression can no longer ship green
- Option B is nearly free: the bench regression gate already exists (cmd/bench/main.go:494-532); enabling it is a flag plus a baseline refresh
- Under Option C, graduation stops being dead code (0 non-test importers today) — shadow concordance and eval scores feed graduation.Evaluate, connecting the StageShadow rung (stage.go:10-11) to the data that justifies it
- Collapsing duplicated diagnosisMatch (metrics.go:43, shadow.go:287) and Wilson (graduation/wilson.go, eval/equity) removes a class of bug where the same case scores differently in different gates
- A single versioned EvalReport gives release managers and auditors one artifact to read — valuable for HIPAA/SaMD evidence trails, not just engineering

### Negative

- A blocking LLM-judge/full-pipeline gate spends API tokens and CI minutes on every PR; without scoping to a small curated set and a deterministic fallback, CI gets slow and flaky
- Turning on -fail-on-regression against the current thin baseline (one provider, 7 cases) will flap on small-sample noise until the dataset and tolerances are hardened
- Option C is a multi-week effort with real abstraction risk: one schema spanning single-case, multi-turn, shadow, and equity is hard, and a wrong abstraction is worse than today's separate-but-honest packages
- graduation needs a persistence layer it does not have (pure function, no Store) before its ≥100-observation default is even reachable — extra scope C implies but A/B don't deliver

### Risks

- False confidence: a single green verdict over a small/unrepresentative dataset can read as 'clinically validated' when it is not — the most dangerous failure mode for a clinical gate, and the reason dataset curation must precede a hard threshold
- Regulatory conflation: if unification blurs the line between 'passed the statistical gate' and 'cleared for autonomy', the autopilot fence (graduation.go:90-95) could be undermined; the fence must remain categorical and outside the statistical pipeline
- Gate-gaming: optimizing prompts to the curated eval set rather than to patients (overfitting the benchmark) — needs a held-out/rotating set and the equity disparity check (equity.go:47) kept on the gate to catch population-level regressions accuracy alone hides
- Scope creep: pulling the whole research suite (redteam, drift, federated, taxonomy, rag) onto the blocking path would make CI brittle; these should remain reporting-only evaluators behind the interface, not release blockers

## References

- pkg/graduation/graduation.go:33
- pkg/graduation/graduation.go:67
- pkg/graduation/graduation.go:90
- pkg/graduation/wilson.go:24
- pkg/graduation/stage.go:10
- pkg/shadow/shadow.go:82
- pkg/shadow/shadow.go:99
- pkg/shadow/shadow.go:287
- pkg/eval/judge.go:77
- pkg/eval/judge.go:159
- pkg/eval/judge.go:203
- pkg/eval/metrics.go:43
- pkg/eval/metrics.go:62
- pkg/eval/equity/equity.go:47
- pkg/evals/multiturn/runner.go:34
- pkg/evals/multiturn/runner.go:48
- pkg/evals/laminar/evaluate.go:151
- cmd/bench/main.go:130
- cmd/bench/main.go:350
- cmd/bench/main.go:494
- cmd/bench/main.go:520
- cmd/shadow/main.go
- data/cases/baseline.json
- .github/workflows/ci.yml:37
- .github/workflows/ci.yml:56
- docs/adr/0001-llm-abstraction-layer.md

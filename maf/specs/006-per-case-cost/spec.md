# Feature Specification: Per-Case Cost (the Pareto cost axis)

**Feature Branch**: `feat/per-case-cost` · **Created**: 2026-07-18 · **Status**: Implemented

**Input**: "The pipeline tracks no cost anywhere, so SD-Bench (specs/005) is accuracy-only and
bodhyanga's MAI-DxO cost-aware-diagnosis thesis is unreproduced. Add a per-case virtual cost that
flows from the ordered tests through to the terminal outputs and the eval/bench surfaces."

This is the keystone that was flagged as a prerequisite by specs/005. It restores the second
axis of the Sequential Diagnosis Benchmark: **diagnostic accuracy vs. resource expenditure.**

## User Scenarios & Testing

### User Story 1 — Every ordered test has a virtual cost (P1)

As a benchmark author, I need each investigation the pipeline runs to carry a virtual USD
price, so a case's total cost reflects how much work-up it consumed.

**Acceptance**: the Lab prices each produced `TestResult` from an illustrative catalog
(troponin $45, ecg $30, cbc $25, blood culture $60, chest xray $80, bnp $55; unknown $0). A
case's cost is the sum of its run tests' prices.

### User Story 2 — Cost rides the case to every terminal surface (P1)

As a consumer of results (care API, eval, bench), I need the total cost on the
`DiagnosisResult`, the `CarePlan`, and the eval `InteractionRecord`, so any surface can report
it without re-deriving.

**Acceptance**: `routeDiagnosis` sums the workup's test costs onto `DiagnosisResult.CostUSD`;
`NewCarePlan` copies it to `CarePlan.CostUSD`; the Runner's eval recorder sets
`CaseOutcome.CostUSD`, surfaced as the `cost_usd` metric.

### User Story 3 — SD-Bench reports the Pareto (P2)

As a benchmark runner, I need accuracy AND mean cost, so I can compare engines on the Pareto
frontier, not accuracy alone.

**Acceptance**: `bench.CaseResult.CostUSD` carries each case's cost; `Summary.MeanCostUSD` is
the mean over completed cases; the table has a `COST_USD` column and the summary line reports
mean cost. `-json` includes it.

## Requirements

- **FR-001** Additive only: cost is a new field on existing types (`TestResult`,
  `DiagnosisResult`, `CarePlan`, `CaseOutcome`, `bench.CaseResult`, `Summary`) — no existing
  behavior changes; every package still compiles and all prior tests stay green.
- **FR-002** Prices are illustrative (NOTICE) — not a real fee schedule.
- **FR-003** Cost never carries PHI; it is a scalar derived from test names only.
- **FR-004** Failures (no terminal plan) have no cost and are excluded from mean cost.

## Non-goals

- A cost budget / Cost Guardian agent that caps or denies work-up (a future policy story).
- Cost-based regression gating in the baseline (the gate stays accuracy-based; the baseline
  remains accuracy-only for now — mean cost is reported, not gated).
- Per-provider cost differences (LLM token cost, etc.).

## Outcome

Implemented on `feat/per-case-cost`. `go run ./cmd/bench` now prints, e.g., ACS $75 / CAP $165
/ CHF $135 / undiff $0, mean $94 — accuracy 100% AND a cost axis. The MAI-DxO thesis is
reproduced on MAF-go.

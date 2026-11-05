# Feature Specification: SD-Bench Harness Port

**Feature Branch**: `feat/sdbench-harness`

**Created**: 2026-07-16

**Status**: Draft

**Input**: User description: "Port bodhyanga's SD-Bench regression runner (`cmd/bench`) to bodhyanga-maf:
an offline evaluation harness that loads a manifest of case fixtures, drives each through
the MAF-go slice pipeline with the rule diagnostician (fully offline, CI-friendly), scores
each case against its gold diagnosis, aggregates an accuracy report, and gates CI by
failing on regression versus a stored baseline — the bodhyanga-maf analog of `cmd/bench`, and
the only place evaluation exists on MAF-go (the framework is .NET-only for evals)."

## Context

The slice safety boundary (specs/001) is complete and green. The pipeline —
`intake → test_planner → diagnostician → care_plan` with an admission boundary on every
hop, an audit bridge, durable physician review, and a terminal-hop eval record — runs one
case at a time via `pkg/pipeline.Runner` (`cmd/slice`). Each terminal case already emits
exactly one `eval.InteractionRecord` (`pkg/evalhook`, closing bodhyanga's eval-mirror gap).

This feature adds the **fleet-level** view: run *many* fixtures in one headless pass, score
each against a gold label, and compare the aggregate against a committed baseline so CI can
fail when accuracy regresses. The **rule diagnostician runs fully offline** (no API key, no
network, no Docker), which is what lets this harness run as a plain `go test` / CI gate.

### Ground-truth findings that shape this spec (all cited from files read, not assumed)

- **No gold label in bodhyanga-maf.** `agents.PatientCase` (`pkg/agents/types.go`) has only
  `CaseID`, `Symptoms []string`, `History []string` — no gold diagnosis. `evalhook.CaseOutcome`
  (`pkg/evalhook/hook.go`) records `Diagnosis` but no gold and no accuracy. **Gold must be
  supplied by a manifest and accuracy re-derived by this harness**, exactly as bodhyanga's
  `cmd/bench` does (bodhyanga's gold lives in `data/cases/manifest.json`, not in the case files).
- **No cost data anywhere in bodhyanga-maf.** A full scan (`grep -rin cost|usd|budget|price`)
  found no cost tracking in any executor or message: tests carry no price
  (`agents.TestResult` = `{Name, Value}`), the planner orders tests without cost
  (`pkg/agents/testplanner.go`), and `DiagnosisResult`/`CarePlan` have no cost field. bodhyanga's
  entire Pareto axis (`CostUSD`, `MeanCostUSD`, per-case `max_cost_usd`, the cost-ceiling
  regression gate) **has no data source here.** This feature therefore ships an
  **accuracy-only** benchmark; cost is a documented, flagged dependency (see FR-011 and the
  Cost prerequisite). This is the single largest divergence from bodhyanga's `cmd/bench`.
- **Fixtures are not reusable as-is.** bodhyanga's `bench_*.json` / `demo_*.json` fixtures carry
  `chief_complaint`/`hpi`/`demographics`/`vitals` feeding `medical.PatientPresentation`;
  bodhyanga-maf's pipeline consumes `PatientCase{Symptoms, History}`. The vocabularies also
  differ: bodhyanga's rule engine yields Title-Case golds ("Diabetic ketoacidosis") while
  bodhyanga-maf's toy rules emit lowercase strings ("acute coronary syndrome",
  "community-acquired pneumonia", "heart failure"; `pkg/agents/diagnostician.go`). **New
  bodhyanga-maf-native fixtures + manifest are required**; bodhyanga's fixtures cannot be copied.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Run all fixtures and see accuracy at a glance (Priority: P1)

As a quality-and-evaluation lead, I need to run every case fixture through the slice
pipeline in one headless command and get a table showing, per case, the gold diagnosis, the
diagnosis the pipeline produced, and whether they matched — plus a single aggregate accuracy
figure — so I can see how the rule diagnostician performs across the whole suite without
running cases one at a time.

**Why this priority**: This is the benchmark itself. Without a batch runner that scores
against gold, there is no evaluation capability on MAF-go at all — everything else (gating,
baseline refresh) builds on top of this table.

**Independent Test**: Point the harness at a manifest of N fixtures and run it with the rule
engine and no credentials; it completes offline, prints one row per case with gold /
produced / match, and one aggregate accuracy figure that equals matches ÷ scored-cases.

**Acceptance Scenarios**:

1. **Given** a manifest listing several fixtures each with a gold diagnosis, **When** the
   harness runs with the rule engine, **Then** it drives every fixture through the pipeline,
   prints a per-case row (case id, gold, produced diagnosis, match yes/no) and an aggregate
   accuracy percentage over the scored cases.
2. **Given** the same manifest, **When** the harness is run twice, **Then** the accuracy
   figure is identical both times (the rule path is deterministic — no network, no clock
   dependence in scoring).
3. **Given** the `-json` output mode, **When** the harness runs, **Then** it emits a
   machine-readable report (per-case results + the aggregate) suitable for a dashboard or a
   diff, in addition to (or instead of) the human table.
4. **Given** a case whose produced diagnosis differs from gold only by surrounding
   whitespace or letter case, **When** it is scored, **Then** it counts as a match
   (case-insensitive, trimmed comparison — bodhyanga's `diagnosisMatch` semantics).

---

### User Story 2 - Fail CI when accuracy regresses against a baseline (Priority: P2)

As a platform maintainer, I need the harness to compare the current aggregate accuracy
against a committed baseline and exit non-zero when accuracy has dropped by more than a
small tolerance, so a code change that quietly breaks the rule diagnostician is caught by CI
before it merges.

**Why this priority**: A benchmark nobody is forced to look at rots. The regression gate is
what turns the P1 table into a guardrail. It depends on P1's aggregate figure and on a
baseline file existing.

**Independent Test**: With a committed baseline recording the current accuracy, run the
harness with the fail-on-regression gate: it exits zero when accuracy holds and exits
non-zero (with a human-readable regression line naming old vs new accuracy) when accuracy is
below baseline minus tolerance.

**Acceptance Scenarios**:

1. **Given** a baseline recording the suite's accuracy, **When** the harness runs with the
   gate enabled and accuracy is at or above baseline (within tolerance), **Then** it exits
   zero and reports no regression.
2. **Given** the same baseline, **When** accuracy has dropped by more than the tolerance,
   **Then** the harness prints a regression line stating the provider, the new accuracy, the
   baseline accuracy, and the tolerance, and exits non-zero.
3. **Given** a drop within tolerance (numerical jitter below the threshold), **When** the
   gate runs, **Then** no regression is reported (tolerance absorbs it).
4. **Given** the gate is NOT enabled (report-only mode), **When** a regression exists,
   **Then** the regression is printed for visibility but the exit code is zero.

---

### User Story 3 - Refresh the baseline after an intentional change (Priority: P3)

As a platform maintainer, after I intentionally change the rule diagnostician, the fixtures,
or the manifest in a way that legitimately moves accuracy, I need a single command to
overwrite the committed baseline with the current results so the new number becomes the
guardrail — without hand-editing JSON.

**Why this priority**: The gate (P2) is only usable if refreshing the baseline is a
deliberate, one-command, reviewable action. It depends on P1's aggregate and P2's baseline
format but is the least frequently used path.

**Independent Test**: Run the harness in update-baseline mode; the baseline file is
rewritten to the current accuracy with a recorded timestamp and provider, and a subsequent
gate run against that fresh baseline reports no regression.

**Acceptance Scenarios**:

1. **Given** a run whose accuracy differs from the committed baseline, **When** the harness
   is run in update-baseline mode, **Then** the baseline file is overwritten with the current
   accuracy, provider, case count, and a UTC timestamp, and the process exits zero.
2. **Given** a freshly written baseline, **When** the gate is run against it immediately,
   **Then** no regression is reported.
3. **Given** update-baseline mode, **When** it runs, **Then** the regression gate is not
   applied in the same invocation (the current run *is* the new baseline).

---

### Edge Cases

- **Missing gold for a case** (manifest entry has no gold diagnosis) → the case is run but
  its result is marked *unscored* and EXCLUDED from the accuracy denominator; unscored cases
  are counted and surfaced so a silently-unscored suite can't inflate accuracy. Whether an
  all-unscored suite is a hard error is a design decision recorded in plan.md.
- **A case that errors mid-run** (pipeline returns an error, the run is denied, or it does
  not reach a terminal `CarePlan`/`CaseRejected`) → the case is recorded as a *failure*, not
  a match; it is excluded from the accuracy numerator (a failed case never counts as
  correct). The report shows failed cases distinctly from scored-but-wrong cases.
- **Empty manifest** (zero cases) → the harness exits non-zero with a clear "manifest
  declares no cases" message rather than reporting 100%/0% of nothing.
- **Baseline file absent on first run** → treated as informational (not fatal): the report
  still prints, the message directs the user to update-baseline mode to create one, and the
  gate cannot fire (nothing to compare against). This mirrors bodhyanga's "missing baseline is
  informational" behavior.
- **Manifest references a missing / unparseable fixture file** → a hard load error before
  any case runs (fail fast — a broken suite must not silently shrink).
- **A case that never clears the confidence bar** → forced progression fires (the pipeline's
  existing max-rounds guard); the produced diagnosis is still scored against gold like any
  other. Forced progression is surfaced in the per-case row.
- **The physician-review gate**: the harness runs headless (no human), so it must either run
  the pipeline with review disabled or auto-approve every review, deterministically. Which
  one is a design decision recorded in plan.md; either way the run must never *pause*.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The harness MUST load a manifest that lists case fixtures, each with a stable
  id, a path to a fixture file, and a gold diagnosis; paths MUST resolve relative to the
  manifest file so the harness runs regardless of the working directory.
- **FR-002**: The harness MUST drive every listed fixture through the existing slice
  pipeline (`pkg/pipeline.Runner`) with the **rule diagnostician**, fully offline — no
  network, no credentials, no Docker — so it runs unmodified in CI.
- **FR-003**: The harness MUST score each terminal case by comparing the produced diagnosis
  against the case's gold diagnosis using a trimmed, case-insensitive equality (bodhyanga's
  `diagnosisMatch` semantics), yielding a per-case boolean match.
- **FR-004**: The harness MUST report, per case: case id, gold diagnosis, produced
  diagnosis, match/failure/unscored status, and whether progression was forced; and MUST
  report one aggregate accuracy figure = matches ÷ scored-cases.
- **FR-005**: A case with no gold MUST be excluded from the accuracy denominator and reported
  as unscored; a case that errors or does not reach a terminal state MUST be reported as a
  failure and never counted as a match.
- **FR-006**: The harness MUST run headless — the physician-review gate MUST NOT pause a run;
  the harness resolves reviews deterministically without human input.
- **FR-007**: The harness MUST emit a machine-readable (JSON) report of per-case results plus
  the aggregate, in addition to a human-readable table.
- **FR-008**: The harness MUST compare the aggregate accuracy against a committed baseline and,
  when a fail-on-regression flag is set, exit non-zero if accuracy is below baseline minus a
  small tolerance; a regression MUST be printed with old vs new accuracy regardless of the
  flag (report-only when the flag is off).
- **FR-009**: The harness MUST support an update-baseline mode that overwrites the baseline
  with the current accuracy, provider label, case count, and a UTC timestamp, and skips the
  regression gate in that same invocation.
- **FR-010**: A missing baseline file MUST be informational (not fatal); an empty manifest
  and an unparseable/missing fixture MUST be fatal (fail fast). The harness MUST NOT report
  an accuracy figure for zero scored cases.
- **FR-011** *(cost — DEFERRED / blocked)*: bodhyanga's Pareto (accuracy-vs-cost) axis and its
  cost-ceiling regression gate are **out of scope** for this feature because bodhyanga-maf tracks
  no per-case or per-test cost (see Context findings). The report and baseline MUST be
  structured so a cost column and a cost gate can be added later without a breaking format
  change, and the missing-cost prerequisite MUST be documented (plan.md risks). No fabricated
  or placeholder cost figure may be emitted.
- **FR-012**: All harness behavior exercised by automated tests MUST be offline and
  deterministic (rule engine only); no test may require credentials, a network, or Docker.
  (Live-LLM benchmarking, if ever added, is opt-in behind env flags — not part of this
  feature.)
- **FR-013**: The harness MUST reuse bodhyanga-maf's existing evaluation and pipeline surfaces
  where they fit (`pkg/pipeline.Runner`, `eval.Store`/`eval.InteractionRecord`,
  `agents.PatientCase`) rather than reimplementing them; gold-scoring, which those surfaces
  do not provide, is the new logic this feature adds.

### Key Entities

- **Fixture**: one synthetic teaching case as a bodhyanga-maf `PatientCase` (case id, symptoms,
  history) stored as a JSON file — no real patient data.
- **Manifest**: the ordered list of fixtures with their gold diagnoses and metadata (tags);
  the authoritative source of bench expectations, as in bodhyanga.
- **Case result**: the outcome of running one fixture — produced diagnosis, forced flag,
  match/failure/unscored status, and the fixture's gold.
- **Aggregate summary**: the suite-level roll-up (provider label, cases scored, accuracy
  percentage) that is compared against the baseline and printed as the headline.
- **Baseline**: the committed record of a provider's expected accuracy (and case count and
  timestamp) that the regression gate consults; the cost fields bodhyanga's baseline carries are
  omitted here pending the cost prerequisite.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Running the harness with the rule engine against the committed manifest
  completes fully offline (no network, no credentials, no Docker) and prints a per-case table
  plus one aggregate accuracy figure.
- **SC-002**: The aggregate accuracy is deterministic — two consecutive runs produce byte-
  identical JSON reports for the per-case matches and the aggregate.
- **SC-003**: With accuracy at baseline, the fail-on-regression gate exits zero; with
  accuracy dropped beyond tolerance, it exits non-zero and names old vs new accuracy.
- **SC-004**: Update-baseline mode rewrites the baseline so an immediately-following gate run
  reports no regression, and does not itself fire the gate.
- **SC-005**: Every documented edge case (missing gold, mid-run failure, empty manifest,
  absent baseline) is exercised by an automated test with the expected outcome, all offline.
- **SC-006**: No cost figure is reported or gated (accuracy-only), and the report/baseline
  formats have a documented, non-breaking extension point for cost.

## Assumptions

- The slice pipeline (`pkg/pipeline.Runner`, the rule diagnostician, the review gate) is the
  substrate this feature drives; its behavior is not changed by this feature — the harness is
  a new caller, not a pipeline edit.
- bodhyanga's evaluation library (`pkg/eval`) is consumable via the existing `replace ../bodhyanga`;
  its `InteractionRecord`/`Store` and `diagnosisMatch` semantics are the authority for record
  shape and match comparison. Gold-vs-produced scoring is added by this feature because the
  bodhyanga-maf terminal record does not carry gold.
- New bodhyanga-maf-native fixtures and a manifest are authored for this feature (bodhyanga's fixtures
  are incompatible — different case schema and diagnosis vocabulary); all fixtures are
  synthetic teaching cases, no real patient data, consistent with bodhyanga's NOTICE.
- Cost/Pareto benchmarking is out of scope until the pipeline emits per-case cost; this is a
  named prerequisite, not a silent omission.
- CI runs the harness (or its test wrapper) as an offline gate; live-LLM benchmarking is a
  possible future opt-in, not part of this feature.

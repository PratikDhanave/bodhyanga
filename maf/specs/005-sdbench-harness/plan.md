# Implementation Plan: SD-Bench Harness Port

**Branch**: `feat/sdbench-harness` | **Date**: 2026-07-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/005-sdbench-harness/spec.md`

## Summary

Add an offline, accuracy-only SD-Bench harness to bodh-maf — the analog of bodh's
`cmd/bench` and the only evaluation runner on MAF-go (the framework ships evals for .NET
only). A new `pkg/bench` library loads a manifest of bodh-maf-native `PatientCase` fixtures
with gold diagnoses, drives each through the existing `pkg/pipeline.Runner` with the **rule
diagnostician** (fully offline), scores the produced diagnosis against gold with bodh's
`diagnosisMatch` semantics, aggregates accuracy, and gates CI by comparing the aggregate to
a committed baseline (`-fail-on-regression`, `-update-baseline`). A thin `cmd/bench` wires
flags → library → table/JSON output and exit codes. **Cost/Pareto is out of scope** because
bodh-maf tracks no per-case cost (documented dependency, not a silent gap).

## Technical Context

**Language/Version**: Go 1.26 (module `github.com/PratikDhanave/bodh-maf`)

**Primary Dependencies**: existing `pkg/pipeline` (`Runner`, `Config`, `Outcome`), `pkg/agents`
(`PatientCase`, `CarePlan`, `CaseRejected`, rule diagnostician, `HashCaseID`), `pkg/admit`
(`AllowAll` policy for the offline path), `github.com/PratikDhanave/bodh/pkg/audit`
(`NewInMemory`), `github.com/PratikDhanave/bodh/pkg/eval` (`InteractionRecord`, `Store`,
`NewInMemoryStore`) — all via the existing `replace ../bodh`. **No new module dependency**;
the harness is stdlib + existing packages.

**Storage**: none. Manifest, fixtures, and baseline are JSON files under `data/cases/`; the
run is in-memory (`audit.NewInMemory`, `eval.NewInMemoryStore`). No Postgres, no Docker.

**Testing**: stdlib `go test`; table-driven; fixtures + manifest + baseline under
`pkg/bench/testdata/` for hermetic tests, plus the committed `data/cases/` suite driven by a
smoke test. **No testcontainers, no network, no credentials** (contrast specs/001, which
needed Postgres for the checkpoint store — this feature deliberately needs none).

**Target Platform**: single-process CLI + library (darwin/linux); offline.

**Project Type**: Go library package (`pkg/bench`) + one thin CLI (`cmd/bench`).

**Performance Goals**: each rule-engine case finalizes in well under a second in-process
(no LLM); the whole committed suite (single-digit case count) runs in under a few seconds —
fast enough for a per-PR CI gate.

**Constraints**: offline + deterministic (rule engine, no clock in scoring); zero PHI —
per-case identifiers in any report or baseline appear only as `agents.HashCaseID`
fingerprints or the manifest's opaque case id, never raw symptoms/history; the harness reads
the pipeline's public surface only and never edits the pipeline.

**Scale/Scope**: 1 library package, 1 thin CLI, 1 manifest + a handful of fixtures + 1
baseline, ~10–12 tests. One-week vertical.

## Constitution Check

*GATE: evaluated against `.specify/memory/constitution.md` (bodh constitution, inherited).*

| Principle | Status | Notes |
|---|---|---|
| I. MARA pipeline | PASS | Drives the existing 4-stage pipeline unchanged; adds a caller, not a stage |
| II. LLM-agnostic provider abstraction | PASS | Rule engine only for this feature; provider seam untouched; live-LLM benchmarking is a future opt-in, explicitly out of scope |
| III. HIPAA (non-negotiable) | PASS | In-memory audit/eval; fixtures are synthetic; report/baseline key on hashed ids or opaque manifest ids; no raw clinical content in any output |
| IV. Test-first, coverage gates | PASS | Tests-first per user story; deterministic offline fixtures; coverage target ≥90% on `pkg/bench` |
| V. Package-first | PASS | Logic lives in `pkg/bench`; `cmd/bench` is flags + I/O only, mirroring `cmd/slice` |
| VI. Observability by default | N/A (DEFERRED) | Batch offline runner; no tracing wiring in scope, consistent with specs/001's deferral |

**Deviation from bodh's `cmd/bench` (justified in Complexity Tracking): accuracy-only, no
Pareto/cost.**

**Post-design re-check**: no new violations; the cost omission is a documented pipeline
prerequisite, not a constitutional deviation.

## Technical Approach

### What imports unchanged vs. what is new

| From bodh / bodh-maf | Reused as-is? | Notes |
|---|---|---|
| `bodh/pkg/eval.InteractionRecord`, `Store`, `NewInMemoryStore` | **Yes** | Record shape + in-memory store; no change |
| bodh's `diagnosisMatch` (trimmed, case-insensitive `EqualFold`) | **Re-implement, not import** | It is unexported in `bodh/pkg/eval/metrics.go`; port the ~5-line comparison into `pkg/bench` and unit-test it against the same cases. Do NOT import `bodh/pkg/eval.ScoreCase` / `CaseMetrics` — they depend on `medical.CaseState` (cost/RPM), which bodh-maf's pipeline never produces |
| `pkg/pipeline.Runner`, `Config`, `Outcome` | **Yes** | The Runner is the per-case driver; `Outcome.Plan.Diagnosis` / `Outcome.Rejected` / `Outcome.Denied` / `Outcome.Failures` are the terminal signals the harness reads |
| `pkg/agents.PatientCase`, `CarePlan`, `CaseRejected`, `HashCaseID` | **Yes** | Fixture unmarshals to `PatientCase`; produced diagnosis read from `Outcome.Plan.Diagnosis` |
| `pkg/admit.AllowAll` | **Yes** | The offline benchmark runs with an allow-all boundary so admission never denies a benign case |
| bodh's `data/cases/*.json`, `manifest.json`, `baseline.json` | **No — author fresh** | Incompatible case schema (`chief_complaint/hpi/...` vs `Symptoms/History`) and diagnosis vocabulary (Title-Case vs lowercase). New bodh-maf fixtures + manifest + baseline are authored under bodh-maf's own `data/cases/` |
| bodh's `cmd/bench` manifest/baseline JSON *structure* + regression logic | **Port the shapes, drop cost** | Keep bodh's manifest v2 shape (`version`, `description`, `cases[]{id,path,gold_diagnosis,tags}`) minus `max_cost_usd`; keep the baseline shape minus `mean_cost_usd`; port `detectRegressions`/`writeBaseline` accuracy half only |

### How metrics are sourced (evalhook vs. re-derive)

The pipeline's `evalhook` record (`pkg/evalhook/hook.go`) carries `Diagnosis`, `Confidence`,
`Rounds`, `Forced`, and a `Success` that means *approved-and-not-forced* — a **review**
outcome, NOT diagnostic accuracy, and it has **no gold**. So:

- **Accuracy is re-derived in the harness**, not read from the eval record: the harness holds
  the gold (from the manifest) and the produced diagnosis (`Outcome.Plan.Diagnosis`) and
  compares them itself. This is unavoidable — no bodh-maf surface knows the gold.
- The eval record is still written per case by the Runner as today (`Config.EvalStore`); the
  harness MAY collect the records for a secondary confidence/forced roll-up, but the
  authoritative accuracy number comes from gold-vs-produced. Keeping the eval store wired
  costs nothing and preserves the specs/001 invariant (one record per terminal case).

### Manifest + baseline formats (reuse bodh's shape, drop cost)

Reuse bodh's manifest **v2** shape so the format is familiar and a future cost column slots
in cleanly, minus the fields with no data source:

```jsonc
// data/cases/manifest.json  (bodh-maf-native)
{ "version": 2, "description": "bodh-maf SD-Bench manifest — synthetic teaching cases only",
  "cases": [ { "id": "maf-acs-001", "path": "data/cases/acs.json",
               "gold_diagnosis": "acute coronary syndrome", "tags": ["cardiology"] } ] }
```
- Gold strings MUST match bodh-maf's rule vocabulary (lowercase: "acute coronary syndrome",
  "community-acquired pneumonia", "heart failure"), verified against `pkg/agents/diagnostician.go`.
- `max_cost_usd` is **omitted** (FR-011). A later cost feature adds it back without breaking
  readers that ignore unknown fields.

```jsonc
// data/cases/baseline.json  (accuracy-only)
{ "version": 1, "providers": [ { "provider": "rule", "cases": 4,
    "accuracy_pct": 100.0, "recorded_at": "2026-07-16T00:00:00Z" } ] }
```
- `mean_cost_usd` omitted; accuracy floor only. Tolerance: accuracy must drop **> 1.0 pp** to
  trip the gate (bodh's `accuracyToleranceP`).

### Staying offline for CI

- Rule diagnostician only; `admit.AllowAll` boundary; `audit.NewInMemory`;
  `eval.NewInMemoryStore`. Nothing touches the network, credentials, or Docker.
- The review gate is handled headlessly by running the Runner with an **auto-approving
  Responder** (or `Config.Review=false`). **Design decision:** run with review enabled and an
  auto-approve `Responder` so the harness exercises the *same* terminal path the real pipeline
  takes (produces a `CarePlan`), rather than a review-disabled shortcut. The Responder never
  parks a request, so no run pauses (satisfies FR-006).
- CI reproduces the gate by running `go test ./pkg/bench` (a test that runs the committed
  suite and asserts no regression against the committed baseline), so the gate runs even
  without invoking the binary.

### MAF-go / Runner integration points (real, from files read)

- `pipeline.NewRunner(cfg, rec)` → `Runner.Run(ctx, pc, "")` returns `*Outcome`
  (`pkg/pipeline/runner.go`). The harness loops fixtures calling `Run` once each with a fresh
  or shared Runner (Runner is stateless per `Run` given no checkpoint store).
- Terminal signals read from `Outcome`: `Plan *CarePlan` (produced diagnosis =
  `Plan.Diagnosis`), `Rejected *CaseRejected`, `Denied bool`, `Paused bool`,
  `Failures []ExecutorFailedEvent`, `Forced []ForcedProgressionEvent`.
  - `Plan != nil` → scorable (compare `Plan.Diagnosis` to gold).
  - `Rejected != nil` / `Denied` / `Paused` / non-nil error / `Plan == nil` → **failure**
    (FR-005): recorded, never a match.
- `Config` fields used: `Tau`, `MaxRounds`, `Boundary` (from `admit.New(AllowAll(), rec, nil)`),
  `Review`, `EvalStore`, `EngineName: "rule"` (all present in `pkg/pipeline/pipeline.go`).
- Auto-approve: set `Runner.Responder` to return an approving `ReviewDecision` for every
  `ExternalRequest`, exactly as `cmd/slice`'s `-approve` path does
  (`cmd/slice/main.go` lines ~104–110) — proven code to copy.

## Project Structure

### Documentation (this feature)

```text
specs/005-sdbench-harness/
├── spec.md
├── plan.md              # this file
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
pkg/bench/
  manifest.go            manifest + fixture loading (path resolution relative to manifest)
  score.go               diagnosisMatch (ported), per-case scoring, failure/unscored classification
  run.go                 drive fixtures through pipeline.Runner (rule, allow-all, auto-approve)
  report.go              per-case table + aggregate + JSON emit
  baseline.go            baseline load/write + accuracy regression detection (cost dropped)
  *_test.go              per-file tests + testdata/ hermetic fixtures
  testdata/              tiny manifest/fixtures/baseline for edge-case tests
cmd/bench/
  main.go                flags (-manifest, -baseline, -json, -fail-on-regression,
                         -update-baseline, -tau, -max-rounds), wiring, exit codes
data/cases/
  manifest.json          committed bodh-maf-native suite
  acs.json, cap.json, chf.json, undiff.json   synthetic fixtures (rule-vocabulary golds)
  baseline.json          committed accuracy baseline for provider "rule"
```

**Structure Decision**: `pkg/bench` library + thin `cmd/bench`, mirroring specs/001's
package-first layout and bodh's own `cmd/bench` split. Each file maps to one user story
concern (load / score+run → US1, baseline+gate → US2, update → US3) so tasks land
independently.

## Complexity Tracking

| Divergence from bodh's `cmd/bench` | Why | Simpler / faithful alternative rejected because |
|---|---|---|
| **Accuracy-only; no Pareto/cost column or cost gate** | bodh-maf's pipeline emits no per-case or per-test cost (verified: no cost field in `TestResult`, planner, `DiagnosisResult`, `CarePlan`, or `evalhook`) | Fabricating a placeholder cost would produce a meaningless Pareto and a false regression signal; deferring cost keeps the number honest. Format leaves a documented extension point (FR-011) |
| **Fresh fixtures/manifest instead of reusing bodh's** | bodh fixtures use `chief_complaint/hpi/...`→`PatientPresentation`; bodh-maf consumes `PatientCase{Symptoms,History}`, and diagnosis vocabularies differ (Title-Case vs lowercase) | Reusing bodh fixtures would require a case-schema adapter *and* a gold-vocabulary map — more code and more drift risk than authoring four small native fixtures |
| **Re-implement `diagnosisMatch` rather than import** | It is unexported in `bodh/pkg/eval`, and `eval.ScoreCase`/`CaseMetrics` pull in `medical.CaseState` (cost/RPM) the pipeline never produces | Importing the scorer would drag the cost/RPM data model into a runner that has none; the comparison is 5 lines, unit-tested for parity |

## Risks

- **Cost data absent (largest gap).** The whole accuracy-vs-cost Pareto — bodh's headline
  framing — has no data source. *Mitigation*: ship accuracy-only, document the pipeline
  prerequisite (per-test cost on `TestResult` + accumulation to a terminal cost field) as the
  blocker, and keep the report/baseline extensible. **Flagged as FR-011 and a follow-on
  dependency, not silently dropped.**
- **Fixture / rule-vocabulary drift.** Gold strings in the manifest must exactly match the
  rule engine's lowercase output; if `pkg/agents/diagnostician.go`'s rules change, golds go
  stale and accuracy silently drops. *Mitigation*: a unit test asserts every manifest gold is
  reachable from the rule table's known diagnoses, so a vocabulary change fails a test rather
  than a benchmark.
- **MAF-go preview churn.** Same risk as specs/001 (pinned commit); the harness reads only
  the stable `pipeline.Outcome` surface, so churn impact is limited to whatever specs/001
  already absorbs.
- **Review-gate headless handling.** If the auto-approve Responder is misconfigured the run
  pauses instead of completing, turning every case into a failure. *Mitigation*: a test
  asserts a benign case reaches `Plan != nil` (not `Paused`) under the harness's Runner
  config.
- **Eval `Success` ≠ accuracy confusion.** `evalhook`'s `Success` means review-approved, not
  correct-vs-gold; a reader could mistake it for accuracy. *Mitigation*: accuracy is derived
  solely from gold-vs-produced in `pkg/bench`; the eval record is secondary telemetry, and
  the report labels the accuracy column unambiguously.

## Scope & Non-Goals (~1 week)

**In scope**: manifest + fixture loading, rule-engine batch run (offline), gold-vs-produced
accuracy scoring, per-case + aggregate report (table + JSON), baseline regression gate,
update-baseline, all edge cases, a native fixture suite, tests.

**Non-goals**: cost/Pareto (blocked on pipeline cost data), LLM-provider benchmarking
(offline-only this feature), multi-provider comparison tables, latency/step metrics, RPM/
equity/drift metrics from bodh's wider `pkg/eval`, any pipeline modification, a CI workflow
file (the `go test` gate suffices; wiring it into a CI runner is a separate op task).

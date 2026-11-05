# Tasks: SD-Bench Harness Port

**Input**: Design documents from `specs/005-sdbench-harness/`

**Prerequisites**: plan.md, spec.md

**Tests**: INCLUDED — tests-first per user story (constitution IV). Every functional
requirement and every edge case has an offline, deterministic test (rule engine only; no
network, no credentials, no Docker).

**Organization**: grouped by user story (P1→P3); each story lands independently testable and
green before the next starts.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US3 per spec.md
- All paths are under `/Users/genesis/Developer/goworkspace/src/github.com/PratikDhanave/bodhyanga-maf/`

## Phase 1: Setup

- [ ] T001 Confirm no new module dependency is needed: the harness uses only `pkg/pipeline`,
  `pkg/agents`, `pkg/admit`, and (via existing `replace ../bodhyanga`) `bodhyanga/pkg/audit` +
  `bodhyanga/pkg/eval`; run `go build ./...` green in `go.mod` before adding files. Create the
  empty package dir `pkg/bench/` and `pkg/bench/testdata/`.

## Phase 2: Foundational (blocking prerequisites)

- [ ] T002 [P] Author the committed synthetic fixture suite in `data/cases/`: `acs.json`,
  `cap.json`, `chf.json`, `undiff.json` as `agents.PatientCase` JSON
  (`{case_id, symptoms, history}`), with symptoms chosen so the rule engine
  (`pkg/agents/diagnostician.go`) yields, respectively, "acute coronary syndrome",
  "community-acquired pneumonia", "heart failure", and "undifferentiated presentation".
  Synthetic only — no real patient data (NOTICE).
- [ ] T003 [P] Author `data/cases/manifest.json` (bodhyanga-maf v2 shape per plan.md: `version`,
  `description`, `cases[]{id,path,gold_diagnosis,tags}`; NO `max_cost_usd`) with a gold per
  fixture matching the rule engine's **lowercase** vocabulary; and `data/cases/baseline.json`
  (accuracy-only: `{version, providers[]{provider,cases,accuracy_pct,recorded_at}}`) left
  with a placeholder accuracy to be overwritten by T017's first update-baseline run.

**Checkpoint**: fixtures + manifest + baseline exist — the harness has a suite to load.

## Phase 3: User Story 1 — Run all fixtures, see accuracy (P1) 🎯 MVP

**Goal**: batch-run every fixture through the rule pipeline offline; score against gold;
print per-case + aggregate accuracy (table + JSON).

**Independent test**: manifest of N fixtures + rule engine + no creds ⇒ completes offline,
prints per-case gold/produced/match and aggregate accuracy = matches ÷ scored (SC-001, SC-002).

- [ ] T004 [P] [US1] Scoring tests first (red) in `pkg/bench/score_test.go`: `Match` is
  trimmed + case-insensitive (bodhyanga `diagnosisMatch` parity — exact, whitespace-diff, case-diff,
  empty-gold=unscored); classify a case as scored-match / scored-miss / unscored (no gold) /
  failure (no terminal plan).
- [ ] T005 [P] [US1] Manifest-load tests first (red) in `pkg/bench/manifest_test.go` using
  `pkg/bench/testdata/`: paths resolve relative to the manifest file; missing `path` ⇒ error;
  missing/unparseable fixture file ⇒ error (fail fast); empty `cases[]` ⇒ error; entry with
  no `gold_diagnosis` loads but is flagged unscored downstream.
- [ ] T006 [US1] Implement `pkg/bench/score.go`: port `diagnosisMatch` (trimmed
  `strings.EqualFold`); `CaseResult{ID, Gold, Produced string, Forced bool, Status}` with
  `Status ∈ {match, miss, unscored, failure}`; `Score(gold, produced string) Status`.
- [ ] T007 [US1] Implement `pkg/bench/manifest.go`: `Manifest`/`Entry` structs (v2 shape,
  no cost); `LoadManifest(path) ([]Fixture, error)` resolving fixture paths
  manifest-relative (mirror bodhyanga's resolver) and unmarshaling each to `agents.PatientCase`;
  empty-manifest + missing-fixture errors.
- [ ] T008 [US1] Run tests first (red) in `pkg/bench/run_test.go`: a benign fixture reaches
  `Outcome.Plan != nil` (NOT `Paused`) under the harness Runner config (allow-all boundary,
  review enabled + auto-approve Responder); a fixture that cannot terminate is classified
  `failure`; two runs of the same suite produce identical `CaseResult`s (determinism, SC-002).
- [ ] T009 [US1] Implement `pkg/bench/run.go`: `RunSuite(ctx, fixtures, cfg) ([]CaseResult, error)`
  building one `pipeline.Runner` with `admit.New(admit.AllowAll(), audit.NewInMemory(), nil)`,
  `Config{Tau, MaxRounds, Review:true, Boundary, EvalStore: eval.NewInMemoryStore(),
  EngineName:"rule"}`, and an auto-approve `Responder` (copy `cmd/slice/main.go` approve
  path); per fixture call `Runner.Run`, read `Outcome.Plan.Diagnosis` (match), else classify
  `failure`; carry gold from the manifest into each `CaseResult`.
- [ ] T010 [US1] Report tests first (red) in `pkg/bench/report_test.go`: aggregate accuracy =
  matches ÷ (scored = match+miss), excluding unscored and failures from the denominator;
  unscored/failure counts surfaced; JSON report is stable/deterministic (SC-002).
- [ ] T011 [US1] Implement `pkg/bench/report.go`: `Summary{Provider, Cases, Scored, Matches,
  Unscored, Failures int; AccuracyPct float64}`; `Aggregate([]CaseResult) Summary`;
  human table (case id, gold, produced, status, forced) + `-json` machine report; NO cost
  column (documented extension point per FR-011).
- [ ] T012 [US1] `cmd/bench/main.go` (thin): flags `-manifest` (default
  `data/cases/manifest.json`), `-tau`, `-max-rounds`, `-json`; load → RunSuite → Aggregate →
  print; exit non-zero on empty manifest / load error (FR-010); no baseline logic yet.

**Checkpoint**: US1 complete — `go run ./cmd/bench` prints the accuracy table offline (MVP).

## Phase 4: User Story 2 — Fail CI on regression vs baseline (P2)

**Goal**: compare aggregate accuracy to a committed baseline; `-fail-on-regression` ⇒
non-zero exit when accuracy drops beyond tolerance; report-only otherwise.

**Independent test**: baseline at current accuracy ⇒ gate exits 0; accuracy dropped > 1 pp ⇒
gate exits non-zero naming old vs new; missing baseline ⇒ informational, exit 0 (SC-003).

- [ ] T013 [P] [US2] Baseline tests first (red) in `pkg/bench/baseline_test.go`: load
  baseline; `DetectRegression` fires when `accuracy_pct + 1.0 < baseline` and not within
  tolerance (port bodhyanga's `accuracyToleranceP=1.0`, accuracy-only); missing baseline file ⇒
  informational (no error surfaced to the gate, returns "create one" message); malformed
  baseline ⇒ error.
- [ ] T014 [US2] Implement `pkg/bench/baseline.go`: `Baseline`/`BaselineEntry` (accuracy-only,
  no `mean_cost_usd`); `LoadBaseline(path)`; `DetectRegression(baseline, summary) []string`
  (accuracy floor with 1.0 pp tolerance, message names provider/new/baseline); missing-file
  handled as informational.
- [ ] T015 [US2] Wire the gate into `cmd/bench/main.go`: `-baseline` (default
  `data/cases/baseline.json`), `-fail-on-regression`; always print regressions for
  visibility, exit non-zero only when the flag is set (FR-008); missing baseline prints the
  "run -update-baseline" hint and does not fail.
- [ ] T016 [US2] Gate integration test in `pkg/bench/gate_test.go` (offline): run the
  committed suite, aggregate, and assert `DetectRegression` against the committed baseline is
  empty — this doubles as the **CI gate** (`go test ./pkg/bench`) so regression protection
  runs without invoking the binary (SC-003).

**Checkpoint**: US2 complete — CI can fail on accuracy regression via `go test ./pkg/bench`.

## Phase 5: User Story 3 — Refresh baseline after intentional change (P3)

**Goal**: one command overwrites the baseline with current accuracy; skips the gate in the
same invocation.

**Independent test**: update-baseline rewrites the file (accuracy, provider, cases, UTC
timestamp); an immediately-following gate run reports no regression; the gate does not fire
during the update run (SC-004).

- [ ] T017 [P] [US3] WriteBaseline tests first (red) in `pkg/bench/baseline_test.go`
  (extend): `WriteBaseline(path, summary)` writes provider `rule`, case count, accuracy_pct,
  and a UTC RFC3339 timestamp; round-trips through `LoadBaseline`; a subsequent
  `DetectRegression` against the written baseline is empty.
- [ ] T018 [US3] Implement `WriteBaseline` in `pkg/bench/baseline.go` (port bodhyanga's
  `writeBaseline`, accuracy-only, `MarshalIndent`, `MkdirAll`) and wire `-update-baseline`
  into `cmd/bench/main.go`: write baseline, print confirmation, **skip** the regression gate,
  exit 0 (FR-009); run it once to populate the real `data/cases/baseline.json` (finishing T003).

**Checkpoint**: US3 complete — baseline refresh is a single reviewable command.

## Phase 6: Polish & Cross-Cutting

- [ ] T019 [P] Edge-case suite in `pkg/bench/edgecases_test.go` (offline): empty manifest ⇒
  fatal; missing/unparseable fixture ⇒ fatal; missing gold ⇒ unscored + excluded from
  denominator; a case forced to non-terminal/denied ⇒ failure, never a match; absent baseline
  ⇒ informational (SC-005) — one test per spec.md edge case.
- [ ] T020 [P] Vocabulary-drift guard in `pkg/bench/manifest_test.go`: assert every committed
  manifest gold is one of the rule engine's producible diagnoses (the lowercase strings in
  `pkg/agents/diagnostician.go` plus "undifferentiated presentation"), so a rule change fails
  a test instead of silently dropping accuracy (plan.md risk).
- [ ] T021 [P] Coverage ≥90% on `pkg/bench` (`go test -cover ./pkg/bench`); `go vet ./...` +
  staticcheck clean; confirm `go test ./...` runs the whole suite with no network / no Docker.
- [ ] T022 Docs: add a `make bench` / `go run ./cmd/bench` line to `CLAUDE.md` commands and a
  short "SD-Bench harness (accuracy-only; cost is a flagged pipeline prerequisite)" note to
  `README.md`/`architecture.md`; record the cost prerequisite as a follow-on so it isn't lost.

## Dependencies

- Setup (T001) → Foundational (T002–T003) → US1 (T004–T012)
- US2 (T013–T016) requires US1's `Aggregate`/`Summary`
- US3 (T017–T018) requires US2's baseline format
- Polish (T019–T022) after the stories; T020 depends on T003's committed manifest

Story order: **US1 → US2 → US3**, strictly sequential (each builds on the prior's output type).

## Parallel example (within US1)

```
T004 (score_test) ∥ T005 (manifest_test) ∥ T002/T003 (fixtures/manifest) first,
then T006 → T007 → (T008 → T009) → (T010 → T011) → T012
```

## Implementation strategy

MVP = Phase 1–3 (US1): `go run ./cmd/bench` printing an offline accuracy table already
delivers the evaluation capability MAF-go otherwise lacks. Then land US2 (the CI gate) and
US3 (baseline refresh) one PR each; every checkpoint leaves `go test ./...` green and offline.
Cost/Pareto is explicitly deferred to a follow-on that first adds per-case cost to the
pipeline — tracked as the named prerequisite, not attempted here.

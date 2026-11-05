# Tasks: FHIR/HL7 Intake

**Input**: Design documents from `specs/002-fhir-hl7-intake/`

**Prerequisites**: plan.md, spec.md

**Tests**: INCLUDED — spec FR-010 mandates automated coverage of the happy path and every
rejection category; tests are written first (red) per story, consistent with specs/001.

**Organization**: grouped by user story (P1→P3); each story lands independently testable and
green before the next starts. US1 is the MVP (HL7 ADT → care plan). All paths below are
relative to the repo root
`/Users/genesis/Developer/goworkspace/src/github.com/PratikDhanave/bodhyanga-maf`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US3 per spec.md

## Phase 1: Setup

- [ ] T001 Confirm no go.mod change is needed: `go list -deps github.com/PratikDhanave/bodhyanga/pkg/hl7v2 github.com/PratikDhanave/bodhyanga/pkg/fhir` resolves via the existing `replace ../bodhyanga` (go.mod:5) with no new require; record the result in a comment in `pkg/intake/doc.go`. If a require *is* needed, add it and `go mod tidy` green.

## Phase 2: Foundational (blocking prerequisites)

- [ ] T002 [P] Package skeleton + core types in `pkg/intake/inbound.go`: `Format` enum (`FormatJSON`, `FormatHL7`, `FormatFHIR`), `Inbound{Format Format; Payload []byte}`, and `RejectReason` enum (`ReasonMalformed`, `ReasonUnsupportedType`, `ReasonUnsupportedTrigger`, `ReasonMissingSegment`, `ReasonMissingResource`, `ReasonAmbiguousSubject`) with a `RejectError{Reason RejectReason; Fingerprint string}` type whose `Error()` string is drawn ONLY from the reason category (never message content) — spec FR-005/FR-006. Unit test in `pkg/intake/inbound_test.go` asserting `RejectError.Error()` contains no interpolated payload.
- [ ] T003 [P] Package doc in `pkg/intake/doc.go`: state the front-door contract (import-unchanged vs. new, PHI-free rejection taxonomy, `medical.*→PatientCase` mapping is demo-grade) per plan.md.

**Checkpoint**: types + reason taxonomy exist — mappers can be written against them.

## Phase 3: User Story 1 — Admit from an HL7 v2 ADT message (P1) 🎯 MVP

**Goal**: an ADT^A01/A04/A08 message becomes one `agents.PatientCase` and runs end-to-end
through the unchanged pipeline, with the translation hop admitted and audited.

**Independent test**: a well-formed ADT^A01 fixture → one PatientCase (CaseID from PID-3) →
care plan, with a translation admission record preceding intake on the trail (SC-001).

- [ ] T004 [P] [US1] Contract tests (write first, red) in `pkg/intake/hl7_test.go`: well-formed A01 (from `../bodhyanga/pkg/hl7v2/testdata/adt_a01_admit.hl7`) → PatientCase with CaseID derived from PID-3 and History from DG1; A04/A08 accepted; non-ADT (ORU) → `ReasonUnsupportedType`; custom-delimiter message still parses. Assert NO raw MRN/name in any `RejectError`.
- [ ] T005 [US1] HL7 mapper in `pkg/intake/hl7.go`: `translateHL7([]byte) (agents.PatientCase, error)` wrapping `hl7v2.ParseMessage` + `hl7v2.MessageToADTEvent` (imported unchanged); map `medical.ADTEvent` → `agents.PatientCase{CaseID: ev.PatientID, History: [ev.PrimaryDx] if present, Symptoms: nil}`; translate the parser's non-ADT / unsupported-trigger / missing-PID errors into the matching `RejectReason` (do NOT wrap the raw error text — categorize) — spec FR-001/FR-003/FR-008/FR-009.
- [ ] T006 [US1] `Translate(Inbound)` dispatch in `pkg/intake/translate.go`: route `FormatHL7`→`translateHL7`, `FormatJSON`→`json.Unmarshal` into PatientCase (parity with today's `cmd/slice`), unknown format → `ReasonUnsupportedType`; unit test in `pkg/intake/translate_test.go`.
- [ ] T007 [US1] Translation executor in `pkg/agents/translator.go`: `NewTranslator(id string, guard Guard) *workflow.Executor` consuming `intake.Inbound`, wrapping the body in `guarded(guard, ctx, id, "inbound_message", token, ...)` where `token = HashCaseID(caseID)` on success (empty on pre-parse failure); on success `ctx.SendMessage("", pc)`, on `RejectError` record + return the error (no PatientCase emitted); `.Extend` with `ConfigureProtocol` declaring it *sends* `PatientCase`. Follows `pkg/agents/intake.go:14-27`. Test in `pkg/agents/translator_test.go` with a fake guard.
- [ ] T008 [US1] Wire opt-in translation into `pkg/pipeline/pipeline.go`: add `Config.Translate bool`; when true, build `translator := agents.NewTranslator("translator", guard).Bind()`, make it the `NewBuilder` start node, and `AddEdge(translator, intake)`; when false, graph is byte-identical to today (SC-006). Existing `pkg/pipeline` tests MUST stay green.
- [ ] T009 [US1] Integration test in `pkg/pipeline/intake_hl7_test.go`: feed an A01 fixture as `Inbound{FormatHL7, ...}` through a `Runner` (reuse `internal/slicetest`/`Runner`); assert a CarePlan is produced and the trail (via specs/001 trail-invariant checker in `pkg/auditbridge`) shows a translation admission record before the intake record — SC-001, FR-004, FR-007.
- [ ] T010 [US1] CLI wiring in `cmd/slice/main.go`: add `-format json|hl7|fhir` (default json); when non-json, read the raw file bytes, build `intake.Inbound`, set `Config.Translate=true`; keep the built-in demo (json) path unchanged. Manual smoke: `go run ./cmd/slice -format hl7 -case adt_a01_admit.hl7`.

**Checkpoint**: US1 fully functional — a real HL7 ADT front door drives the slice.

## Phase 4: User Story 2 — Admit from a FHIR R4 bundle (P2)

**Goal**: a FHIR R4 bundle (one Patient + Conditions/Observations) becomes one PatientCase and
runs end-to-end with the same admission/trail shape as US1.

**Independent test**: a bundle fixture → one PatientCase (CaseID from Patient.id, History from
Conditions) → care plan; trail from intake onward structurally identical to the same facts as
ADT (SC-002).

- [ ] T011 [P] [US2] FHIR bundle fixtures in `pkg/intake/testdata/`: `bundle_one_condition.json`, `bundle_multi_condition.json`, `bundle_no_condition.json` (undifferentiated), `bundle_no_patient.json`, `bundle_two_patients.json`, `bundle_malformed.json` — PHI-shaped MRNs/names present so SC-004's scan is meaningful.
- [ ] T012 [P] [US2] Bundle-decoder tests (write first, red) in `pkg/intake/fhir_test.go`: one-Condition bundle → PatientCase with History; multi-Condition → each Condition contributes one entry (none dropped silently); no-Condition → undifferentiated (empty clinical, no error, FR-008); no-Patient → `ReasonMissingResource`; two-Patient → `ReasonAmbiguousSubject`; non-JSON → `ReasonMalformed`. Assert no raw MRN/name in any `RejectError`.
- [ ] T013 [US2] FHIR bundle decoder + mapper in `pkg/intake/fhir.go`: NEW minimal `Bundle{ResourceType string; Entry []struct{Resource json.RawMessage}}` (bodhyanga has none — plan.md); dispatch each entry by `resourceType` into `bodhyanga/pkg/fhir` types (`Patient`, `Condition`, `Observation`); reject ≠1 Patient; map `Patient.id`→CaseID, each `fhir.ConditionCode(c)`→History entry, each recognized `fhir.ObservationToRPM`→Symptoms/History entry — spec FR-002/FR-003/FR-008/FR-009.
- [ ] T014 [US2] Route `FormatFHIR`→`translateFHIR` in `pkg/intake/translate.go`; extend `translate_test.go`.
- [ ] T015 [US2] Integration + parity test in `pkg/pipeline/intake_fhir_test.go`: bundle fixture → CarePlan; and a parity assertion that the SAME clinical facts fed as HL7 (US1) and as FHIR yield structurally identical trails from the intake hop onward (specs/001 invariant checker) — SC-002, SC-005, FR-007.
- [ ] T016 [US2] CLI: `-format fhir` path already covered by T010's generic raw-bytes read; add a smoke assertion / example to `cmd/slice/main.go` usage doc comment.

**Checkpoint**: FHIR bundle is a first-class source alongside HL7.

## Phase 5: User Story 3 — Bad input rejected loudly and on the record (P3)

**Goal**: every un-translatable input is rejected with a distinct PHI-free reason, produces
no PatientCase and no pipeline run, and lands a rejection record on the trail.

**Independent test**: the four canonical bad inputs each → distinct PHI-free reason, zero
runs, one rejection record; automated PHI scan of every front-door record finds nothing
(SC-003, SC-004).

- [ ] T017 [P] [US3] Rejection-taxonomy unit tests in `pkg/intake/reject_test.go`: malformed HL7 (`../bodhyanga/pkg/hl7v2/testdata/adt_malformed.hl7`)→`ReasonMalformed`; valid-ADT-unsupported-trigger (synthesize an A40 message)→`ReasonUnsupportedTrigger`; ADT missing PID→`ReasonMissingSegment`; each maps to a distinct reason and produces no PatientCase — spec FR-005.
- [ ] T018 [US3] Translator rejection path in `pkg/agents/translator.go` (extend T007): on `RejectError`, record an `agent_error`-shaped audit event carrying ONLY the reason category + fingerprint (fingerprint empty when unrecoverable pre-parse) and terminate the run without emitting PatientCase — spec FR-004/FR-006. Extend `translator_test.go`.
- [ ] T019 [US3] Rejection integration test in `pkg/pipeline/intake_reject_test.go`: each canonical bad input through a Runner ⇒ no CarePlan, no CaseRejected, exactly one rejection record on the trail, run does not start intake — SC-003.
- [ ] T020 [US3] PHI-freedom test in `pkg/pipeline/intake_phi_test.go`: run all success + all rejection fixtures; scan every emitted audit record's fields for each fixture's literal MRN/patient-name/narrative substring; assert zero hits; assert identifiers appear only as `HashCaseID` fingerprints — SC-004 (reuse the specs/001 PHI-scan invariant from `pkg/auditbridge` where available).
- [ ] T021 [US3] Fail-closed-on-translation test in `pkg/pipeline/intake_deny_test.go`: with a deny-all / deny-agent=`translator` policy, a valid HL7 message produces no case, one synchronous deny record, and the run halts — proving the translation hop is inside the admission boundary (FR-004, specs/001 FR-002/FR-003).

**Checkpoint**: the ingestion boundary is fail-closed and provably PHI-free.

## Phase 6: Polish & Cross-Cutting

- [ ] T022 [P] Coverage ≥90% on `pkg/intake` and the new translator (`go test -cover ./pkg/intake/... ./pkg/agents/...`); `go vet ./...` + staticcheck clean.
- [ ] T023 [P] Confirm the specs/001 suite is untouched: `go test ./...` green with `Config.Translate` defaulting false (SC-006 — no graph regression).
- [ ] T024 Update `README.md` "Explicit non-goals" (remove FHIR/HL7 ingestion from the non-goals list, add a scope line) and `architecture.md` (document the pre-intake translation hop); note the demo-grade `medical.*→PatientCase` mapping.
- [ ] T025 Update `CLAUDE.md` layout section (add `pkg/intake/`) and the `<!-- SPECKIT -->` block to point at `specs/002-fhir-hl7-intake`; add the `-format` flag to the commands list.

## Dependencies

- Setup (T001) → Foundational (T002–T003) → US1 (T004–T010)
- US2 (T011–T016) requires US1's `Translate` dispatch + translator node + pipeline wiring
  (T006–T008); the bundle decoder itself (T011–T013) is independent and can start in parallel
  with late US1.
- US3 (T017–T021) requires US1 (translator) + US2 (both parse paths to reject across); T020
  requires all success + rejection fixtures to exist.
- Polish (T022–T025) last.

Story order: **US1 → US2 → US3** — US1 is the demoable MVP; US2 reuses US1's seam; US3 is the
fail-closed spine that must be green before ship.

## Parallel example (within US1/US2)

```
T002 (types) ∥ T003 (doc)
then T004 (HL7 tests) → T005 (HL7 mapper) → T006 (dispatch) → T007 (executor) → T008 (wire) → T009 (integ) → T010 (CLI)
T011 (FHIR fixtures) ∥ T012 (FHIR tests) can begin during late US1; then T013 → T014 → T015
```

## Implementation strategy

MVP = Phase 1–3 (US1): an HL7 ADT front door alone answers "can the slice ingest a real
hospital protocol into the unchanged pipeline" and is demoable (`-format hl7`). Then land US2
(FHIR) and US3 (rejection taxonomy) as separate commits/PRs, each leaving `go test ./...`
green and the specs/001 graph untouched.

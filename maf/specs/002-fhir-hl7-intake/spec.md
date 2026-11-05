# Feature Specification: FHIR/HL7 Intake

**Feature Branch**: `feat/fhir-hl7-intake`

**Created**: 2026-07-16

**Status**: Draft

**Input**: User description: "Let the MARA slice pipeline ingest a case from a FHIR R4
bundle and/or an HL7 v2 ADT message, not just a native `PatientCase` JSON. A hospital's
'front door' is an ADT feed and a FHIR endpoint; the slice can only accept its own JSON
today. Add a translation front-door that turns an inbound clinical message into the exact
`agents.PatientCase` the existing intake executor already consumes — with the same
admission boundary on the translation hop and the same zero-PHI-in-the-audit-trail
invariant the safety-boundary slice (specs/001) established. Success = an ADT^A01 admit
and a FHIR R4 bundle each run end-to-end into a care plan, unsupported/malformed inputs
are rejected loudly and audited, and no raw MRN or clinical narrative ever reaches the
trail."

## User Scenarios & Testing *(mandatory)*

The pipeline entry point is fixed and **not** changed by this feature: the intake executor
consumes `agents.PatientCase{CaseID, Symptoms []string, History []string}`
(`pkg/agents/types.go:13`, `pkg/agents/intake.go:14`). This feature adds a *translation
front-door* that produces that struct from an external clinical message and hands it to the
existing pipeline unchanged. Every user story below is an increment on that front-door.

### User Story 1 - Admit a patient from an HL7 v2 ADT^A01 message (Priority: P1)

As an EHR-integration engineer whose hospital's admit feed is HL7 v2 ADT, I need to hand the
pipeline a raw ADT^A01 admit message and have it run the identical MARA pipeline that native
JSON runs, so that a real hospital front-door produces a care plan without me pre-translating
to the slice's private format.

**Why this priority**: HL7 v2 ADT is the single most common "front door" in US hospital
integrations, and bodhyanga already ships a battle-tested ADT parser (`bodhyanga/pkg/hl7v2`) we can
import unchanged — so this is the highest-value, lowest-risk first increment and it stands
alone (it needs neither FHIR nor the malformed-handling story to be demoable).

**Independent Test**: Feed a well-formed ADT^A01 message (MSH/EVN/PID/PV1/DG1) to the
front-door and assert (a) a `PatientCase` is produced whose `CaseID` derives from PID-3 and
whose clinical fields derive from DG1/PV1, and (b) the case completes end-to-end into a care
plan through the unchanged pipeline, with a translation admission record preceding the intake
record on the trail.

**Acceptance Scenarios**:

1. **Given** a well-formed ADT^A01 admit message, **When** it enters the front-door, **Then**
   exactly one `PatientCase` is produced and the case runs to a care plan through the same
   graph native JSON uses (no graph edits).
2. **Given** the same message, **When** the trail is read back, **Then** it contains a
   translation-hop admission record, keyed on the case *fingerprint*, that precedes the
   intake record — i.e. translation is inside the admission boundary like every other hop.
3. **Given** an ADT^A04 (register outpatient) or ADT^A08 (update) message, **When** it enters
   the front-door, **Then** it is accepted and produces a `PatientCase` (same admit family as
   A01), because these are the trigger events the ported parser already supports.
4. **Given** an ADT message whose PID-3 carries an `MR`-typed identifier, **When** the case is
   built, **Then** the `PatientCase.CaseID` is derived from that MR number (the durable
   cross-system identifier) and appears on the trail only as a fingerprint.

---

### User Story 2 - Admit a patient from a FHIR R4 bundle (Priority: P2)

As an integration engineer whose EHR exposes a FHIR R4 API, I need to hand the pipeline a
FHIR bundle (a Patient plus its Conditions/Observations) and have it produce the same
`PatientCase` and run the same pipeline, so that a FHIR-native site is a first-class source
alongside HL7.

**Why this priority**: FHIR is the modern, second-most-common front door and reuses the same
translation seam and the same admission/trail invariants as US1, so it is a clean parallel
increment. It ranks below US1 because the ported FHIR code parses *individual* resources but
has **no bundle decoder** (`bodhyanga/pkg/fhir` has Patient/Observation/Encounter/Condition types
but no `Bundle` type — verified in `bodhyanga/pkg/fhir/fhir.go` and `bulk.go`), so this story
carries slightly more new code than US1.

**Independent Test**: Feed a FHIR R4 transaction/collection bundle containing one Patient and
one or more Condition/Observation resources; assert one `PatientCase` is produced whose
`CaseID` derives from the Patient resource id, whose `Symptoms`/`History` derive from the
Condition/Observation resources, and that the case completes end-to-end with the same
admission/trail shape as US1.

**Acceptance Scenarios**:

1. **Given** a FHIR R4 bundle with a Patient and one Condition, **When** it enters the
   front-door, **Then** one `PatientCase` is produced (CaseID from the Patient id, a symptom
   or history entry from the Condition code/text) and it runs to a care plan.
2. **Given** a bundle with several Conditions/Observations, **When** it is translated, **Then**
   each recognized clinical resource contributes one entry to `Symptoms`/`History` — no
   resource is silently dropped without a recorded reason.
3. **Given** a bundle, **When** the trail is read back, **Then** the translation admission
   record precedes intake and keys only on the case fingerprint, identical in shape to US1's
   trail (source format is invisible to the trail).
4. **Given** the same clinical facts expressed once as ADT and once as FHIR, **When** both
   run, **Then** their downstream trail shapes are structurally identical (the front-door is
   the only place format matters).

---

### User Story 3 - Bad input is rejected loudly and on the record (Priority: P3)

As a clinical-platform safety steward, I need any input that cannot be safely and
unambiguously turned into a case to be *rejected* — never guessed at, never partially
ingested — with the rejection reason on the audit trail (and never containing PHI), so that a
garbled or unexpected message can never silently become a fabricated case.

**Why this priority**: This is the safety spine of an ingestion boundary — the analogue of
specs/001's fail-closed rule, applied to parsing. It ranks after the two happy paths because
those must exist before there is a translation seam to reject *into*, but it is mandatory
before the feature can ship.

**Independent Test**: Feed (a) a malformed HL7 message, (b) an HL7 message with a supported
message type but an unsupported trigger event, (c) an HL7 message missing a required segment,
and (d) a malformed/empty FHIR bundle; assert each is rejected with a distinct, PHI-free
reason, no `PatientCase` is produced, no pipeline run starts, and a rejection record exists on
the trail.

**Acceptance Scenarios**:

1. **Given** a syntactically malformed HL7 message (e.g. not starting with MSH, or truncated
   header), **When** it enters the front-door, **Then** translation fails with a clear reason,
   no case runs, and a PHI-free rejection is recorded.
2. **Given** an HL7 message that is a valid ADT but carries an unsupported trigger event (e.g.
   A40 merge), **When** it enters the front-door, **Then** it is rejected as "unsupported
   trigger event" (not silently mapped to admit), and the rejection is recorded.
3. **Given** an ADT message missing a required segment (no PID, or PID-3 has no usable
   identifier), **When** it enters the front-door, **Then** it is rejected as "missing
   required segment/identifier", no case runs, and the rejection is recorded.
4. **Given** a FHIR payload that is not valid JSON, is an empty bundle, or contains no Patient
   resource, **When** it enters the front-door, **Then** it is rejected with the specific
   reason and recorded.
5. **Given** any rejection record from the above, **When** its fields are inspected, **Then**
   they contain no raw MRN, no patient name, no clinical narrative — the reason is a *category*,
   and any identifier appears only as a fingerprint.

---

### Edge Cases

- **Message that is well-formed but not ADT** (e.g. an ORU results message) → rejected as
  "unsupported message type"; the ported parser's `MessageToADTEvent` already refuses
  non-ADT (`bodhyanga/pkg/hl7v2/hl7v2.go:347`), so the front-door surfaces that as a category
  rejection, not a panic.
- **ADT with no DG1 / FHIR bundle with a Patient but no Condition/Observation** → a case with
  empty clinical content is *undifferentiated*, not an error: it produces a `PatientCase` with
  empty `Symptoms`/`History` and flows into the pipeline's existing "undifferentiated / forced
  progression" behavior (specs/001 edge case), because absence of coded findings is a clinical
  reality, not a parse failure.
- **Custom HL7 encoding characters** (a sender using `#` instead of `|`) → honored, because the
  ported parser reads delimiters off MSH-1/MSH-2 rather than hardcoding them
  (`bodhyanga/pkg/hl7v2/hl7v2.go:100`); no special handling needed here, but a test asserts it.
- **HL7 timestamp precision variance** (YYYY vs YYYYMMDDHHMMSS, ±TZ) → handled by the ported
  parser; the front-door does not re-parse time and never puts a raw timestamp on the trail.
- **Multiple patients in one FHIR bundle** → out of scope for the slice; the front-door
  rejects a bundle with >1 Patient resource as "ambiguous subject" rather than guessing which
  patient the case is about.
- **Translation succeeds but the admission boundary denies the translation hop** (a demoted or
  deny-all policy) → behaves exactly like a denial on any other hop (specs/001 FR-002):
  fail-closed, no case runs, one synchronous deny record. The front-door does not bypass
  admission.
- **PHI in a rejection reason** → forbidden by construction: reasons are drawn from a fixed
  category set, never interpolated from message content.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST accept an HL7 v2 ADT message (trigger events A01/A02/A03/A04/A08,
  the set the ported parser supports) as case input and translate it to exactly one
  `agents.PatientCase` consumed by the existing intake executor — with no change to the
  pipeline graph.
- **FR-002**: The system MUST accept a FHIR R4 bundle containing exactly one Patient resource
  (plus zero or more Condition/Observation resources) as case input and translate it to
  exactly one `agents.PatientCase` — with no change to the pipeline graph.
- **FR-003**: `PatientCase.CaseID` MUST derive from the source's durable patient identifier
  (HL7: PID-3, MR-typed preferred; FHIR: Patient.id) and MUST be treated as PHI everywhere
  downstream (appears on the trail only as `agents.HashCaseID` fingerprint, per specs/001
  FR-005).
- **FR-004**: The translation step MUST sit *inside* the admission boundary: it is admitted,
  audited, and fail-closed exactly like any pipeline hop (specs/001 FR-001/FR-002); a denied
  translation prevents any case from running.
- **FR-005**: A message that cannot be unambiguously translated MUST be rejected with a
  reason drawn from a fixed, PHI-free category set (malformed, unsupported message type,
  unsupported trigger event, missing required segment/resource, ambiguous subject); the system
  MUST NOT produce a partial or guessed `PatientCase`.
- **FR-006**: Every rejection MUST produce an audit record (reason category + case fingerprint
  if one is recoverable, else no identifier); rejection records MUST NOT contain raw MRNs,
  patient names, or clinical narrative.
- **FR-007**: The audit trail of a successfully translated case MUST be structurally identical
  regardless of source format (native JSON, HL7, FHIR) from the intake hop onward — the source
  format is invisible past the front-door.
- **FR-008**: A message with no coded clinical findings (no DG1 / no Condition) MUST NOT be an
  error; it MUST produce an undifferentiated `PatientCase` that flows into the pipeline's
  existing forced-progression path.
- **FR-009**: The HL7 and FHIR parsing logic MUST reuse bodhyanga's parsers where they exist
  (import unchanged, not reimplement) — `bodhyanga/pkg/hl7v2` for ADT; `bodhyanga/pkg/fhir` resource
  types for FHIR — with new code limited to (a) a FHIR *bundle* envelope decoder (which bodhyanga
  lacks) and (b) the mapping from bodhyanga's `medical.*` types to `agents.PatientCase`.
- **FR-010**: Automated tests MUST cover the happy path and every rejection category using
  fixture messages (HL7 fixtures may reuse `bodhyanga/pkg/hl7v2/testdata`); no external EHR, FHIR
  server, or network is contacted (consistent with specs/001 deterministic-test rule).

### Key Entities

- **Inbound clinical message**: an opaque payload in one of the supported wire formats
  (HL7 v2 ADT text, FHIR R4 bundle JSON) plus a declared format; the raw material of the
  front-door. PHI-bearing.
- **Translation result**: either one `agents.PatientCase` (success) or a PHI-free
  *rejection reason category* (failure) — never both, never partial.
- **Rejection reason**: a member of a fixed enumeration (malformed / unsupported-type /
  unsupported-trigger / missing-segment-or-resource / ambiguous-subject); the only thing about
  a failed message that reaches the trail beyond an optional fingerprint.
- **PatientCase**: the *unchanged* pipeline entry type (`agents.PatientCase`); the front-door's
  sole output on success. This feature adds no field to it.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A well-formed ADT^A01 message runs end-to-end to a care plan through the
  unchanged pipeline graph, with a translation admission record preceding intake on the trail.
- **SC-002**: A well-formed FHIR R4 bundle runs end-to-end to a care plan, and its trail from
  the intake hop onward is structurally identical to the same clinical facts fed as ADT.
- **SC-003**: 100% of the four canonical bad inputs (malformed HL7, unsupported trigger,
  missing segment, malformed/empty FHIR) are rejected with a distinct PHI-free reason and
  produce zero pipeline runs.
- **SC-004**: An automated scan of every audit record produced by the front-door (success and
  rejection alike) finds zero raw MRNs, zero patient names, and zero clinical narrative;
  identifiers appear only as fingerprints.
- **SC-005**: Source format is invisible past the front-door: the same case fed as JSON, HL7,
  and FHIR yields structurally identical trails from intake onward (verified with the specs/001
  trail-invariant checker).
- **SC-006**: Zero edits to the pipeline graph (`pkg/pipeline/pipeline.go`) and zero fields
  added to `agents.PatientCase` — the front-door is purely additive.

## Assumptions

- bodhyanga's `pkg/hl7v2` and `pkg/fhir` are consumable unchanged via the existing
  `replace ../bodhyanga` directive; their only transitive bodhyanga deps are `pkg/llm` and `pkg/medical`,
  which are already reachable (verified with `go list -deps`).
- The slice remains single-process and single-patient-per-case; batch ingestion, multi-patient
  bundles, and a live FHIR/HL7 *network listener* are out of scope (the front-door is a
  library + a CLI flag reading a file, not a server).
- bodhyanga's parsers map to `medical.ADTEvent` / `medical.PatientRecord`, **not** to
  `agents.PatientCase`; the semantic mapping (diagnosis/condition → symptom/history) is a
  deliberate, testable adaptation local to this feature, acknowledged as demo-grade clinical
  fidelity (consistent with the whole repo's research-not-clinical stance).
- The mapping from a coded diagnosis/condition to the `Symptoms`/`History` string slices is
  intentionally simple (code or display text as one entry) for the slice; richer terminology
  mapping is a non-goal.
- specs/001's admission boundary, audit bridge, and trail-invariant checker exist and are the
  authority this feature plugs into, not re-solves.

## Non-Goals

- No network listener / MLLP server / FHIR REST endpoint — input is a file or byte payload.
- No multi-patient bundles, no HL7 batch (BHS/FHS) messages, no ORU/ORM ingestion (ADT only).
- No FHIR write-back, no ACK/NACK transport semantics beyond an in-process rejection reason.
- No new field on `agents.PatientCase` and no change to the pipeline graph or any specs/001
  package's behavior.
- No richer clinical-terminology mapping (SNOMED/ICD normalization) beyond code-or-text.

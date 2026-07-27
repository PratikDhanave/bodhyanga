# Feature Specification: Slice Safety Boundary

**Feature Branch**: `feat/slice-safety-boundary`

**Created**: 2026-07-12

**Status**: Draft

**Input**: User description: "Complete the bodh-maf MARA vertical slice: admission-controlled, auditable, resumable clinical pipeline proving MAF-go can carry bodh's safety boundary — fail-closed governance / fail-open audit admission on every hop, audit bridge with synchronous deny writes, physician-review HITL gate with durable checkpointing surviving restart, swappable rule/LLM diagnostician behind a flag, and terminal-hop evaluation records closing the eval-mirror gap. Success = the five exit criteria in README.md, each proven by a test."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Every agent action is admission-controlled (Priority: P1)

As a clinical-platform safety steward, I need every action any pipeline agent attempts —
on every hop of every case — to first pass a policy decision, so that a misconfigured,
demoted, or compromised agent can never touch a case. When policy says no (or cannot
answer), the action must not happen, and the attempt must be on the record.

**Why this priority**: This is the safety boundary the entire evaluation exists to prove.
If admission fidelity fails, the platform migration is a no-go regardless of everything else.

**Independent Test**: Configure a deny-all policy and submit a case: no agent work may
execute, the run must halt, and one denial record per attempted hop must exist. Configure
an allow policy and the same case must complete.

**Acceptance Scenarios**:

1. **Given** an allow-all policy, **When** a case runs, **Then** it completes and every hop
   has an admission record preceding its execution record.
2. **Given** a deny-all policy, **When** a case is submitted, **Then** no agent processes it,
   the run terminates without producing a care plan, and each attempted hop has a denial record.
3. **Given** a policy engine that errors or is unreachable, **When** an agent action is
   attempted, **Then** it is treated exactly as a denial (fail-closed).
4. **Given** a policy denying only one specific agent, **When** a case runs, **Then** upstream
   agents execute normally and the case halts at the denied agent.

---

### User Story 2 - Complete, PHI-free audit trail per case (Priority: P2)

As a compliance auditor, I need the trail for any case to show every consequential moment —
admission decisions, agent executions, failures, review requests and decisions, pauses,
completions — and I need certainty that no raw patient identifier or clinical narrative
appears anywhere in that trail.

**Why this priority**: The audit trail is the regulatory backbone (HIPAA §164.312(b)); it is
only trustworthy if it is complete for denials and clean of PHI. It builds directly on P1's
records.

**Independent Test**: Run one case to completion and independently verify (a) a
denial-inclusive, ordered set of expected record types exists and (b) a scan of every
record field finds zero raw identifiers and zero clinical content.

**Acceptance Scenarios**:

1. **Given** a completed case, **When** its trail is read back, **Then** it contains records
   for every hop's admission and completion, the review request and decision, and case
   finalization — in a coherent order.
2. **Given** any recorded event, **When** its fields are inspected, **Then** patient
   identifiers appear only in shortened-fingerprint form and no clinical content is present.
3. **Given** the audit sink fails while care is in progress, **When** an agent action is
   admitted, **Then** care flow continues (fail-open) and the audit failure itself is
   surfaced loudly to operators.
4. **Given** a denial occurs, **When** the denial is recorded, **Then** the record is written
   before the run proceeds, so a lagging observer can never miss it.

---

### User Story 3 - Physician review survives a restart (Priority: P3)

As a reviewing physician, when a diagnosis reaches me for approval I may not respond for
hours — and the platform may be redeployed meanwhile. My pending review, and the case
behind it, must survive process death: when the system returns, the case is exactly where
it was, my approval or rejection still lands, and the outcome is recorded.

**Why this priority**: Human oversight is mandatory for clinical AI; oversight that loses
state on restart is oversight in name only. This is the hardest technical risk in the
migration and needs P1/P2's boundary in place to be meaningful.

**Independent Test**: Pause a case at review, kill the process, restart from durable
storage, deliver the physician's decision, and observe the correct terminal outcome plus a
complete trail spanning both process lifetimes.

**Acceptance Scenarios**:

1. **Given** a diagnosis above the confidence bar, **When** it reaches the review gate,
   **Then** the case pauses and a review request is surfaced and recorded.
2. **Given** a paused case, **When** the physician approves, **Then** a care plan is produced
   and the approval is on the record.
3. **Given** a paused case, **When** the physician rejects, **Then** no care plan is produced
   from that diagnosis and the rejection is on the record.
4. **Given** a paused case and a full process termination, **When** the system restarts and
   resumes from durable state, **Then** the pending review is re-surfaced and the physician's
   decision completes the case as if no restart had occurred.

---

### User Story 4 - Diagnostic engine is a configuration choice (Priority: P4)

As a clinical-platform operator, I choose per deployment whether the diagnostician is the
deterministic rule engine or an AI model (hosted or local) — by configuration only. The
rest of the pipeline, the safety boundary, and the output format must be identical either way.

**Why this priority**: Provider-pluggability is an existing platform guarantee
(constitution I/II) that the migration must preserve; it matters only once the boundary
(P1–P3) exists to hold it to.

**Independent Test**: Run the same case once per engine configuration; verify identical
output structure, identical trail shape, and zero workflow definition changes between runs.

**Acceptance Scenarios**:

1. **Given** the rule engine is configured, **When** a case runs, **Then** it completes with
   a structured diagnosis and care plan.
2. **Given** an AI engine is configured, **When** the same case runs, **Then** the output has
   the identical structure and the same downstream flow (review gate, care plan, records).
3. **Given** an AI engine that returns malformed or unparseable output, **When** the
   diagnosis hop runs, **Then** the failure is recorded and the case does not silently
   proceed with a fabricated diagnosis.
4. **Given** automated tests, **When** they exercise the AI engine path, **Then** no real
   external AI service is called (deterministic stand-ins only).

---

### User Story 5 - Every finished case yields an evaluation record (Priority: P5)

As a quality-and-evaluation lead, I need each case that reaches an outcome to produce one
evaluation record capturing what was decided and how it got there, so that quality metrics
and case-status reporting reflect reality. Today, a known gap means decoupled deployments
stop producing these records and finished cases report as "pending" forever; this feature
must close that gap.

**Why this priority**: Valuable for platform go/no-go evidence, but it depends on all prior
stories and its absence doesn't endanger patients in the slice.

**Independent Test**: Complete one case and verify exactly one evaluation record exists with
the case's outcome, rounds, confidence, and review decision; verify case status reports
finalized, not pending.

**Acceptance Scenarios**:

1. **Given** a case completes with an approved care plan, **When** evaluation storage is
   read, **Then** exactly one record exists for the case with outcome, confidence, round
   count, and review decision.
2. **Given** a case is rejected at review, **When** evaluation storage is read, **Then** the
   record reflects the rejection outcome.
3. **Given** a case forced forward at low confidence, **When** its record is read, **Then**
   the forced-progression fact is captured.

---

### Edge Cases

- Policy engine unreachable or erroring → treated as denial on that hop (fail-closed); the
  case halts rather than proceeding unchecked.
- Audit sink down mid-case → admitted care actions continue (fail-open) with loud operator
  signal; denial records are still written synchronously with the denial itself.
- Duplicate audit records are tolerable (observers may re-emit); missing denial records are
  never tolerable.
- Process dies between the diagnosis and the review request being surfaced → on resume, the
  pending request is re-surfaced, not lost and not duplicated as a second review.
- Physician responds to a review request that no longer exists (already answered, or case
  restarted from an older checkpoint) → the stale response is rejected and recorded, not
  silently applied.
- AI engine timeout or malformed output → recorded failure, no fabricated diagnosis, case
  does not advance on fabricated content.
- Low-confidence loop exhausts its round budget → forced progression is flagged in the
  diagnosis, the trail, and the evaluation record (already built; must remain true under
  the new boundary).
- A case with symptoms matching no known rule → proceeds as "undifferentiated" with forced
  progression; review gate still applies.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Every agent action on every hop MUST pass an admission decision before any
  agent logic executes; there is exactly one admission mechanism shared by all hops (no
  per-agent variants).
- **FR-002**: A denied or undecidable admission (policy error, engine unreachable) MUST
  prevent the agent action entirely and MUST terminate the case's forward progress on that
  path (fail-closed).
- **FR-003**: Every admission decision — allow or deny — MUST produce an audit record;
  denial records MUST be durably written synchronously with the denial.
- **FR-004**: Audit failures MUST NOT block admitted care actions (fail-open) and MUST be
  loudly surfaced to operators.
- **FR-005**: Audit records MUST never contain raw patient identifiers or clinical message
  content; identifiers appear only as shortened fingerprints.
- **FR-006**: The full lifecycle of a case — admissions, executions, failures, review
  request/decision, checkpoint saves, finalization — MUST be reconstructible from the audit
  trail alone.
- **FR-007**: A confident (or forced) diagnosis MUST pause the case at a physician review
  gate; only an explicit approval releases the care plan, and an explicit rejection
  terminates the case without one. Both outcomes are recorded.
- **FR-008**: A paused case MUST survive full process termination: durable state is
  sufficient to restart, re-surface the pending review, accept the decision, and complete —
  with the trail spanning both lifetimes.
- **FR-009**: Stale or duplicate review responses MUST be rejected and recorded, never
  silently applied.
- **FR-010**: The diagnostician MUST be selectable by configuration among: deterministic
  rules, a hosted AI model, and a locally served AI model — with structurally identical
  output and no change to the workflow definition.
- **FR-011**: AI-engine failures (timeout, malformed output) MUST be recorded and MUST NOT
  yield a fabricated diagnosis.
- **FR-012**: Automated tests MUST NOT call real external AI services (constitution II);
  the AI path is tested with deterministic stand-ins, and live-service tests are opt-in
  via explicit environment flags.
- **FR-013**: Every case reaching an outcome (approved, rejected, or failed terminal state)
  MUST produce exactly one evaluation record capturing outcome, confidence, round count,
  forced-progression flag, and review decision.
- **FR-014**: All five README exit criteria MUST each be proven by at least one automated
  test.

### Key Entities

- **Case**: one patient presentation moving through the pipeline; carries a fingerprinted
  identity everywhere outside the clinical payload.
- **Admission decision**: the allow/deny verdict for one agent action on one hop; the atom
  of the safety boundary.
- **Audit record**: one appended, immutable statement of something that happened to a case;
  PHI-free by construction.
- **Review request / review decision**: the pause point surfaced to a physician and their
  explicit approval or rejection; at most one active request per case.
- **Checkpoint**: the durable snapshot from which a case (including a pending review) can be
  resumed after process death; lineage-ordered.
- **Diagnosis result**: the structured output of any diagnostic engine — same shape whether
  rules or AI produced it; includes confidence, rounds, forced flag, evidence.
- **Evaluation record**: the one-per-case quality record written at the terminal hop.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: With a deny-all policy, 0% of agent actions execute and 100% of attempted hops
  have a denial record; with an allow policy, the same case completes end-to-end.
- **SC-002**: For a completed case, an independent reader reconstructs the full lifecycle
  from the trail alone, and an automated scan of every record finds zero raw identifiers
  and zero clinical content.
- **SC-003**: 100% of cases paused at physician review are recoverable across full process
  termination: the decision still lands and the case reaches its correct terminal state.
- **SC-004**: Switching diagnostic engines is a configuration-only change (zero workflow
  definition edits), verified by running the same case on at least two engines with
  structurally identical outputs and trails.
- **SC-005**: 100% of finished cases have exactly one evaluation record, and none report a
  perpetual "pending" status.
- **SC-006**: All five README exit criteria pass in the automated test suite on every run
  (no opt-in flags required for the proof).

## Assumptions

- The parent platform's governance, audit, and evaluation capabilities are consumable as
  libraries from this codebase (already wired via a local module reference) and their
  semantics — not reimplementations — are the compliance authority.
- Durable storage (PostgreSQL) is available for checkpoint and trail persistence; automated
  tests provision it ephemerally (containerized), consistent with the parent platform's
  test conventions.
- The slice remains a single-process deployment; distribution across processes is
  explicitly out of scope for this feature.
- The existing step-1 pipeline (four agents, loop-back, forced progression) is the substrate
  this feature hardens; its behavior under the new boundary must not regress.
- Hosted-AI credentials and a local model server exist for manual/live verification but are
  never required by the automated suite.
- The physician "inbox" is the process operator surface (CLI/programmatic) for this slice;
  a clinical UI is out of scope.

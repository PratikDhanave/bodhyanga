# Phase 1 Data Model: Slice Safety Boundary

Existing step-1 types (`pkg/agents/types.go`: PatientCase, DiagnosticQuery, Workup,
DiagnosisResult, CarePlan) are unchanged. This feature adds boundary/persistence
entities and pins the mappings between them.

## Hop envelope (pkg/admit)

Synthesized per hop for governance + audit; never carries payload.

| Field | Source | PHI? |
|---|---|---|
| `protocol.Message.From` | sending executor ID / "operator" | no |
| `.To` | target executor ID | no |
| `.Type` | slice label: `patient_case` \| `diagnostic_query` \| `workup` \| `diagnosis_result` \| `care_plan` | no |
| `.Metadata["case_id"]` | opaque case token (post-de-id) | no (opaque) |
| `.Content` | always `""` | — |

**Validation**: admit rejects (denies) a hop whose payload type has no label — an
unlabeled message type is a design error, and fail-closed is the safe default.

## Audit record usage (bodh audit.Event — schema unchanged)

| Moment | Kind | Decision | Notes |
|---|---|---|---|
| admission allow | `policy_decision` | `allow` | AgentID = target executor; synchronous |
| admission deny / policy error | `policy_decision` | `deny` | Reason from PolicyResult or `policy_error:<class>`; synchronous with denial |
| hop completed | `message` | — | from auditbridge (async, dedup-tolerant) |
| handler / LLM failure | `agent_error` | — | ErrorText = err.Error(), never payload |
| review requested | `message` | — | MessageID = ReviewRequest.ID (resume handle) |
| review decided | `review_decision` | `approve` \| `reject` | ReviewerID mandatory |
| stale review response | `review_decision` | `reject` | Reason = `stale_response` |
| checkpoint saved | `message` | — | Type = `checkpoint`; MessageID = checkpoint ID |
| case finalized | `message` | — | Type = `case_finalized` |

**Invariants**: `CaseID` always the opaque token; `ErrorText` never from message
content; deny records precede run-halt observably (write returns before ErrDenied).

## ReviewRequest / ReviewDecision (pkg/pipeline + RequestPort)

| Entity | Fields | Rules |
|---|---|---|
| ReviewRequest | `ID` (uuid), `CaseToken`, `Diagnosis`, `Confidence`, `Forced`, `Rounds` | at most one active per case; carried as the RequestPort request type |
| ReviewDecision | `RequestID`, `Approved` (bool), `ReviewerID`, `Note` | `RequestID` must match the pending request, else stale → rejected+audited; `ReviewerID` required (21 USC §360j(o) anchor per bodh audit doc) |

**State transitions**: `diagnosed → awaiting_review → approved|rejected`;
`awaiting_review` survives process death (checkpoint); a resumed run re-emits the SAME
ReviewRequest.ID (republish, not re-create).

## Checkpoint row (pkg/ckptpg, db/0001_checkpoints.sql)

```sql
CREATE TABLE checkpoints (
    id         UUID PRIMARY KEY,
    session_id TEXT        NOT NULL,
    parent_id  UUID        NULL REFERENCES checkpoints(id),
    data       JSONB       NOT NULL,   -- MAF checkpoint payload; PHI-bearing
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX checkpoints_session_idx ON checkpoints (session_id, created_at);
-- append-only: application role gets INSERT+SELECT only (bodh discipline)
```

Maps 1:1 onto MAF `checkpoint.Store[json.RawMessage]`:
`CreateCheckpoint` → INSERT; `RetrieveCheckpoint` → SELECT by (session_id, id);
`RetrieveIndex` → SELECT ordered by created_at, optionally filtered by parent.
`data` is never logged or exposed; lineage (`parent_id`) supports time-travel/restore.

## Evaluation record (bodh eval.InteractionRecord — schema unchanged)

| Field | Value |
|---|---|
| `ID` | uuid |
| `Scenario` | `mara-slice/<case-token>` |
| `Success` | reviewed-approved AND NOT forced |
| `Metrics` | `confidence`, `rounds`, `forced` (0/1) |
| `Metadata` | `review_decision`, `reviewer_id`, `diagnostician` (rule/llm-*), `diagnosis` |
| `StartedAt/EndedAt` | run start / OutputEvent time |

**Invariant**: exactly one record per case reaching a terminal state (approved,
rejected, or terminal failure); rejected cases have `Success=false`,
`Metadata.review_decision="reject"`.

## Diagnostician output (shared shape — FR-010)

Both engines emit `agents.DiagnosisResult`. The LLM path enforces it via structured
output + post-validation: `Confidence ∈ [0,1]`, non-empty `Diagnosis`, `CaseID` echoed
from the Workup (never trusted from the model — overwritten from input). Validation
failure ⇒ `agent_error`, no result (FR-011).

# Contract: Admission & Physician Review

## pkg/admit

```go
// Boundary is the one admission implementation (FR-001).
type Boundary struct { /* governance.Policy, audit.Recorder, logger */ }

func New(pol governance.Policy, rec audit.Recorder, log *slog.Logger) *Boundary

// Wrap decorates an executor handler. Semantics:
//  1. build PHI-free envelope (data-model.md) for hop agentID/msgType
//  2. pol.Evaluate: non-allow OR error ⇒ record deny (SYNCHRONOUS) ⇒ return ErrDenied
//  3. record allow; call inner handler
//  4. inner error ⇒ record agent_error; propagate
func Wrap[In, Out any](b *Boundary, agentID, msgType string,
    h func(*workflow.Context, In) (Out, error)) func(*workflow.Context, In) (Out, error)

// Middleware guards an LLM agent's provider run with the same steps 1–3.
func (b *Boundary) Middleware(agentID string) agent.Middleware

var ErrDenied = errors.New("admit: denied by governance")
```

**Guarantees**
- Fail-closed: policy error ≡ deny. No handler code executes after a deny.
- Deny audit write completes before Wrap returns ErrDenied (observable ordering).
- Audit write failure never changes control flow; it logs at ERROR.
- Envelope never contains payload; unlabeled message types are denied.

**Error taxonomy**: `ErrDenied` (halts the path; surfaces as ExecutorFailedEvent) ·
inner handler errors pass through unchanged · audit errors are swallowed-but-logged.

## Physician review (RequestPort)

- Request type: `ReviewRequest{ID, CaseToken, Diagnosis, Confidence, Forced, Rounds}`
- Response type: `ReviewDecision{RequestID, Approved, ReviewerID, Note}`
- Port ID: `physician_review`

**Guarantees**
- Every DiagnosisResult (confident or forced) traverses the port — no bypass edge.
- Approve ⇒ care_plan hop runs (still admission-guarded). Reject ⇒ case terminates
  without a care plan; terminal state is `rejected`.
- `ReviewDecision.RequestID ≠ pending request` ⇒ decision is discarded, audited as
  `review_decision/reject/stale_response`; the pending request stays pending.
- `ReviewerID == ""` ⇒ treated as stale (invalid) — an anonymous approval never lands.
- After resume-from-checkpoint, the SAME request ID is republished.

## CLI surface (cmd/slice)

```
-policy       allow|deny|deny-agent=<id>     (demo policies; default allow)
-diagnostician rule|llm-anthropic|llm-ollama (default rule)
-db           postgres://... (enables durable checkpointing)
-resume       <session-id>   (resume a paused run; requires -db)
-approve/-reject + -reviewer <id>            (answer a pending review)
```

Exit codes: 0 completed · 2 denied by governance · 3 awaiting review (paused) ·
1 any other failure.

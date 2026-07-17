package pipeline

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/microsoft/agent-framework-go/workflow"
	"github.com/microsoft/agent-framework-go/workflow/checkpoint"
	"github.com/microsoft/agent-framework-go/workflow/inproc"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/auditbridge"
	"github.com/PratikDhanave/bodh-maf/pkg/evalhook"
)

// Runner owns one case's execution: it runs the workflow, feeds the audit
// bridge, answers (or parks) physician-review requests, and reduces the
// event stream to an Outcome. cmd/slice and the integration tests share it
// so the trail is identical in both.
type Runner struct {
	wf    *workflow.Workflow
	cfg   Config
	rec   audit.Recorder
	mgr   checkpoint.Manager
	index func(ctx context.Context, sessionID string, withParent *workflow.CheckpointInfo) ([]workflow.CheckpointInfo, error)
	// Responder answers external review requests mid-run. nil parks the
	// request: the run pauses (Outcome.Paused) and survives via checkpoint.
	Responder func(*workflow.ExternalRequest) (*workflow.ExternalResponse, error)
}

// Outcome is what one run observably produced.
type Outcome struct {
	Plan      *agents.CarePlan
	Rejected  *agents.CaseRejected
	Denied    bool
	Paused    bool
	SessionID string
	Forced    []agents.ForcedProgressionEvent
	Failures  []workflow.ExecutorFailedEvent
	Events    []workflow.Event
	// Eval is the terminal-hop record written to Config.EvalStore (US5).
	Eval *eval.InteractionRecord
}

// NewRunner builds the workflow from cfg and pairs it with the audit sink.
func NewRunner(cfg Config, rec audit.Recorder) (*Runner, error) {
	wf, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return &Runner{wf: wf, cfg: cfg, rec: rec}, nil
}

// WithCheckpointStore enables durable state + cross-process Resume backed by
// a checkpoint store (pkg/ckptpg's Postgres store, or MAF's file store). The
// MAF Manager interface is opaque, so the store doubles as the index used to
// find a session's latest checkpoint.
func (r *Runner) WithCheckpointStore(store checkpoint.Store[json.RawMessage]) *Runner {
	r.mgr = checkpoint.NewJSONManager(store)
	r.index = store.RetrieveIndex
	return r
}

// CaseToken returns the opaque fingerprint the trail is keyed on — computed
// on the post-de-identification case ID so every hop agrees.
func (r *Runner) CaseToken(pc agents.PatientCase) string {
	if r.cfg.Deidentify != nil {
		pc = r.cfg.Deidentify(pc)
	}
	return agents.HashCaseID(pc.CaseID)
}

func (r *Runner) env() *inproc.ExecutionEnvironment {
	if r.mgr != nil {
		return inproc.Default.WithCheckpointing(r.mgr)
	}
	return inproc.Default
}

// Run executes one case until terminal state, denial, or pause-for-review.
func (r *Runner) Run(ctx context.Context, pc agents.PatientCase, sessionID string) (*Outcome, error) {
	var opts []inproc.ExecutionOption
	if sessionID != "" {
		opts = append(opts, inproc.WithSessionID(sessionID))
	}
	run, err := r.env().RunStreaming(ctx, r.wf, pc, opts...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = run.Close(ctx) }()
	return r.drain(ctx, run, r.CaseToken(pc))
}

// RunInbound executes one case starting from a raw Inbound message, driving
// the translation front door (Config.Translate must be true). The case token
// is unknown until translation succeeds, so — like Resume — the audit bridge
// learns it from the first executor invocation rather than the input.
func (r *Runner) RunInbound(ctx context.Context, in agents.Inbound, sessionID string) (*Outcome, error) {
	var opts []inproc.ExecutionOption
	if sessionID != "" {
		opts = append(opts, inproc.WithSessionID(sessionID))
	}
	run, err := r.env().RunStreaming(ctx, r.wf, in, opts...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = run.Close(ctx) }()
	return r.drain(ctx, run, "")
}

// Resume restarts a paused session from its latest checkpoint — typically in
// a NEW process after the original died. The pending review request is
// republished by MAF; the Responder (if set) answers it.
func (r *Runner) Resume(ctx context.Context, sessionID string) (*Outcome, error) {
	if r.mgr == nil || r.index == nil {
		return nil, errors.New("pipeline: Resume requires WithCheckpointStore")
	}
	infos, err := r.index(ctx, sessionID, nil)
	if err != nil {
		return nil, fmt.Errorf("pipeline: list checkpoints for %s: %w", sessionID, err)
	}
	if len(infos) == 0 {
		return nil, fmt.Errorf("pipeline: no checkpoints for session %s", sessionID)
	}
	run, err := r.env().ResumeStreaming(ctx, r.wf, infos[len(infos)-1])
	if err != nil {
		return nil, err
	}
	defer func() { _ = run.Close(ctx) }()
	// The case token is inside the checkpointed payload; the bridge learns it
	// from the first executor invocation instead of the original input.
	return r.drain(ctx, run, "")
}

func (r *Runner) drain(ctx context.Context, run *inproc.StreamingRun, caseToken string) (*Outcome, error) {
	started := time.Now().UTC()
	bridge := auditbridge.New(r.rec, caseToken)
	out := &Outcome{SessionID: run.SessionID()}
	defer func() { r.recordEval(out, started) }()
	for evt, err := range run.WatchStream(ctx) {
		if err != nil {
			return out, err
		}
		bridge.Handle(evt)
		out.Events = append(out.Events, evt)
		switch e := evt.(type) {
		case agents.ForcedProgressionEvent:
			out.Forced = append(out.Forced, e)
		case workflow.RequestInfoEvent:
			if r.Responder == nil {
				// Park the request. WatchStream stays open awaiting a
				// response, so the runner must stop consuming itself — but
				// only after the checkpoint that captures the pending
				// request has committed (the same super-step's completion
				// event), or immediately when nothing durable exists.
				out.Paused = true
				if r.mgr == nil {
					return out, nil
				}
				continue
			}
			resp, err := r.Responder(e.Request)
			if err != nil {
				return out, err
			}
			if err := run.SendResponse(ctx, resp); err != nil {
				return out, err
			}
			out.Paused = false
		case workflow.SuperStepCompletedEvent:
			if out.Paused && e.CompletionInfo != nil && e.CompletionInfo.CheckpointInfo != nil {
				// Pending review is durably checkpointed — safe to detach.
				return out, nil
			}
		case workflow.ExecutorFailedEvent:
			out.Failures = append(out.Failures, e)
			if errors.Is(e.Error, admit.ErrDenied) {
				out.Denied = true
			}
		case workflow.ErrorEvent:
			// A denial at the start executor surfaces as a workflow-level
			// error (input processing halts) — that is a governed outcome,
			// not a runner failure.
			if errors.Is(e.Error, admit.ErrDenied) {
				out.Denied = true
				continue
			}
			return out, e.Error
		case workflow.OutputEvent:
			switch o := e.Output.(type) {
			case agents.CarePlan:
				out.Plan = &o
			case agents.CaseRejected:
				out.Rejected = &o
			}
		}
	}
	return out, nil
}

// recordEval writes the one-per-terminal-case eval record (US5, closing
// bodh's eval-mirror gap). Eval storage is quality telemetry: failures are
// logged by the store's owner, never fatal to care flow.
func (r *Runner) recordEval(out *Outcome, started time.Time) {
	if r.cfg.EvalStore == nil || (out.Plan == nil && out.Rejected == nil) {
		return
	}
	o := evalhook.CaseOutcome{
		Diagnostician: cmp.Or(r.cfg.EngineName, "rule"),
		StartedAt:     started,
		EndedAt:       time.Now().UTC(),
	}
	// The diagnosis under decision and the reviewer's verdict ride the event
	// stream as executor invocations — recover them from there.
	for _, evt := range out.Events {
		inv, ok := evt.(workflow.ExecutorInvokedEvent)
		if !ok {
			continue
		}
		switch m := inv.Message.(type) {
		case agents.DiagnosisResult:
			o.CaseToken = agents.HashCaseID(m.CaseID)
			o.Diagnosis = m.Diagnosis
			o.Confidence = m.Confidence
			o.Rounds = m.Rounds
			o.Forced = m.Forced
		case agents.ReviewDecision:
			o.ReviewerID = m.ReviewerID
		}
	}
	switch {
	case out.Rejected != nil:
		o.Rejected = true
		o.ReviewerID = out.Rejected.ReviewerID
	case out.Plan != nil:
		o.Approved = true
		if o.CaseToken == "" {
			o.CaseToken = agents.HashCaseID(out.Plan.CaseID)
		}
	}
	rec, err := evalhook.Record(r.cfg.EvalStore, o)
	if err != nil {
		return
	}
	out.Eval = &rec
}

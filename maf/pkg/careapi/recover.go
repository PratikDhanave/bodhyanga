package careapi

import (
	"context"

	"github.com/microsoft/agent-framework-go/workflow"
)

// checkpointIndex is the subset of pkg/ckptpg.Store the recovery path needs:
// confirm a session has durable checkpoints. It matches Store.RetrieveIndex
// (ckptpg/store.go:79).
type checkpointIndex interface {
	RetrieveIndex(ctx context.Context, sessionID string, withParent *workflow.CheckpointInfo) ([]workflow.CheckpointInfo, error)
}

// RecoverPending rebuilds awaiting_review registry entries after a restart.
//
// The checkpoint store has no cross-session "list sessions with a pending
// request" query, so the API MUST persist the set of session ids it started
// (its callers own that set — here the keys of `known`). For each, RecoverPending
// confirms durable checkpoints exist and re-materializes the pending-review row
// from the last-known Entry the caller supplies. This is the scope boundary in
// plan.md risk 2: recovery is only as complete as the session-id set the API
// remembered. A session with no checkpoints is skipped (it never durably paused).
func RecoverPending(ctx context.Context, store checkpointIndex, reg Registry, known map[string]Entry) error {
	for sessionID, entry := range known {
		infos, err := store.RetrieveIndex(ctx, sessionID, nil)
		if err != nil {
			return err
		}
		if len(infos) == 0 {
			continue
		}
		entry.SessionID = sessionID
		entry.Status = StatusAwaitingReview
		reg.Put(entry)
	}
	return nil
}

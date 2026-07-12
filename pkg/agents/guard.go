package agents

import "context"

// Guard is the hop-admission hook (architecture.md §3). pkg/admit's
// Boundary.Guard satisfies it; nil means unguarded (step-1 behavior, tests).
// It is defined here as a bare func type so agent definitions do not import
// the boundary package.
type Guard func(ctx context.Context, agentID, msgType, caseToken string, run func() error) error

// guarded runs fn under g, or directly when g is nil.
func guarded(g Guard, ctx context.Context, agentID, msgType, caseToken string, fn func() error) error {
	if g == nil {
		return fn()
	}
	return g(ctx, agentID, msgType, caseToken, fn)
}

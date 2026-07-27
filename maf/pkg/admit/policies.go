package admit

import (
	"context"
	"errors"
	"time"

	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// Demo policies for the slice CLI and tests. Real deployments compose bodh's
// policy stack (CompositePolicy, kill switches, OPA) — these exist so the
// boundary can be exercised without clinical policy configuration.

type policyFunc func(ctx context.Context, msg protocol.Message) (governance.PolicyResult, error)

func (f policyFunc) Evaluate(ctx context.Context, msg protocol.Message) (governance.PolicyResult, error) {
	return f(ctx, msg)
}

// AllowAll admits every hop.
func AllowAll() governance.Policy {
	return policyFunc(func(context.Context, protocol.Message) (governance.PolicyResult, error) {
		return governance.PolicyResult{
			Decision: governance.DecisionAllow, Reason: "allow_all",
			CheckedAt: time.Now().UTC(),
		}, nil
	})
}

// DenyAll denies every hop.
func DenyAll() governance.Policy {
	return policyFunc(func(context.Context, protocol.Message) (governance.PolicyResult, error) {
		return governance.PolicyResult{
			Decision: governance.DecisionDeny, Reason: "deny_all",
			CheckedAt: time.Now().UTC(),
		}, nil
	})
}

// DenyAgent denies hops targeting one agent and allows everything else.
func DenyAgent(agentID string) governance.Policy {
	return policyFunc(func(_ context.Context, msg protocol.Message) (governance.PolicyResult, error) {
		if msg.To == agentID {
			return governance.PolicyResult{
				Decision: governance.DecisionDeny, Reason: "deny_agent:" + agentID,
				CheckedAt: time.Now().UTC(),
			}, nil
		}
		return governance.PolicyResult{
			Decision: governance.DecisionAllow, Reason: "allow_all",
			CheckedAt: time.Now().UTC(),
		}, nil
	})
}

// Erroring simulates a broken policy engine — the boundary must treat it as
// a denial (fail-closed).
func Erroring() governance.Policy {
	return policyFunc(func(context.Context, protocol.Message) (governance.PolicyResult, error) {
		return governance.PolicyResult{}, errors.New("policy engine unavailable")
	})
}

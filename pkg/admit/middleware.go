package admit

import (
	"context"
	"iter"

	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/message"
)

type caseTokenKey struct{}

// WithCaseToken attaches the opaque case fingerprint to a context so the
// middleware can stamp envelopes/audit for the run it guards.
func WithCaseToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, caseTokenKey{}, token)
}

// CaseTokenFrom returns the attached token ("" when absent).
func CaseTokenFrom(ctx context.Context) string {
	t, _ := ctx.Value(caseTokenKey{}).(string)
	return t
}

// Middleware guards an LLM agent's provider run with the identical
// evaluate/record semantics as Guard — defense-in-depth on the actual model
// call, in addition to the executor-level Guard around the hop.
func (b *Boundary) Middleware(agentID, msgType string) agent.Middleware {
	return agent.MiddlewareFunc(func(next agent.RunFunc, ctx context.Context, msgs []*message.Message, opts ...agent.Option) iter.Seq2[*agent.ResponseUpdate, error] {
		if err := b.Admit(ctx, agentID, msgType, CaseTokenFrom(ctx)); err != nil {
			return func(yield func(*agent.ResponseUpdate, error) bool) {
				yield(nil, err)
			}
		}
		return next(ctx, msgs, opts...)
	})
}

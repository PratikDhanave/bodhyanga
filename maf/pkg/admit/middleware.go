package admit

import (
	"context"
	"iter"

	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/message"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/agents"
)

// WithCaseToken / CaseTokenFrom live in pkg/agents (the LLM executor sets
// them; agents cannot import admit). Re-exported here for callers that only
// know the boundary package.
var (
	WithCaseToken = agents.WithCaseToken
	CaseTokenFrom = agents.CaseTokenFrom
)

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

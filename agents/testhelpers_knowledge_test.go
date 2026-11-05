package agents

import (
	"context"
	"errors"

	"github.com/PratikDhanave/bodhyanga/pkg/knowledge"
)

// fakeKnowledge is a stub knowledge.Store for tests. Set Resp / Err to control
// Search behaviour; SearchCalls records every (query, k) pair the agent issued.
type fakeKnowledge struct {
	Resp []knowledge.Citation
	Err  error

	SearchCalls []fakeKnowledgeCall
}

type fakeKnowledgeCall struct {
	Query string
	K     int
}

// Index is a no-op for the stub.
func (f *fakeKnowledge) Index(_ context.Context, _ []knowledge.Document) error {
	return nil
}

// Search records the call and returns the canned response or error.
func (f *fakeKnowledge) Search(_ context.Context, query string, k int) ([]knowledge.Citation, error) {
	f.SearchCalls = append(f.SearchCalls, fakeKnowledgeCall{Query: query, K: k})
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Resp, nil
}

// Size reports the canned response length.
func (f *fakeKnowledge) Size() int { return len(f.Resp) }

// errSearch is a convenience constructor used in negative-path tests.
var errSearch = errors.New("knowledge: simulated search failure")

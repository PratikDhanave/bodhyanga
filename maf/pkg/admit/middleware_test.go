package admit

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/message"
)

// fakeAgent builds a MAF agent whose provider replies with fixed text,
// guarded by the boundary's middleware.
func fakeAgent(b *Boundary) *agent.Agent {
	run := func(ctx context.Context, msgs []*message.Message, opts ...agent.Option) iter.Seq2[*agent.ResponseUpdate, error] {
		return func(yield func(*agent.ResponseUpdate, error) bool) {
			yield(&agent.ResponseUpdate{
				Contents: []message.Content{&message.TextContent{Text: "ok"}},
			}, nil)
		}
	}
	return agent.New(agent.ProviderConfig{
		ProviderName: "fake",
		Run:          run,
	}, agent.Config{
		ID:          "diagnostician-llm",
		Middlewares: []agent.Middleware{b.Middleware("diagnostician", TypeWorkup)},
	})
}

func TestMiddlewareDenyShortCircuitsProviderRun(t *testing.T) {
	rec := &capturingRecorder{}
	a := fakeAgent(New(DenyAll(), rec, nil))

	ctx := WithCaseToken(context.Background(), "abcd1234")
	_, err := a.RunText(ctx, "diagnose this workup").Collect()
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("run error = %v, want ErrDenied", err)
	}
	evs := rec.List()
	if len(evs) == 0 || evs[0].Decision != "deny" || evs[0].CaseID != "abcd1234" {
		t.Fatalf("provider-run denial not audited with case token: %+v", evs)
	}
}

func TestMiddlewareAllowPassesThrough(t *testing.T) {
	rec := &capturingRecorder{}
	a := fakeAgent(New(AllowAll(), rec, nil))

	resp, err := a.RunText(WithCaseToken(context.Background(), "abcd1234"), "diagnose").Collect()
	if err != nil {
		t.Fatalf("run error = %v, want nil", err)
	}
	if resp == nil {
		t.Fatal("no response from allowed provider run")
	}
	if evs := rec.List(); len(evs) == 0 || evs[0].Decision != "allow" {
		t.Fatalf("allowed provider run not audited: %+v", evs)
	}
}

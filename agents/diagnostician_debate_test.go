package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// scriptedClient returns canned (response, err) pairs in order. Used so the
// debate tests can give each LLM call (N specialists + 1 chair) a distinct
// answer without depending on the order in which specialists complete.
type scriptedClient struct {
	mu      sync.Mutex
	scripts []scriptedReply
	Calls   []llm.Request
}

type scriptedReply struct {
	resp *llm.Response
	err  error
}

func (c *scriptedClient) Provider() string { return "scripted" }
func (c *scriptedClient) Model() string    { return "scripted-model" }

func (c *scriptedClient) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Calls = append(c.Calls, req)
	if len(c.scripts) == 0 {
		return nil, errors.New("scriptedClient: no scripts left")
	}
	// Match the chair call last (it has no specialist-persona prefix). The
	// chair's system prompt starts with "You are the chair" — everything
	// else starts with "You are the <name> expert". So we hand out the
	// chair reply only when we see the chair prompt; otherwise dispense
	// specialist replies in order.
	isChair := strings.HasPrefix(req.System, "You are the chair")
	if isChair {
		// Chair reply is always the last script.
		s := c.scripts[len(c.scripts)-1]
		c.scripts = c.scripts[:len(c.scripts)-1]
		return s.resp, s.err
	}
	s := c.scripts[0]
	c.scripts = c.scripts[1:]
	return s.resp, s.err
}

func TestDebate_AllSpecialistsContributeChairSynthesises(t *testing.T) {
	_, encoded := newCaseStateJSON(t)

	specialists := []Specialist{
		{Name: "Cardiology", Description: "Heart stuff."},
		{Name: "Infectious Disease", Description: "Infections."},
		{Name: "Pulmonology", Description: "Lungs."},
	}

	cardioResp := `{"diagnosis":"Heart failure","confidence":0.6,"rationale":"Edema and BNP up."}`
	idResp := `{"diagnosis":"Community-acquired pneumonia","confidence":0.85,"rationale":"Infiltrate plus leukocytosis."}`
	pulmResp := `{"diagnosis":"CAP","confidence":0.8,"rationale":"RLL infiltrate on CXR."}`
	chairResp := `{"diagnosis":"Community-acquired pneumonia","confidence":0.9,"differential":["CAP","Heart failure"],"rationale":"Two of three specialists converge on CAP grounded in the RLL infiltrate and leukocytosis."}`

	client := &scriptedClient{
		scripts: []scriptedReply{
			{resp: &llm.Response{Content: cardioResp}},
			{resp: &llm.Response{Content: idResp}},
			{resp: &llm.Response{Content: pulmResp}},
			// Chair is pulled from the end of the slice.
			{resp: &llm.Response{Content: chairResp}},
		},
	}
	base := &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}}
	d := &DebateDiagnostician{Base: base, Specialists: specialists}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if got, want := len(client.Calls), len(specialists)+1; got != want {
		t.Fatalf("LLM call count = %d, want %d (N specialists + 1 chair)", got, want)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output, got %d", len(out))
	}
	if out[0].To != "reasoning_verifier" {
		t.Fatalf("To=%s, want reasoning_verifier", out[0].To)
	}
	if out[0].Type != "diagnosis_proposal" {
		t.Fatalf("Type=%s, want diagnosis_proposal", out[0].Type)
	}

	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if p.Diagnosis != "Community-acquired pneumonia" {
		t.Fatalf("final Diagnosis=%q, want chair's CAP", p.Diagnosis)
	}
	if p.Confidence != 0.9 {
		t.Fatalf("final Confidence=%v, want 0.9", p.Confidence)
	}

	// Each specialist call must have its persona prefix in the system prompt.
	personaSeen := map[string]bool{}
	for _, c := range client.Calls {
		for _, s := range specialists {
			if strings.Contains(c.System, "You are the "+s.Name+" expert.") {
				personaSeen[s.Name] = true
			}
		}
	}
	for _, s := range specialists {
		if !personaSeen[s.Name] {
			t.Errorf("specialist %q persona not seen in any LLM call", s.Name)
		}
	}

	// Chair call must have listed each specialist by name in its user message.
	var chairCall *llm.Request
	for i := range client.Calls {
		if strings.HasPrefix(client.Calls[i].System, "You are the chair") {
			chairCall = &client.Calls[i]
			break
		}
	}
	if chairCall == nil {
		t.Fatalf("no chair call found")
	}
	if len(chairCall.Messages) == 0 {
		t.Fatalf("chair call has no messages")
	}
	chairUser := chairCall.Messages[0].Content
	for _, s := range specialists {
		if !strings.Contains(chairUser, s.Name) {
			t.Errorf("chair user message missing specialist %q", s.Name)
		}
	}
}

func TestDebate_FallsBackWhenMostSpecialistsFail(t *testing.T) {
	_, encoded := newCaseStateJSON(t)

	specialists := []Specialist{
		{Name: "Cardiology", Description: "Heart."},
		{Name: "Infectious Disease", Description: "Infections."},
		{Name: "Pulmonology", Description: "Lungs."},
		{Name: "Emergency Medicine", Description: "Acute."},
	}

	goodResp := `{"diagnosis":"CAP","confidence":0.85,"rationale":"r"}`
	client := &scriptedClient{
		scripts: []scriptedReply{
			{err: errors.New("network down")},
			{err: errors.New("network down")},
			{err: errors.New("network down")},
			{resp: &llm.Response{Content: goodResp}},
		},
	}
	base := &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}}
	d := &DebateDiagnostician{Base: base, Specialists: specialists}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" {
		t.Fatalf("fallback should still emit one proposal: %+v", out)
	}
	// Only the 4 specialist calls should have hit the LLM — no chair because
	// the quorum check tripped first.
	if got := len(client.Calls); got != len(specialists) {
		t.Fatalf("LLM calls=%d, want %d (specialists only, no chair)", got, len(specialists))
	}
	if !containsLog(env, "insufficient specialist quorum") {
		t.Fatalf("expected quorum fallback log, got %v", env.logs)
	}
}

func TestDebate_FallsBackOnChairFailure(t *testing.T) {
	_, encoded := newCaseStateJSON(t)

	specialists := []Specialist{
		{Name: "Cardiology", Description: "Heart."},
		{Name: "Pulmonology", Description: "Lungs."},
	}

	goodResp := `{"diagnosis":"CAP","confidence":0.85,"rationale":"r"}`
	client := &scriptedClient{
		scripts: []scriptedReply{
			{resp: &llm.Response{Content: goodResp}},
			{resp: &llm.Response{Content: goodResp}},
			// Chair fails.
			{err: errors.New("chair upstream timeout")},
		},
	}
	base := &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}}
	d := &DebateDiagnostician{Base: base, Specialists: specialists}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" {
		t.Fatalf("expected fallback emission on chair failure: %+v", out)
	}
	if got := len(client.Calls); got != len(specialists)+1 {
		t.Fatalf("LLM calls=%d, want %d (specialists + 1 chair attempt)", got, len(specialists)+1)
	}
	if !containsLog(env, "chair synthesis failed") {
		t.Fatalf("expected chair-failure log, got %v", env.logs)
	}
}

func TestDebate_DefaultSpecialistsCountIs4(t *testing.T) {
	got := DefaultSpecialists()
	if len(got) != 4 {
		t.Fatalf("DefaultSpecialists len=%d, want 4", len(got))
	}
	wantNames := map[string]bool{
		"Cardiology":          false,
		"Infectious Disease":  false,
		"Pulmonology":         false,
		"Emergency Medicine":  false,
	}
	for _, s := range got {
		if _, ok := wantNames[s.Name]; !ok {
			t.Errorf("unexpected specialist %q", s.Name)
			continue
		}
		wantNames[s.Name] = true
		if s.Description == "" {
			t.Errorf("specialist %q has empty Description", s.Name)
		}
	}
	for n, seen := range wantNames {
		if !seen {
			t.Errorf("missing default specialist %q", n)
		}
	}
}

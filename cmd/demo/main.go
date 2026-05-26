package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/PratikDhanave/bodh/agents"
	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/knowledge"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/orchestration"
	"github.com/PratikDhanave/bodh/pkg/registry"
)

// Demo: sequential diagnosis workflow inspired by Microsoft MAI-DxO and SD Bench.
//
// Flow (virtual physician panel):
//   user → intake → rpm_monitor → diagnostic_supervisor → questioner → ...
//
// Use -case=data/cases/demo_chf.json for RPM / CHF; default is demo_pneumonia.

func main() {
	casePath := flag.String("case", "", "path to case JSON (e.g. data/cases/demo_chf.json)")
	provider := flag.String("diagnostician", agents.ProviderRule,
		"diagnostician backend: rule | llm-anthropic | llm-openai | llm-ollama | llm-kronk")
	flag.Parse()

	ctx := context.Background()

	store := loadRAGStore(ctx, *provider)

	diag, err := agents.NewDiagnosticianWithKnowledge(*provider, store)
	if err != nil {
		fmt.Fprintf(os.Stderr, "diagnostician: %v\n", err)
		os.Exit(2)
	}

	logger := observability.NewStdLogger()
	env := &orchestration.SimpleEnvironment{
		Logger: logger,
		Clock:  observability.SystemClock{},
	}

	reg := registry.NewInMemory()
	bus := comm.NewInMemoryBus()
	policy := governance.NewComposite(
		governance.MaxContentLengthPolicy{Max: 65536},
		governance.RequireCaseIDPolicy{},
		governance.MaxDiagnosticCostPolicy{},
		governance.ResearchDisclaimerPolicy{},
	)

	agentList := []agent.Agent{
		&agents.IntakeAgent{},
		&agents.RPMMonitorAgent{},
		&agents.DiagnosticSupervisor{},
		&agents.QuestionerAgent{},
		&agents.TestPlannerAgent{},
		&agents.CostGuardianAgent{},
		diag,
		&agents.ReasoningVerifierAgent{},
	}
	for _, a := range agentList {
		_ = reg.Register(ctx, a)
	}

	orch := orchestration.NewOrchestrator(reg, bus, policy, env)
	orch.Start(ctx)

	presentation, meta := loadCase(*casePath)

	start := agent.NewMessage(
		"user",
		"intake",
		agent.RoleUser,
		"presentation",
		medical.EncodePresentation(presentation),
		meta,
	)
	bus.Publish(ctx, start)

	time.Sleep(3 * time.Second)
}

func loadCase(path string) (medical.PatientPresentation, map[string]any) {
	if path == "" {
		return pneumoniaDefault(), pneumoniaMeta()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	var fixture struct {
		CaseID          string                   `json:"case_id"`
		Condition       string                   `json:"condition"`
		ChiefComplaint  string                   `json:"chief_complaint"`
		HPI             string                   `json:"hpi"`
		Demographics    string                   `json:"demographics"`
		Vitals          string                   `json:"vitals"`
		GoldDiagnosis   string                   `json:"gold_diagnosis"`
		MaxCostUSD      float64                  `json:"max_cost_usd"`
		RPMObservations []medical.RPMObservation `json:"rpm_observations"`
	}
	if err := json.Unmarshal(b, &fixture); err != nil {
		panic(err)
	}
	pres := medical.PatientPresentation{
		ChiefComplaint:  fixture.ChiefComplaint,
		HPI:             fixture.HPI,
		Demographics:    fixture.Demographics,
		Vitals:          fixture.Vitals,
		Condition:       fixture.Condition,
		RPMObservations: fixture.RPMObservations,
	}
	meta := map[string]any{
		"case_id":        fixture.CaseID,
		"trace_id":       "trace-" + fixture.CaseID,
		"max_cost_usd":   fixture.MaxCostUSD,
		"gold_diagnosis": fixture.GoldDiagnosis,
		"message_steps":  12,
	}
	return pres, meta
}

func pneumoniaDefault() medical.PatientPresentation {
	return medical.PatientPresentation{
		ChiefComplaint: "Cough and fever",
		HPI:            "58-year-old presents with 5 days productive cough and fever to 38.9°C",
		Demographics:   "58M",
		Vitals:         "T 38.9°C, HR 98, RR 22, SpO2 94% on room air",
		Condition:      "cap",
	}
}

// loadRAGStore builds the knowledge.Store used by the diagnostician when
// BODH_KNOWLEDGE_CORPUS is set and an embedder credential is available for
// the supplied provider. Prints a one-line startup log either way.
func loadRAGStore(ctx context.Context, provider string) knowledge.Store {
	corpus := os.Getenv("BODH_KNOWLEDGE_CORPUS")
	if corpus == "" {
		fmt.Println("[demo] RAG disabled (BODH_KNOWLEDGE_CORPUS not set)")
		return nil
	}
	embedder, err := agents.DefaultEmbedderForProvider(provider)
	if err != nil {
		fmt.Printf("[demo] RAG disabled (embedder for %s: %v)\n", provider, err)
		return nil
	}
	if embedder == nil {
		fmt.Printf("[demo] RAG disabled (no embedder credential for %s)\n", provider)
		return nil
	}
	store, err := agents.LoadDefaultKnowledge(ctx, embedder)
	if err != nil {
		fmt.Printf("[demo] RAG disabled (corpus load failed: %v)\n", err)
		return nil
	}
	if store == nil {
		fmt.Println("[demo] RAG disabled (BODH_KNOWLEDGE_CORPUS not set)")
		return nil
	}
	fmt.Printf("[demo] RAG enabled with %d docs\n", store.Size())
	return store
}

func pneumoniaMeta() map[string]any {
	return map[string]any{
		"case_id":        "demo-case-001",
		"trace_id":       "trace-demo-001",
		"max_cost_usd":   500.0,
		"gold_diagnosis": "Community-acquired pneumonia",
		"message_steps":  12,
	}
}

package governance

import (
	"context"
	"time"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// RequireCaseIDPolicy denies clinical messages missing case_id in metadata.
// Aligns with MARA governance: fail closed before agents handle PHI-bearing payloads.
type RequireCaseIDPolicy struct{}

func (RequireCaseIDPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	clinicalTypes := map[string]bool{
		"presentation": true, "case_state": true, "ask_question": true,
		"order_test": true, "diagnosis": true, "verify_reasoning": true,
	}
	if !clinicalTypes[msg.Type] {
		return allow("non-clinical message"), nil
	}
	if msg.Metadata == nil {
		return deny("missing metadata with case_id"), nil
	}
	if _, ok := msg.Metadata["case_id"].(string); !ok {
		return deny("case_id required for clinical messages"), nil
	}
	return allow("case_id present"), nil
}

// MaxDiagnosticCostPolicy denies test orders that would exceed budget in metadata.
type MaxDiagnosticCostPolicy struct{}

func (MaxDiagnosticCostPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	if msg.Type != "order_test" {
		return allow("not a test order"), nil
	}
	if msg.Metadata == nil {
		return allow("no cost metadata"), nil
	}
	maxCost, ok := msg.Metadata["max_cost_usd"].(float64)
	if !ok {
		return allow("no max_cost_usd constraint"), nil
	}
	testCost, ok := msg.Metadata["test_cost_usd"].(float64)
	if !ok {
		return allow("no test_cost_usd in metadata"), nil
	}
	spent, _ := msg.Metadata["spent_usd"].(float64)
	if spent+testCost > maxCost {
		return deny("test would exceed diagnostic budget"), nil
	}
	return allow("within diagnostic budget"), nil
}

// ResearchDisclaimerPolicy tags system messages; allows all but logs intent.
type ResearchDisclaimerPolicy struct{}

func (ResearchDisclaimerPolicy) Evaluate(_ context.Context, _ protocol.Message) (PolicyResult, error) {
	return allow("research demo — not for clinical use"), nil
}

func allow(reason string) PolicyResult {
	return PolicyResult{
		Decision:  DecisionAllow,
		Reason:    reason,
		CheckedAt: time.Now().UTC(),
	}
}

func deny(reason string) PolicyResult {
	return PolicyResult{
		Decision:  DecisionDeny,
		Reason:    reason,
		CheckedAt: time.Now().UTC(),
	}
}

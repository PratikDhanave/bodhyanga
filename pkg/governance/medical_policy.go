package governance

import (
	"context"
	"strings"
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

// RequireTenantPolicy denies any message missing tenant_id in metadata.
// Operators enabling multi-tenancy compose this into their CompositePolicy so
// every hop carries a tenant correlator that downstream stores can enforce
// (e.g. PostgreSQL row-level security).
type RequireTenantPolicy struct{}

func (RequireTenantPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	if msg.Metadata == nil {
		return deny("tenant_id required: no metadata"), nil
	}
	if v, ok := msg.Metadata["tenant_id"].(string); !ok || v == "" {
		return deny("tenant_id required"), nil
	}
	return allow("tenant_id present"), nil
}

// RequireScopePolicy denies a message if the SMART-on-FHIR scope expected for
// its Type is missing from metadata.scopes (space-separated string, as in OAuth
// 2.0 token introspection). Required maps message Type -> required scope.
//
// Example:
//
//	RequireScopePolicy{Required: map[string]string{
//	    "cdm_request":      "user/CarePlan.write",
//	    "discharge_event":  "user/Encounter.read",
//	}}
type RequireScopePolicy struct {
	Required map[string]string
}

func (p RequireScopePolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	required, ok := p.Required[msg.Type]
	if !ok {
		return allow("no scope required for type"), nil
	}
	if msg.Metadata == nil {
		return deny("scopes missing for " + required), nil
	}
	scopes, _ := msg.Metadata["scopes"].(string)
	for _, s := range strings.Fields(scopes) {
		if s == required {
			return allow("scope " + required + " present"), nil
		}
	}
	return deny("required scope missing: " + required), nil
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

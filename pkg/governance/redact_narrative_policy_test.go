package governance

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/protocol"
)

// clinicalJSON builds a JSON string containing the supplied narrative fields.
func clinicalJSON(fields map[string]string) string {
	payload := make(map[string]any, len(fields))
	for k, v := range fields {
		payload[k] = v
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

// TestRedactNarrative_AuditDestination verifies that clinical narrative fields
// are redacted when the destination is "audit".
func TestRedactNarrative_AuditDestination(t *testing.T) {
	policy := RedactNarrativePolicy{}

	content := clinicalJSON(map[string]string{
		"chief_complaint": "Patient Jane Doe reports chest pain since 2026-01-15",
		"hpi":             "64-year-old female, MRN: MRN-98765, presents with...",
		"assessment":      "Acute coronary syndrome, phone 415-555-1234",
		"plan":            "Admit to CCU, order ECG",
	})

	msg := protocol.NewMessage("intake", "audit-service", protocol.RoleAgent, "presentation", content, map[string]any{
		"destination": "audit",
		"case_id":     "abc123",
	})

	result, err := policy.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s: %s", result.Decision, result.Reason)
	}

	// narrative_redacted flag must be set.
	if msg.Metadata["narrative_redacted"] != "true" {
		t.Errorf("expected narrative_redacted=true, got %v", msg.Metadata["narrative_redacted"])
	}

	// redacted_content must exist and contain sentinels, not originals.
	redactedStr, ok := msg.Metadata["redacted_content"].(string)
	if !ok || redactedStr == "" {
		t.Fatalf("expected redacted_content in metadata, got %v", msg.Metadata["redacted_content"])
	}

	var redacted map[string]any
	if err := json.Unmarshal([]byte(redactedStr), &redacted); err != nil {
		t.Fatalf("redacted_content is not valid JSON: %v", err)
	}

	for _, field := range []string{"chief_complaint", "hpi", "assessment", "plan"} {
		v, exists := redacted[field]
		if !exists {
			t.Errorf("field %q missing from redacted content", field)
			continue
		}
		if v != redactedSentinel {
			t.Errorf("field %q: expected %q, got %q", field, redactedSentinel, v)
		}
	}
}

// TestRedactNarrative_ObservabilityDestination verifies redaction for the
// "observability" destination.
func TestRedactNarrative_ObservabilityDestination(t *testing.T) {
	policy := RedactNarrativePolicy{}

	content := clinicalJSON(map[string]string{
		"notes":     "Free-text note with SSN 123-45-6789",
		"narrative": "Long-form narrative",
		"reason":    "Dyspnoea on exertion",
	})

	msg := protocol.NewMessage("diagnostician", "otel-collector", protocol.RoleAgent, "diagnosis", content, map[string]any{
		"destination": "observability",
	})

	result, err := policy.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", result.Decision)
	}

	if msg.Metadata["narrative_redacted"] != "true" {
		t.Errorf("expected narrative_redacted=true")
	}

	redactedStr, _ := msg.Metadata["redacted_content"].(string)
	var redacted map[string]any
	if err := json.Unmarshal([]byte(redactedStr), &redacted); err != nil {
		t.Fatalf("redacted_content invalid JSON: %v", err)
	}
	for _, field := range []string{"notes", "narrative", "reason"} {
		if redacted[field] != redactedSentinel {
			t.Errorf("field %q: want %q, got %v", field, redactedSentinel, redacted[field])
		}
	}
}

// TestRedactNarrative_AnalyticsDestination verifies redaction for the
// "analytics" destination.
func TestRedactNarrative_AnalyticsDestination(t *testing.T) {
	policy := RedactNarrativePolicy{}

	content := clinicalJSON(map[string]string{
		"plan": "Continue warfarin 5mg daily",
	})

	msg := protocol.NewMessage("care-planner", "analytics", protocol.RoleAgent, "care_plan", content, map[string]any{
		"destination": "analytics",
	})

	result, err := policy.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", result.Decision)
	}
	if msg.Metadata["narrative_redacted"] != "true" {
		t.Errorf("expected narrative_redacted=true")
	}
}

// TestRedactNarrative_ExportDestination verifies redaction for the
// "export" destination.
func TestRedactNarrative_ExportDestination(t *testing.T) {
	policy := RedactNarrativePolicy{}

	content := clinicalJSON(map[string]string{
		"hpi":    "Patient Bob Smith, DOB 1960-03-12",
		"reason": "Pre-op evaluation",
	})

	msg := protocol.NewMessage("export-agent", "downstream", protocol.RoleAgent, "export_event", content, map[string]any{
		"destination": "export",
	})

	result, err := policy.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", result.Decision)
	}
	if msg.Metadata["narrative_redacted"] != "true" {
		t.Errorf("expected narrative_redacted=true")
	}
}

// TestRedactNarrative_NonClinicalFieldsPassThrough verifies that a JSON
// message with no narrative fields is allowed without modification.
func TestRedactNarrative_NonClinicalFieldsPassThrough(t *testing.T) {
	policy := RedactNarrativePolicy{}

	content := `{"status":"ok","code":200,"message_id":"abcd-1234"}`

	msg := protocol.NewMessage("system", "audit-service", protocol.RoleSystem, "heartbeat", content, map[string]any{
		"destination": "audit",
	})

	result, err := policy.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", result.Decision)
	}

	// No narrative fields were present; narrative_redacted must NOT be set.
	if msg.Metadata["narrative_redacted"] == "true" {
		t.Errorf("narrative_redacted should not be set when no narrative fields present")
	}
	if _, exists := msg.Metadata["redacted_content"]; exists {
		t.Errorf("redacted_content should not be set when no narrative fields present")
	}
}

// TestRedactNarrative_NonJSONPassThrough verifies that non-JSON content is
// allowed unchanged (no redaction attempted).
func TestRedactNarrative_NonJSONPassThrough(t *testing.T) {
	policy := RedactNarrativePolicy{}

	msg := protocol.NewMessage("hl7-agent", "audit-service", protocol.RoleAgent, "hl7_adt", "MSH|^~\\&|EPIC|...", map[string]any{
		"destination": "audit",
	})

	result, err := policy.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", result.Decision)
	}

	if msg.Metadata["narrative_redacted"] == "true" {
		t.Errorf("narrative_redacted should not be set for non-JSON content")
	}
}

// TestRedactNarrative_NonLogBoundDestinationPassThrough verifies that messages
// destined for non-log-bound targets pass through with no modification.
func TestRedactNarrative_NonLogBoundDestinationPassThrough(t *testing.T) {
	policy := RedactNarrativePolicy{}

	content := clinicalJSON(map[string]string{
		"hpi":        "Detailed patient history with PHI",
		"assessment": "Working diagnosis",
	})

	for _, dest := range []string{"agent", "user", "intake", "diagnostician", ""} {
		t.Run("dest="+dest, func(t *testing.T) {
			meta := map[string]any{}
			if dest != "" {
				meta["destination"] = dest
			}
			msg := protocol.NewMessage("a", "b", protocol.RoleAgent, "presentation", content, meta)

			result, err := policy.Evaluate(context.Background(), msg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Decision != DecisionAllow {
				t.Errorf("expected allow for dest=%q, got %s", dest, result.Decision)
			}
			if msg.Metadata["narrative_redacted"] == "true" {
				t.Errorf("narrative_redacted must not be set for dest=%q", dest)
			}
			if _, exists := msg.Metadata["redacted_content"]; exists {
				t.Errorf("redacted_content must not be set for dest=%q", dest)
			}
		})
	}
}

// TestRedactNarrative_NilMetadataDestination verifies that a message with nil
// metadata (no destination) passes through without panic.
func TestRedactNarrative_NilMetadataDestination(t *testing.T) {
	policy := RedactNarrativePolicy{}

	content := clinicalJSON(map[string]string{"hpi": "Some narrative"})

	msg := protocol.NewMessage("a", "b", protocol.RoleAgent, "presentation", content, nil)

	result, err := policy.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != DecisionAllow {
		t.Errorf("expected allow with nil metadata, got %s", result.Decision)
	}
}

// TestRedactNarrative_AllNarrativeFields verifies every field in the
// narrativeFields list is redacted.
func TestRedactNarrative_AllNarrativeFields(t *testing.T) {
	policy := RedactNarrativePolicy{}

	fields := map[string]string{}
	for _, f := range narrativeFields {
		fields[f] = "sensitive free-text content for " + f
	}
	content := clinicalJSON(fields)

	msg := protocol.NewMessage("intake", "audit", protocol.RoleAgent, "presentation", content, map[string]any{
		"destination": "audit",
		"case_id":     "test-case",
	})

	result, err := policy.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", result.Decision)
	}

	redactedStr, ok := msg.Metadata["redacted_content"].(string)
	if !ok {
		t.Fatalf("redacted_content not set")
	}

	var redacted map[string]any
	if err := json.Unmarshal([]byte(redactedStr), &redacted); err != nil {
		t.Fatalf("invalid JSON in redacted_content: %v", err)
	}

	for _, f := range narrativeFields {
		v, exists := redacted[f]
		if !exists {
			t.Errorf("field %q missing from redacted output", f)
			continue
		}
		if v != redactedSentinel {
			t.Errorf("field %q: want %q, got %q", f, redactedSentinel, v)
		}
	}
}

// TestRedactNarrative_MixedFields verifies that non-narrative fields in the
// same JSON object are preserved intact while narrative fields are redacted.
func TestRedactNarrative_MixedFields(t *testing.T) {
	policy := RedactNarrativePolicy{}

	content := `{"case_id":"case-abc","severity":3,"chief_complaint":"Chest pain since yesterday","lab_result":"troponin 0.02"}`

	msg := protocol.NewMessage("intake", "audit", protocol.RoleAgent, "presentation", content, map[string]any{
		"destination": "audit",
	})

	result, err := policy.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", result.Decision)
	}

	redactedStr, _ := msg.Metadata["redacted_content"].(string)
	var redacted map[string]any
	if err := json.Unmarshal([]byte(redactedStr), &redacted); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Narrative field should be redacted.
	if redacted["chief_complaint"] != redactedSentinel {
		t.Errorf("chief_complaint: want %q, got %v", redactedSentinel, redacted["chief_complaint"])
	}

	// Non-narrative fields must be preserved.
	if redacted["case_id"] != "case-abc" {
		t.Errorf("case_id: want %q, got %v", "case-abc", redacted["case_id"])
	}
	if redacted["lab_result"] != "troponin 0.02" {
		t.Errorf("lab_result: want %q, got %v", "troponin 0.02", redacted["lab_result"])
	}
	// Numeric field preserved.
	if redacted["severity"] != float64(3) {
		t.Errorf("severity: want 3, got %v", redacted["severity"])
	}
}

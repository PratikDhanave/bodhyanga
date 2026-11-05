package governance

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/protocol"
)

// newPart2Msg is a test helper that builds a minimal protocol.Message with the
// given metadata, keeping Part 2 policy test cases focused on the fields under test.
func newPart2Msg(metadata map[string]any) protocol.Message {
	return protocol.NewMessage("test-sender", "test-recipient", protocol.RoleUser, "diagnosis", "test content", metadata)
}

// TestPart2_AllowsNonSUDMessages verifies that messages without any SUD
// condition markers are allowed without requiring consent. This covers the
// common case where a clinical message carries a non-SUD condition (e.g.,
// hypertension, diabetes) and must not be blocked by the Part 2 policy.
func TestPart2_AllowsNonSUDMessages(t *testing.T) {
	policy := Part2ConsentPolicy{}
	ctx := context.Background()

	cases := []struct {
		name     string
		metadata map[string]any
	}{
		{
			name:     "no metadata at all",
			metadata: nil,
		},
		{
			name:     "condition absent from metadata",
			metadata: map[string]any{"case_id": "abc123"},
		},
		{
			name:     "non-SUD condition: hypertension",
			metadata: map[string]any{"condition": "hypertension"},
		},
		{
			name:     "non-SUD condition: type2_diabetes",
			metadata: map[string]any{"condition": "type2_diabetes"},
		},
		{
			name:     "non-SUD condition: pneumonia",
			metadata: map[string]any{"condition": "pneumonia"},
		},
		{
			name:     "empty condition string",
			metadata: map[string]any{"condition": ""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := policy.Evaluate(ctx, newPart2Msg(tc.metadata))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Decision != DecisionAllow {
				t.Errorf("expected allow, got deny (reason: %s)", res.Reason)
			}
			if res.CheckedAt.IsZero() {
				t.Error("CheckedAt must be non-zero")
			}
		})
	}
}

// TestPart2_DeniesUnconsentedSUDAccess verifies that messages carrying SUD
// condition markers are denied when neither part2_consent nor break_glass is
// set. This is the core regulatory requirement of 42 CFR Part 2.
func TestPart2_DeniesUnconsentedSUDAccess(t *testing.T) {
	policy := Part2ConsentPolicy{}
	ctx := context.Background()

	wantReason := "42 CFR Part 2: SUD record access requires patient consent (part2_consent=true)"

	cases := []struct {
		name      string
		condition string
	}{
		{"opioid_use_disorder", "opioid_use_disorder"},
		{"alcohol_use_disorder", "alcohol_use_disorder"},
		{"substance_use", "substance_use"},
		{"sud", "sud"},
		{"opioid", "opioid"},
		{"methadone", "methadone"},
		{"buprenorphine", "buprenorphine"},
		{"naltrexone", "naltrexone"},
		{"addiction", "addiction"},
		// Compound / qualified values — substring matching must catch these.
		{"severe_opioid_use_disorder", "severe_opioid_use_disorder"},
		{"opioid_use_disorder_moderate", "opioid_use_disorder_moderate"},
		// Uppercase — policy must be case-insensitive.
		{"uppercase OUD", "OPIOID_USE_DISORDER"},
		{"mixed case SUD", "Substance_Use"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := map[string]any{"condition": tc.condition}
			res, err := policy.Evaluate(ctx, newPart2Msg(meta))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Decision != DecisionDeny {
				t.Errorf("expected deny for condition %q, got allow (reason: %s)", tc.condition, res.Reason)
			}
			if res.Reason != wantReason {
				t.Errorf("reason mismatch\n  got:  %q\n  want: %q", res.Reason, wantReason)
			}
			if res.CheckedAt.IsZero() {
				t.Error("CheckedAt must be non-zero")
			}
		})
	}
}

// TestPart2_AllowsWithConsent verifies that a SUD-bearing message is allowed
// when part2_consent="true" is present in metadata, covering the normal
// consent-on-file access path.
func TestPart2_AllowsWithConsent(t *testing.T) {
	policy := Part2ConsentPolicy{}
	ctx := context.Background()

	cases := []struct {
		name      string
		condition string
	}{
		{"opioid_use_disorder with consent", "opioid_use_disorder"},
		{"alcohol_use_disorder with consent", "alcohol_use_disorder"},
		{"methadone with consent", "methadone"},
		{"buprenorphine with consent", "buprenorphine"},
		{"naltrexone with consent", "naltrexone"},
		{"addiction with consent", "addiction"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := map[string]any{
				"condition":    tc.condition,
				"part2_consent": "true",
			}
			res, err := policy.Evaluate(ctx, newPart2Msg(meta))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Decision != DecisionAllow {
				t.Errorf("expected allow with consent, got deny (reason: %s)", res.Reason)
			}
			if res.CheckedAt.IsZero() {
				t.Error("CheckedAt must be non-zero")
			}
		})
	}

	// part2_consent values other than "true" must NOT grant access.
	t.Run("consent value false is not consent", func(t *testing.T) {
		meta := map[string]any{
			"condition":    "opioid_use_disorder",
			"part2_consent": "false",
		}
		res, err := policy.Evaluate(ctx, newPart2Msg(meta))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Decision != DecisionDeny {
			t.Error("part2_consent=false should not grant access")
		}
	})

	t.Run("consent value 1 is not accepted", func(t *testing.T) {
		meta := map[string]any{
			"condition":    "opioid_use_disorder",
			"part2_consent": "1",
		}
		res, err := policy.Evaluate(ctx, newPart2Msg(meta))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Decision != DecisionDeny {
			t.Error("part2_consent=1 should not grant access (must be literal string 'true')")
		}
	})
}

// TestPart2_AllowsBreakGlass verifies that a SUD-bearing message is allowed
// when break_glass="true" is present in metadata, covering the emergency
// override path. This is distinct from consent: the break-glass path should
// be separately audited by the break-glass workflow.
func TestPart2_AllowsBreakGlass(t *testing.T) {
	policy := Part2ConsentPolicy{}
	ctx := context.Background()

	cases := []struct {
		name      string
		condition string
	}{
		{"opioid_use_disorder break-glass", "opioid_use_disorder"},
		{"alcohol_use_disorder break-glass", "alcohol_use_disorder"},
		{"substance_use break-glass", "substance_use"},
		{"addiction break-glass", "addiction"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := map[string]any{
				"condition":   tc.condition,
				"break_glass": "true",
			}
			res, err := policy.Evaluate(ctx, newPart2Msg(meta))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Decision != DecisionAllow {
				t.Errorf("expected allow with break-glass, got deny (reason: %s)", res.Reason)
			}
			if res.CheckedAt.IsZero() {
				t.Error("CheckedAt must be non-zero")
			}
		})
	}

	// break_glass values other than "true" must NOT override the policy.
	t.Run("break_glass value false is not an override", func(t *testing.T) {
		meta := map[string]any{
			"condition":   "opioid_use_disorder",
			"break_glass": "false",
		}
		res, err := policy.Evaluate(ctx, newPart2Msg(meta))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Decision != DecisionDeny {
			t.Error("break_glass=false should not override the policy")
		}
	})

	// break_glass alone (no condition) should allow — but this is the
	// trivial path because a non-SUD message is already allowed before
	// break_glass is even checked.
	t.Run("break_glass on non-SUD message", func(t *testing.T) {
		meta := map[string]any{
			"condition":   "hypertension",
			"break_glass": "true",
		}
		res, err := policy.Evaluate(ctx, newPart2Msg(meta))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Decision != DecisionAllow {
			t.Errorf("non-SUD message should be allowed regardless of break_glass: %s", res.Reason)
		}
	})

	// Both consent and break_glass present: consent check fires first.
	t.Run("both consent and break_glass present", func(t *testing.T) {
		meta := map[string]any{
			"condition":    "opioid_use_disorder",
			"part2_consent": "true",
			"break_glass":  "true",
		}
		res, err := policy.Evaluate(ctx, newPart2Msg(meta))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Decision != DecisionAllow {
			t.Errorf("expected allow with both consent and break-glass: %s", res.Reason)
		}
	})
}

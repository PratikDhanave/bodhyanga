package guardrails

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// PIIGuardrail tests
// ---------------------------------------------------------------------------

func TestPIIGuardrail_DetectsSSN(t *testing.T) {
	g := &PIIGuardrail{}
	result := g.Check(context.Background(), "Patient SSN is 123-45-6789")
	if result.Safe {
		t.Fatal("expected PII guardrail to detect SSN")
	}
	if result.Rule != "pii_check" {
		t.Fatalf("expected rule pii_check, got %s", result.Rule)
	}
	if !strings.Contains(result.Reason, "ssn") {
		t.Fatalf("expected reason to mention ssn, got %s", result.Reason)
	}
}

func TestPIIGuardrail_DetectsEmail(t *testing.T) {
	g := &PIIGuardrail{}
	result := g.Check(context.Background(), "Contact patient at john.doe@hospital.org")
	if result.Safe {
		t.Fatal("expected PII guardrail to detect email")
	}
	if !strings.Contains(result.Reason, "email") {
		t.Fatalf("expected reason to mention email, got %s", result.Reason)
	}
}

func TestPIIGuardrail_DetectsPhone(t *testing.T) {
	g := &PIIGuardrail{}
	result := g.Check(context.Background(), "Call the patient at 555-123-4567")
	if result.Safe {
		t.Fatal("expected PII guardrail to detect phone")
	}
	if !strings.Contains(result.Reason, "phone") {
		t.Fatalf("expected reason to mention phone, got %s", result.Reason)
	}
}

func TestPIIGuardrail_DetectsMRN(t *testing.T) {
	g := &PIIGuardrail{}
	result := g.Check(context.Background(), "MRN: 12345678")
	if result.Safe {
		t.Fatal("expected PII guardrail to detect MRN")
	}
	if !strings.Contains(result.Reason, "mrn") {
		t.Fatalf("expected reason to mention mrn, got %s", result.Reason)
	}
}

func TestPIIGuardrail_SafeOutput(t *testing.T) {
	g := &PIIGuardrail{}
	result := g.Check(context.Background(), "The patient presents with chest pain and shortness of breath.")
	if !result.Safe {
		t.Fatalf("expected safe output, got reason: %s", result.Reason)
	}
}

// ---------------------------------------------------------------------------
// SQLInjectionGuardrail tests
// ---------------------------------------------------------------------------

func TestSQLInjection_DetectsDropTable(t *testing.T) {
	g := &SQLInjectionGuardrail{}
	result := g.Check(context.Background(), "SELECT * FROM users; DROP TABLE users;")
	if result.Safe {
		t.Fatal("expected SQL injection guardrail to detect DROP TABLE")
	}
}

func TestSQLInjection_DetectsUnionSelect(t *testing.T) {
	g := &SQLInjectionGuardrail{}
	result := g.Check(context.Background(), "SELECT name FROM patients UNION SELECT password FROM admin")
	if result.Safe {
		t.Fatal("expected SQL injection guardrail to detect UNION SELECT")
	}
}

func TestSQLInjection_DetectsDeleteFrom(t *testing.T) {
	g := &SQLInjectionGuardrail{}
	result := g.Check(context.Background(), "DELETE FROM patients WHERE id > 0")
	if result.Safe {
		t.Fatal("expected SQL injection guardrail to detect DELETE FROM")
	}
}

func TestSQLInjection_DetectsOR1Eq1(t *testing.T) {
	g := &SQLInjectionGuardrail{}
	result := g.Check(context.Background(), "SELECT * FROM users WHERE username='admin' OR 1=1")
	if result.Safe {
		t.Fatal("expected SQL injection guardrail to detect OR 1=1")
	}
}

func TestSQLInjection_SafeQuery(t *testing.T) {
	g := &SQLInjectionGuardrail{}
	result := g.Check(context.Background(), "SELECT patient_id, name FROM patients WHERE age > 65")
	if !result.Safe {
		t.Fatalf("expected safe query, got reason: %s", result.Reason)
	}
}

// ---------------------------------------------------------------------------
// SQLSyntaxGuardrail tests
// ---------------------------------------------------------------------------

func TestSQLSyntax_ValidSelect(t *testing.T) {
	g := &SQLSyntaxGuardrail{}
	result := g.Check(context.Background(), "SELECT id, name FROM patients WHERE age > 50")
	if !result.Safe {
		t.Fatalf("expected valid SQL, got reason: %s", result.Reason)
	}
}

func TestSQLSyntax_ValidInsert(t *testing.T) {
	g := &SQLSyntaxGuardrail{}
	result := g.Check(context.Background(), "INSERT INTO logs (message) VALUES ('test')")
	if !result.Safe {
		t.Fatalf("expected valid SQL, got reason: %s", result.Reason)
	}
}

func TestSQLSyntax_ValidWithCTE(t *testing.T) {
	g := &SQLSyntaxGuardrail{}
	result := g.Check(context.Background(), "WITH cte AS (SELECT 1) SELECT * FROM cte")
	if !result.Safe {
		t.Fatalf("expected valid SQL with CTE, got reason: %s", result.Reason)
	}
}

func TestSQLSyntax_EmptyOutput(t *testing.T) {
	g := &SQLSyntaxGuardrail{}
	result := g.Check(context.Background(), "")
	if result.Safe {
		t.Fatal("expected empty SQL to fail")
	}
}

func TestSQLSyntax_InvalidPrefix(t *testing.T) {
	g := &SQLSyntaxGuardrail{}
	result := g.Check(context.Background(), "HELLO WORLD")
	if result.Safe {
		t.Fatal("expected invalid SQL prefix to fail")
	}
}

func TestSQLSyntax_UnbalancedParens(t *testing.T) {
	g := &SQLSyntaxGuardrail{}
	result := g.Check(context.Background(), "SELECT * FROM (SELECT id FROM patients")
	if result.Safe {
		t.Fatal("expected unbalanced parentheses to fail")
	}
	if !strings.Contains(result.Reason, "unbalanced") {
		t.Fatalf("expected reason about parentheses, got %s", result.Reason)
	}
}

func TestSQLSyntax_ExtraCloseParen(t *testing.T) {
	g := &SQLSyntaxGuardrail{}
	result := g.Check(context.Background(), "SELECT * FROM patients)")
	if result.Safe {
		t.Fatal("expected extra close paren to fail")
	}
}

// ---------------------------------------------------------------------------
// ToxicityGuardrail tests
// ---------------------------------------------------------------------------

func TestToxicity_DetectsHarmfulAdvice(t *testing.T) {
	cases := []string{
		"You should stop taking your medication immediately",
		"Just ignore your doctor and try this instead",
		"This is a guaranteed cure for all diseases",
		"Big pharma doesn't want you to know about this",
		"Vaccines cause autism in children",
	}
	g := &ToxicityGuardrail{}
	for _, c := range cases {
		result := g.Check(context.Background(), c)
		if result.Safe {
			t.Fatalf("expected toxic content to be detected: %s", c)
		}
	}
}

func TestToxicity_SafeOutput(t *testing.T) {
	g := &ToxicityGuardrail{}
	result := g.Check(context.Background(), "Based on the lab results, the patient may benefit from a follow-up appointment.")
	if !result.Safe {
		t.Fatalf("expected safe output, got reason: %s", result.Reason)
	}
}

// ---------------------------------------------------------------------------
// LengthGuardrail tests
// ---------------------------------------------------------------------------

func TestLength_ExceedsMax(t *testing.T) {
	g := NewLengthGuardrail(10)
	result := g.Check(context.Background(), "This is definitely more than ten runes long")
	if result.Safe {
		t.Fatal("expected length check to fail")
	}
	if !strings.Contains(result.Reason, "exceeds maximum") {
		t.Fatalf("expected reason about exceeding max, got %s", result.Reason)
	}
}

func TestLength_WithinMax(t *testing.T) {
	g := NewLengthGuardrail(100)
	result := g.Check(context.Background(), "Short output")
	if !result.Safe {
		t.Fatalf("expected safe output, got reason: %s", result.Reason)
	}
}

func TestLength_DefaultMax(t *testing.T) {
	g := NewLengthGuardrail(0)
	if g.MaxRunes != 2000 {
		t.Fatalf("expected default max 2000, got %d", g.MaxRunes)
	}
}

func TestLength_ExactlyAtMax(t *testing.T) {
	g := NewLengthGuardrail(5)
	result := g.Check(context.Background(), "abcde")
	if !result.Safe {
		t.Fatal("expected output at exactly max to be safe")
	}
}

func TestLength_UnicodeRunes(t *testing.T) {
	g := NewLengthGuardrail(5)
	// 6 Unicode characters (each multi-byte), should exceed max of 5.
	result := g.Check(context.Background(), "éèêëïô")
	if result.Safe {
		t.Fatal("expected unicode string exceeding max runes to fail")
	}
}

// ---------------------------------------------------------------------------
// JSONValidityGuardrail tests
// ---------------------------------------------------------------------------

func TestJSON_ValidObject(t *testing.T) {
	g := &JSONValidityGuardrail{}
	result := g.Check(context.Background(), `{"diagnosis": "flu", "confidence": 0.85}`)
	if !result.Safe {
		t.Fatalf("expected valid JSON, got reason: %s", result.Reason)
	}
}

func TestJSON_ValidArray(t *testing.T) {
	g := &JSONValidityGuardrail{}
	result := g.Check(context.Background(), `["flu", "cold", "covid"]`)
	if !result.Safe {
		t.Fatalf("expected valid JSON array, got reason: %s", result.Reason)
	}
}

func TestJSON_InvalidJSON(t *testing.T) {
	g := &JSONValidityGuardrail{}
	result := g.Check(context.Background(), `{"diagnosis": "flu",}`)
	if result.Safe {
		t.Fatal("expected invalid JSON to fail")
	}
	if !strings.Contains(result.Reason, "invalid JSON") {
		t.Fatalf("expected reason about invalid JSON, got %s", result.Reason)
	}
}

func TestJSON_EmptyOutput(t *testing.T) {
	g := &JSONValidityGuardrail{}
	result := g.Check(context.Background(), "")
	if result.Safe {
		t.Fatal("expected empty output to fail JSON check")
	}
}

func TestJSON_PlainText(t *testing.T) {
	g := &JSONValidityGuardrail{}
	result := g.Check(context.Background(), "This is plain text, not JSON")
	if result.Safe {
		t.Fatal("expected plain text to fail JSON check")
	}
}

func TestJSON_WhitespaceAroundValid(t *testing.T) {
	g := &JSONValidityGuardrail{}
	result := g.Check(context.Background(), "  \n{\"ok\": true}\n  ")
	if !result.Safe {
		t.Fatalf("expected whitespace-padded JSON to pass, got reason: %s", result.Reason)
	}
}

// ---------------------------------------------------------------------------
// MedicalDisclaimerGuardrail tests
// ---------------------------------------------------------------------------

func TestMedicalDisclaimer_DiagnosisWithDisclaimer(t *testing.T) {
	g := &MedicalDisclaimerGuardrail{}
	output := "Differential diagnosis: influenza vs COVID-19. This is AI-generated and not for clinical use."
	result := g.Check(context.Background(), output)
	if !result.Safe {
		t.Fatalf("expected output with disclaimer to pass, got reason: %s", result.Reason)
	}
}

func TestMedicalDisclaimer_DiagnosisWithoutDisclaimer(t *testing.T) {
	g := &MedicalDisclaimerGuardrail{}
	output := "Differential diagnosis: influenza vs COVID-19. Recommend oseltamivir."
	result := g.Check(context.Background(), output)
	if result.Safe {
		t.Fatal("expected diagnosis without disclaimer to fail")
	}
	if !strings.Contains(result.Reason, "disclaimer") {
		t.Fatalf("expected reason about disclaimer, got %s", result.Reason)
	}
}

func TestMedicalDisclaimer_NoDiagnosticContent(t *testing.T) {
	g := &MedicalDisclaimerGuardrail{}
	output := "The weather today is sunny with a high of 72 degrees."
	result := g.Check(context.Background(), output)
	if !result.Safe {
		t.Fatal("expected non-diagnostic content to pass without disclaimer")
	}
}

func TestMedicalDisclaimer_TreatmentPlanWithDisclaimer(t *testing.T) {
	g := &MedicalDisclaimerGuardrail{}
	output := "Treatment plan: start amoxicillin 500mg TID. For informational purposes only."
	result := g.Check(context.Background(), output)
	if !result.Safe {
		t.Fatalf("expected treatment plan with disclaimer to pass, got reason: %s", result.Reason)
	}
}

func TestMedicalDisclaimer_PrescriptionWithoutDisclaimer(t *testing.T) {
	g := &MedicalDisclaimerGuardrail{}
	output := "Prescribe metformin 500mg twice daily for type 2 diabetes."
	result := g.Check(context.Background(), output)
	if result.Safe {
		t.Fatal("expected prescription without disclaimer to fail")
	}
}

// ---------------------------------------------------------------------------
// Pipeline tests
// ---------------------------------------------------------------------------

func TestPipeline_AllPass(t *testing.T) {
	p := NewPipeline(
		&ToxicityGuardrail{},
		NewLengthGuardrail(1000),
	)
	result, action := p.Run(context.Background(), "Normal clinical output.")
	if !result.Safe {
		t.Fatalf("expected all-pass, got reason: %s", result.Reason)
	}
	if action != "" {
		t.Fatalf("expected empty action on pass, got %s", action)
	}
}

func TestPipeline_FirstFailureShortCircuits(t *testing.T) {
	p := NewPipeline(
		&PIIGuardrail{},
		&ToxicityGuardrail{},
	)
	// Contains SSN (PII) — should fail at first guardrail.
	result, action := p.Run(context.Background(), "Patient SSN: 123-45-6789")
	if result.Safe {
		t.Fatal("expected PII failure")
	}
	if result.Rule != "pii_check" {
		t.Fatalf("expected pii_check rule, got %s", result.Rule)
	}
	if action != ActionReject {
		t.Fatalf("expected reject action, got %s", action)
	}
}

func TestPipeline_StatsTracking(t *testing.T) {
	p := NewPipeline(
		&PIIGuardrail{},
		NewLengthGuardrail(1000),
	)

	// Run a safe check.
	p.Run(context.Background(), "Safe output")
	// Run a failing check.
	p.Run(context.Background(), "SSN: 123-45-6789")

	stats := p.Stats()
	if stats.TotalChecks != 2 {
		t.Fatalf("expected 2 total checks, got %d", stats.TotalChecks)
	}
	if stats.TotalBlocked != 1 {
		t.Fatalf("expected 1 blocked, got %d", stats.TotalBlocked)
	}
	if stats.ByRule["pii_check"] != 1 {
		t.Fatalf("expected 1 pii_check block, got %d", stats.ByRule["pii_check"])
	}
}

func TestPipeline_ActionMapping(t *testing.T) {
	cases := []struct {
		guard    Guardrail
		input    string
		expected Action
	}{
		{&PIIGuardrail{}, "SSN: 123-45-6789", ActionReject},
		{&SQLInjectionGuardrail{}, "DROP TABLE users", ActionReject},
		{&SQLSyntaxGuardrail{}, "INVALID SQL", ActionRetry},
		{&ToxicityGuardrail{}, "guaranteed cure for cancer", ActionReject},
		{NewLengthGuardrail(5), "this is way too long", ActionLog},
		{&JSONValidityGuardrail{}, "not json", ActionRetry},
		{&MedicalDisclaimerGuardrail{}, "Diagnosis: flu", ActionLog},
	}
	for _, tc := range cases {
		p := NewPipeline(tc.guard)
		_, action := p.Run(context.Background(), tc.input)
		if action != tc.expected {
			t.Fatalf("guardrail %s: expected action %s, got %s",
				tc.guard.Name(), tc.expected, action)
		}
	}
}

func TestPipeline_SetAction(t *testing.T) {
	p := NewPipeline(&PIIGuardrail{})
	p.SetAction("pii_check", ActionLog)
	_, action := p.Run(context.Background(), "SSN: 123-45-6789")
	if action != ActionLog {
		t.Fatalf("expected log action after override, got %s", action)
	}
}

func TestPipeline_DefaultPipeline(t *testing.T) {
	p := DefaultPipeline()
	if len(p.guards) != 7 {
		t.Fatalf("expected 7 guards in default pipeline, got %d", len(p.guards))
	}
}

// ---------------------------------------------------------------------------
// CheckResult field tests
// ---------------------------------------------------------------------------

func TestCheckResult_LatencyPositive(t *testing.T) {
	g := &PIIGuardrail{}
	result := g.Check(context.Background(), "Normal output without PII")
	if result.Latency < 0 {
		t.Fatal("expected non-negative latency")
	}
}

func TestCheckResult_RuleSet(t *testing.T) {
	guards := []Guardrail{
		&PIIGuardrail{},
		&SQLInjectionGuardrail{},
		&SQLSyntaxGuardrail{},
		&ToxicityGuardrail{},
		NewLengthGuardrail(2000),
		&JSONValidityGuardrail{},
		&MedicalDisclaimerGuardrail{},
	}
	expected := []string{
		"pii_check",
		"sql_injection_check",
		"sql_syntax_check",
		"toxicity_check",
		"length_check",
		"json_validity_check",
		"medical_disclaimer_check",
	}
	for i, g := range guards {
		if g.Name() != expected[i] {
			t.Fatalf("expected guard name %s, got %s", expected[i], g.Name())
		}
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestPIIGuardrail_CreditCardPattern(t *testing.T) {
	g := &PIIGuardrail{}
	result := g.Check(context.Background(), "Card number: 4111 1111 1111 1111")
	if result.Safe {
		t.Fatal("expected credit card detection")
	}
}

func TestSQLInjection_CommentDash(t *testing.T) {
	g := &SQLInjectionGuardrail{}
	result := g.Check(context.Background(), "SELECT * FROM users WHERE id = 1 --")
	if result.Safe {
		t.Fatal("expected SQL comment injection detection")
	}
}

func TestSQLInjection_ExecCommand(t *testing.T) {
	g := &SQLInjectionGuardrail{}
	result := g.Check(context.Background(), "EXECUTE sp_executesql N'SELECT 1'")
	if result.Safe {
		t.Fatal("expected EXECUTE detection")
	}
}

func TestToxicity_CaseInsensitive(t *testing.T) {
	g := &ToxicityGuardrail{}
	result := g.Check(context.Background(), "STOP TAKING YOUR MEDICATION right now")
	if result.Safe {
		t.Fatal("expected case-insensitive toxicity detection")
	}
}

func TestItoa(t *testing.T) {
	cases := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{-7, "-7"},
		{1000, "1000"},
	}
	for _, c := range cases {
		got := itoa(c.input)
		if got != c.expected {
			t.Fatalf("itoa(%d) = %s, expected %s", c.input, got, c.expected)
		}
	}
}

func TestPipeline_ConcurrentAccess(t *testing.T) {
	p := NewPipeline(
		&PIIGuardrail{},
		&ToxicityGuardrail{},
		NewLengthGuardrail(1000),
	)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				p.Run(context.Background(), "Safe clinical output")
				p.Stats()
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	stats := p.Stats()
	if stats.TotalChecks != 1000 {
		t.Fatalf("expected 1000 total checks from concurrent access, got %d", stats.TotalChecks)
	}
}

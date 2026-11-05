package medical

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

// ---- Depression care plan (0%) ----

func TestBuildDepressionCarePlan_Minimal(t *testing.T) {
	now := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	p := BuildDepressionCarePlan("case1", "pt1", 3, now)
	if p.Condition != "depression" {
		t.Errorf("condition = %q, want depression", p.Condition)
	}
	if len(p.Education) == 0 {
		t.Error("expected education entries for minimal score")
	}
	wantReview := now.AddDate(0, 3, 0)
	if !p.NextReview.Equal(wantReview) {
		t.Errorf("NextReview = %v, want %v", p.NextReview, wantReview)
	}
}

func TestBuildDepressionCarePlan_Mild(t *testing.T) {
	now := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	p := BuildDepressionCarePlan("case1", "pt1", 7, now)
	wantReview := now.AddDate(0, 0, 28)
	if !p.NextReview.Equal(wantReview) {
		t.Errorf("NextReview = %v, want %v", p.NextReview, wantReview)
	}
}

func TestBuildDepressionCarePlan_Moderate(t *testing.T) {
	now := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	p := BuildDepressionCarePlan("case1", "pt1", 12, now)
	if len(p.Medications) == 0 {
		t.Error("expected medications for moderate score")
	}
	if p.Medications[0].Name != "Sertraline" {
		t.Errorf("med name = %q, want Sertraline", p.Medications[0].Name)
	}
}

func TestBuildDepressionCarePlan_ModeratelySevere(t *testing.T) {
	now := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	p := BuildDepressionCarePlan("case1", "pt1", 17, now)
	wantReview := now.AddDate(0, 0, 14)
	if !p.NextReview.Equal(wantReview) {
		t.Errorf("NextReview = %v, want %v", p.NextReview, wantReview)
	}
}

func TestBuildDepressionCarePlan_Severe(t *testing.T) {
	now := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	p := BuildDepressionCarePlan("case1", "pt1", 22, now)
	wantReview := now.AddDate(0, 0, 7)
	if !p.NextReview.Equal(wantReview) {
		t.Errorf("NextReview = %v, want %v", p.NextReview, wantReview)
	}
	// Check for crisis line in education.
	found := false
	for _, e := range p.Education {
		if e == "Crisis line: 988 Suicide & Crisis Lifeline" {
			found = true
		}
	}
	if !found {
		t.Error("expected crisis line in education for severe score")
	}
}

// ---- Registry (0%) ----

func TestPatientRegistry(t *testing.T) {
	reg := NewPatientRegistry()

	// Empty registry.
	if list := reg.List(); len(list) != 0 {
		t.Errorf("empty registry should return 0 patients, got %d", len(list))
	}

	// Get from empty.
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("expected false for nonexistent patient")
	}

	// Upsert.
	p1 := PatientRecord{PatientID: "P001", Age: 65, Conditions: []string{"dm", "htn"}}
	reg.Upsert(p1)

	got, ok := reg.Get("P001")
	if !ok {
		t.Fatal("expected to find P001")
	}
	if got.Age != 65 {
		t.Errorf("age = %d, want 65", got.Age)
	}

	// Update existing.
	p1.Age = 66
	reg.Upsert(p1)
	got, _ = reg.Get("P001")
	if got.Age != 66 {
		t.Errorf("updated age = %d, want 66", got.Age)
	}

	// Add another.
	p2 := PatientRecord{PatientID: "P002", Age: 45, Conditions: []string{"dm"}}
	reg.Upsert(p2)

	if list := reg.List(); len(list) != 2 {
		t.Errorf("expected 2 patients, got %d", len(list))
	}

	// FilterByCondition.
	dmPatients := reg.FilterByCondition("dm")
	if len(dmPatients) != 2 {
		t.Errorf("expected 2 dm patients, got %d", len(dmPatients))
	}

	htnPatients := reg.FilterByCondition("htn")
	if len(htnPatients) != 1 {
		t.Errorf("expected 1 htn patient, got %d", len(htnPatients))
	}

	noPatients := reg.FilterByCondition("copd")
	if len(noPatients) != 0 {
		t.Errorf("expected 0 copd patients, got %d", len(noPatients))
	}
}

// ---- Review encode/decode (0%) ----

func TestEncodeDecodeReview(t *testing.T) {
	r := ReviewRequest{
		ID:       "rev-1",
		Kind:     "diagnosis_review",
		Status:   ReviewPending,
		Priority: "high",
		Subject:  "Test review",
	}

	encoded := EncodeReview(r)
	decoded, err := DecodeReview(encoded)
	if err != nil {
		t.Fatalf("DecodeReview error: %v", err)
	}
	if decoded.ID != "rev-1" {
		t.Errorf("ID = %q, want rev-1", decoded.ID)
	}
	if decoded.Status != ReviewPending {
		t.Errorf("Status = %q, want pending", decoded.Status)
	}
}

func TestDecodeReview_Invalid(t *testing.T) {
	_, err := DecodeReview("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---- Discharge encode/decode (0%) ----

func TestEncodeDecodeDischarge(t *testing.T) {
	d := DischargeSummary{
		CaseID:    "case-1",
		PatientID: "pt-1",
		PrimaryDx: "CHF",
	}

	encoded := EncodeDischarge(d)
	decoded, err := DecodeDischarge(encoded)
	if err != nil {
		t.Fatalf("DecodeDischarge error: %v", err)
	}
	if decoded.PrimaryDx != "CHF" {
		t.Errorf("PrimaryDx = %q, want CHF", decoded.PrimaryDx)
	}
}

func TestDecodeDischarge_Invalid(t *testing.T) {
	_, err := DecodeDischarge("{invalid")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---- CarePlan Summary (0%) ----

func TestCarePlanSummary(t *testing.T) {
	now := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	p := BuildCarePlan("case1", "pt1", "htn", now)
	s := p.Summary()
	if s == "" {
		t.Error("Summary should not be empty")
	}
	// Should contain condition name.
	if got := p.Summary(); got == "" || len(got) < 10 {
		t.Errorf("Summary too short: %q", got)
	}
}

// ---- DecodeCarePlan error (line 132) ----

func TestDecodeCarePlan_Invalid(t *testing.T) {
	_, err := DecodeCarePlan("{bad json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---- EncodePresentation / DecodePresentation (0%) ----

func TestEncodeDecodePresentation(t *testing.T) {
	p := PatientPresentation{
		ChiefComplaint: "chest pain",
		HPI:            "onset 2 hours ago",
		Condition:      "acs",
	}

	encoded := EncodePresentation(p)
	decoded, err := DecodePresentation(encoded)
	if err != nil {
		t.Fatalf("DecodePresentation error: %v", err)
	}
	if decoded.ChiefComplaint != "chest pain" {
		t.Errorf("ChiefComplaint = %q, want 'chest pain'", decoded.ChiefComplaint)
	}
}

func TestDecodePresentation_Invalid(t *testing.T) {
	_, err := DecodePresentation("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---- DecodeState error (line 90) ----

func TestDecodeState_Invalid(t *testing.T) {
	_, err := DecodeState("bad json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---- DecodeADT error (line 35) ----

func TestDecodeADT_Invalid(t *testing.T) {
	_, err := DecodeADT("{invalid")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---- ScoreReadmissionRisk uncovered branches ----

func TestScoreReadmissionRisk_HighAge(t *testing.T) {
	now := time.Now()
	p := PatientRecord{
		Age:            78,
		Conditions:     []string{"dm", "htn", "chf", "ckd", "copd"},
		DischargedAt:   now.Add(-5 * 24 * time.Hour),
		LastSystolicBP: 170,
		LastA1c:        10.0,
	}
	result := ScoreReadmissionRisk(p, now)
	if result.Band != "high" {
		t.Errorf("expected high band, got %q", result.Band)
	}
	if result.Score > 1 {
		t.Errorf("score should be capped at 1, got %f", result.Score)
	}
}

func TestScoreReadmissionRisk_LowRisk(t *testing.T) {
	now := time.Now()
	p := PatientRecord{
		Age:            40,
		Conditions:     []string{},
		LastSystolicBP: 120,
		LastA1c:        5.5,
	}
	result := ScoreReadmissionRisk(p, now)
	if result.Band != "low" {
		t.Errorf("expected low band, got %q", result.Band)
	}
}

func TestScoreReadmissionRisk_Medium(t *testing.T) {
	now := time.Now()
	p := PatientRecord{
		Age:            67,
		Conditions:     []string{"dm", "htn"},
		DischargedAt:   now.Add(-10 * 24 * time.Hour),
		LastSystolicBP: 130,
		LastA1c:        7.0,
	}
	result := ScoreReadmissionRisk(p, now)
	if result.Band != "medium" {
		t.Errorf("expected medium band, got %q (score=%.2f)", result.Band, result.Score)
	}
}

// ---- NEWS2 SpO2 Scale 2 uncovered branches ----

func TestSpo2Scale2_AllBranches(t *testing.T) {
	cases := []struct {
		spo2 float64
		want int
	}{
		{80, 3},   // <= 83
		{84, 2},   // 84-85
		{86, 1},   // 86-87
		{90, 0},   // 88-92
		{93, 1},   // 93-94
		{95, 2},   // 95-96
		{98, 3},   // >= 97
	}
	for _, tc := range cases {
		got := spo2Scale2Score(tc.spo2)
		if got != tc.want {
			t.Errorf("spo2Scale2Score(%.0f) = %d, want %d", tc.spo2, got, tc.want)
		}
	}
}

// ---- HeartRate uncovered branches ----

func TestHeartRateScore_AllBranches(t *testing.T) {
	cases := []struct {
		hr   int
		want int
	}{
		{35, 3},   // <= 40
		{45, 1},   // 41-50
		{70, 0},   // 51-90
		{100, 1},  // 91-110
		{120, 2},  // 111-130
		{135, 3},  // >= 131
	}
	for _, tc := range cases {
		got := heartRateScore(tc.hr)
		if got != tc.want {
			t.Errorf("heartRateScore(%d) = %d, want %d", tc.hr, got, tc.want)
		}
	}
}

// ---- Panel rules uncovered branches ----

func TestA1cOverdueRule_UncontrolledA1c(t *testing.T) {
	now := time.Now()
	p := PatientRecord{
		PatientID:   "P001",
		Conditions:  []string{"dm"},
		LastA1cDate: now.Add(-10 * 24 * time.Hour), // recent
		LastA1c:     9.5,
	}
	gaps := a1cOverdueRule(p, now)
	if len(gaps) == 0 {
		t.Fatal("expected a1c_uncontrolled gap")
	}
	if gaps[0].Rule != "a1c_uncontrolled" {
		t.Errorf("rule = %q, want a1c_uncontrolled", gaps[0].Rule)
	}
}

func TestA1cOverdueRule_ControlledRecent(t *testing.T) {
	now := time.Now()
	p := PatientRecord{
		PatientID:   "P001",
		Conditions:  []string{"dm"},
		LastA1cDate: now.Add(-10 * 24 * time.Hour),
		LastA1c:     6.5,
	}
	gaps := a1cOverdueRule(p, now)
	if len(gaps) != 0 {
		t.Errorf("expected no gap for controlled recent A1c, got %d", len(gaps))
	}
}

func TestA1cOverdueRule_OverdueDate(t *testing.T) {
	now := time.Now()
	p := PatientRecord{
		PatientID:   "P001",
		Conditions:  []string{"dm"},
		LastA1cDate: now.Add(-200 * 24 * time.Hour),
		LastA1c:     7.0,
	}
	gaps := a1cOverdueRule(p, now)
	if len(gaps) == 0 || gaps[0].Rule != "a1c_overdue" {
		t.Error("expected a1c_overdue gap for old date")
	}
}

func TestA1cOverdueRule_ZeroDate(t *testing.T) {
	now := time.Now()
	p := PatientRecord{
		PatientID:  "P001",
		Conditions: []string{"dm"},
	}
	gaps := a1cOverdueRule(p, now)
	if len(gaps) == 0 || gaps[0].Rule != "a1c_overdue" {
		t.Error("expected a1c_overdue gap for zero date")
	}
}

func TestBpUncontrolledRule_AllBranches(t *testing.T) {
	// Not htn
	gaps := bpUncontrolledRule(PatientRecord{}, time.Now())
	if len(gaps) != 0 {
		t.Error("expected no gap for non-htn patient")
	}

	// htn + SBP >= 160 (urgent)
	gaps = bpUncontrolledRule(PatientRecord{
		Conditions:     []string{"htn"},
		LastSystolicBP: 165,
	}, time.Now())
	if len(gaps) == 0 || gaps[0].Rule != "bp_uncontrolled" {
		t.Error("expected bp_uncontrolled gap")
	}

	// htn + SBP 140-159 (warn)
	gaps = bpUncontrolledRule(PatientRecord{
		Conditions:     []string{"htn"},
		LastSystolicBP: 145,
	}, time.Now())
	if len(gaps) == 0 || gaps[0].Rule != "bp_elevated" {
		t.Error("expected bp_elevated gap")
	}

	// htn + controlled
	gaps = bpUncontrolledRule(PatientRecord{
		Conditions:     []string{"htn"},
		LastSystolicBP: 120,
	}, time.Now())
	if len(gaps) != 0 {
		t.Error("expected no gap for controlled BP")
	}
}

func TestChfWeightRule_NoGap(t *testing.T) {
	gaps := chfWeightRule(PatientRecord{
		Conditions:  []string{"chf"},
		LastWeightLb: 180,
	}, time.Now())
	if len(gaps) != 0 {
		t.Error("expected no gap for weight < 190")
	}
}

func TestPostDischargeFollowUpRule_AllBranches(t *testing.T) {
	now := time.Now()

	// No discharge
	gaps := postDischargeFollowUpRule(PatientRecord{}, now)
	if len(gaps) != 0 {
		t.Error("expected no gap for no discharge")
	}

	// Discharged < 7 days ago
	gaps = postDischargeFollowUpRule(PatientRecord{
		DischargedAt: now.Add(-3 * 24 * time.Hour),
	}, now)
	if len(gaps) != 0 {
		t.Error("expected no gap for <7 days")
	}

	// Discharged > 30 days ago
	gaps = postDischargeFollowUpRule(PatientRecord{
		DischargedAt: now.Add(-35 * 24 * time.Hour),
	}, now)
	if len(gaps) != 0 {
		t.Error("expected no gap for >30 days")
	}

	// Discharged 10 days ago, no visit after discharge (urgent)
	gaps = postDischargeFollowUpRule(PatientRecord{
		DischargedAt: now.Add(-10 * 24 * time.Hour),
	}, now)
	if len(gaps) == 0 {
		t.Error("expected tcm_followup_open gap")
	}

	// Discharged 10 days ago, visit after discharge (no gap)
	gaps = postDischargeFollowUpRule(PatientRecord{
		DischargedAt: now.Add(-10 * 24 * time.Hour),
		LastVisit:    now.Add(-5 * 24 * time.Hour),
	}, now)
	if len(gaps) != 0 {
		t.Error("expected no gap when visit after discharge")
	}
}

func TestPhq9OverdueRule_AllBranches(t *testing.T) {
	now := time.Now()

	// No depression
	gaps := phq9OverdueRule(PatientRecord{}, now)
	if len(gaps) != 0 {
		t.Error("expected no gap for non-depression patient")
	}

	// Depression, no PHQ9 date
	gaps = phq9OverdueRule(PatientRecord{
		PatientID:  "P001",
		Conditions: []string{"depression"},
	}, now)
	if len(gaps) == 0 || gaps[0].Rule != "phq9_overdue" {
		t.Error("expected phq9_overdue gap")
	}

	// Depression, PHQ9 recent
	gaps = phq9OverdueRule(PatientRecord{
		PatientID:    "P001",
		Conditions:   []string{"depression"},
		LastPHQ9Date: now.Add(-30 * 24 * time.Hour),
	}, now)
	if len(gaps) != 0 {
		t.Error("expected no gap for recent PHQ9")
	}

	// major_depressive_disorder, PHQ9 overdue
	gaps = phq9OverdueRule(PatientRecord{
		PatientID:    "P001",
		Conditions:   []string{"major_depressive_disorder"},
		LastPHQ9Date: now.Add(-100 * 24 * time.Hour),
	}, now)
	if len(gaps) == 0 {
		t.Error("expected phq9_overdue gap for MDD with old PHQ9")
	}
}

// ---- nextWellChildVisit uncovered branches ----

func TestNextWellChildVisit_AllBranches(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		age  int
		want time.Time
	}{
		{3, now.AddDate(0, 2, 0)},   // < 6
		{9, now.AddDate(0, 3, 0)},   // 6-17
		{30, now.AddDate(0, 6, 0)},  // 18-35
		{48, now.AddDate(1, 0, 0)},  // >= 36
	}
	for _, tc := range cases {
		got := nextWellChildVisit(tc.age, now)
		if !got.Equal(tc.want) {
			t.Errorf("nextWellChildVisit(%d) = %v, want %v", tc.age, got, tc.want)
		}
	}
}

// ---- BuildCarePlan default branch (line 118) ----

func TestBuildCarePlan_DefaultCondition(t *testing.T) {
	now := time.Now()
	p := BuildCarePlan("case1", "pt1", "unknown_condition", now)
	if len(p.Goals) == 0 {
		t.Error("expected default goal for unknown condition")
	}
	if p.Goals[0].Metric != "encounter_followup" {
		t.Errorf("default goal metric = %q, want encounter_followup", p.Goals[0].Metric)
	}
}

// ---- RPM alerts uncovered branches ----

func TestEvaluateRPMAlerts_HeartRate(t *testing.T) {
	obs := []RPMObservation{
		{Device: "watch", Metric: "heart_rate", Value: 115, Unit: "bpm"},
	}
	alerts := EvaluateRPMAlerts(obs)
	if len(alerts) == 0 {
		t.Error("expected heart rate alert")
	}
}

func TestEvaluateRPMAlerts_NoAlertThreshold(t *testing.T) {
	obs := []RPMObservation{
		{Device: "cuff", Metric: "systolic_bp", Value: 130, Unit: "mmHg"},
		{Device: "scale", Metric: "weight_lb", Value: 170, Unit: "lb"},
		{Device: "oximeter", Metric: "spo2", Value: 96, Unit: "%"},
		{Device: "watch", Metric: "heart_rate", Value: 80, Unit: "bpm"},
	}
	alerts := EvaluateRPMAlerts(obs)
	if len(alerts) != 0 {
		t.Errorf("expected no alerts, got %d", len(alerts))
	}
}

func TestEvaluateRPMAlerts_PresetAlert(t *testing.T) {
	obs := []RPMObservation{
		{Device: "custom", Metric: "unknown", Value: 0, Unit: "", Alert: true},
	}
	alerts := EvaluateRPMAlerts(obs)
	if len(alerts) == 0 {
		t.Error("expected alert for pre-set Alert flag")
	}
}

// ---- RuleBasedScribe empty transcript (line 237) ----

func TestRuleBasedScribe_Empty(t *testing.T) {
	out := RuleBasedScribe(ScribeInput{Transcript: ""})
	if out.Method != "rule" {
		t.Errorf("Method = %q, want rule", out.Method)
	}
	if out.Confidence != 0 {
		t.Errorf("Confidence = %f, want 0", out.Confidence)
	}
}

func TestRuleBasedScribe_NoSentenceBreak(t *testing.T) {
	// Single sentence with no period-space: entire text is chief complaint.
	input := ScribeInput{Transcript: "Patient has cough"}
	out := RuleBasedScribe(input)
	if out.ChiefComplaint != "Patient has cough" {
		t.Errorf("ChiefComplaint = %q, want whole text", out.ChiefComplaint)
	}
	if out.HPI != "" {
		t.Errorf("HPI should be empty, got %q", out.HPI)
	}
}

func TestRuleBasedScribe_MaxConfidence(t *testing.T) {
	// Transcript with everything: vitals, meds, allergies, chief complaint.
	input := ScribeInput{
		Transcript: "Patient presents with chest pain. Heart rate 88, blood pressure 120 over 80, temperature 98.6, respiratory rate 18, oxygen sat 95%. Taking metformin and lisinopril. Allergic to penicillin. No other issues.",
	}
	out := RuleBasedScribe(input)
	if out.Confidence > 1.0 {
		t.Errorf("Confidence %f should not exceed 1.0", out.Confidence)
	}
}

// ---- LLMScribe error paths ----

type errClient struct{}

func (e *errClient) Provider() string { return "mock" }
func (e *errClient) Model() string    { return "mock" }
func (e *errClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return nil, context.Canceled
}

func TestLLMScribe_NilClient(t *testing.T) {
	out, err := LLMScribe(context.Background(), nil, ScribeInput{Transcript: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Method != "rule" {
		t.Errorf("Method = %q, want rule for nil client", out.Method)
	}
}

func TestLLMScribe_ClientError(t *testing.T) {
	out, err := LLMScribe(context.Background(), &errClient{}, ScribeInput{Transcript: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Method != "rule" {
		t.Errorf("Method = %q, want rule (fallback)", out.Method)
	}
}

type badJSONClient struct{}

func (b *badJSONClient) Provider() string { return "mock" }
func (b *badJSONClient) Model() string    { return "mock" }
func (b *badJSONClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return &llm.Response{Content: "not json at all"}, nil
}

func TestLLMScribe_BadJSON(t *testing.T) {
	out, err := LLMScribe(context.Background(), &badJSONClient{}, ScribeInput{Transcript: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Method != "rule" {
		t.Errorf("Method = %q, want rule (JSON parse fallback)", out.Method)
	}
}

type goodJSONClient struct{}

func (g *goodJSONClient) Provider() string { return "mock" }
func (g *goodJSONClient) Model() string    { return "mock" }
func (g *goodJSONClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	out := ScribeOutput{
		ChiefComplaint: "headache",
		Confidence:     0.9,
		Method:         "llm",
	}
	b, _ := json.Marshal(out)
	// Wrap in code fences to test stripCodeFences.
	return &llm.Response{Content: "```json\n" + string(b) + "\n```"}, nil
}

func TestLLMScribe_Success(t *testing.T) {
	out, err := LLMScribe(context.Background(), &goodJSONClient{}, ScribeInput{Transcript: "patient has headache"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Method != "llm" {
		t.Errorf("Method = %q, want llm", out.Method)
	}
	if out.ChiefComplaint != "headache" {
		t.Errorf("ChiefComplaint = %q, want headache", out.ChiefComplaint)
	}
}

// ---- Pediatric well-child default age range ----

func TestBuildPediatricWellChildCarePlan_DefaultAge(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	// Age 36 months: not in any explicit case, should hit default.
	p := BuildPediatricWellChildCarePlan("c1", "p1", 36, 14.0, now)
	if p.Condition != "well_child" {
		t.Errorf("condition = %q, want well_child", p.Condition)
	}
}

// ---- BuildCarePlan all conditions for full coverage ----

func TestBuildCarePlan_CKD(t *testing.T) {
	now := time.Now()
	p := BuildCarePlan("c1", "p1", "ckd", now)
	if p.Medications[0].Name != "Losartan" {
		t.Errorf("expected Losartan for CKD")
	}
}

func TestBuildCarePlan_COPD(t *testing.T) {
	now := time.Now()
	p := BuildCarePlan("c1", "p1", "copd", now)
	if p.Medications[0].Name != "Tiotropium" {
		t.Errorf("expected Tiotropium for COPD")
	}
}

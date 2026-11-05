package perturbation_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/eval/perturbation"
)

// sampleMedicalTrace returns a four-turn medical conversation for testing.
func sampleMedicalTrace() perturbation.ConversationTrace {
	return perturbation.ConversationTrace{
		ID: "medical-001",
		Turns: []perturbation.Turn{
			{Role: "user", Content: "What is the capital for Ottawa?"},
			{Role: "assistant", Content: "Ottawa is the capital of Canada."},
			{Role: "user", Content: "And diabetes? What are the symptoms?"},
			{Role: "assistant", Content: "Common symptoms of diabetes include increased thirst, frequent urination, and fatigue."},
		},
	}
}

// longerMedicalTrace returns a six-turn medical conversation with more depth.
func longerMedicalTrace() perturbation.ConversationTrace {
	return perturbation.ConversationTrace{
		ID: "medical-002",
		Turns: []perturbation.Turn{
			{Role: "user", Content: "What causes hypertension?"},
			{Role: "assistant", Content: "Hypertension can be caused by obesity, high sodium intake, genetics, and stress."},
			{Role: "user", Content: "What is the normal blood pressure range?"},
			{Role: "assistant", Content: "Normal blood pressure is typically around 120/80 mmHg."},
			{Role: "user", Content: "What medications are used to treat it?"},
			{Role: "assistant", Content: "Common medications include ACE inhibitors, ARBs, calcium channel blockers, and diuretics."},
		},
	}
}

// TestGeneratePerturbations_SampleTrace verifies that perturbations are
// generated for each user turn in the sample trace.
func TestGeneratePerturbations_SampleTrace(t *testing.T) {
	trace := sampleMedicalTrace()
	perturbs := perturbation.GeneratePerturbations(trace)

	if len(perturbs) == 0 {
		t.Fatal("expected at least one perturbation, got 0")
	}

	// The trace has 2 user turns (index 0 and 2).
	// Turn 0 (user): goal_change, add_ambiguity, add_constraint = 3
	//   (no contradict because no preceding assistant, no drop_context/repeat
	//    because index < 2)
	// Turn 2 (user): goal_change, add_ambiguity, contradict, add_constraint,
	//   drop_context, repeat_request = 6
	// Total expected: 9
	expected := 9
	if len(perturbs) != expected {
		t.Errorf("expected %d perturbations, got %d", expected, len(perturbs))
		for _, p := range perturbs {
			t.Logf("  type=%s turn=%d desc=%s", p.Type, p.TurnIndex, p.Description)
		}
	}
}

// TestGeneratePerturbations_LongerTrace verifies perturbation counts for a
// longer conversation.
func TestGeneratePerturbations_LongerTrace(t *testing.T) {
	trace := longerMedicalTrace()
	perturbs := perturbation.GeneratePerturbations(trace)

	if len(perturbs) == 0 {
		t.Fatal("expected perturbations, got 0")
	}

	// Verify we have at least one of each type.
	types := make(map[perturbation.PerturbationType]int)
	for _, p := range perturbs {
		types[p.Type]++
	}

	requiredTypes := []perturbation.PerturbationType{
		perturbation.PerturbGoalChange,
		perturbation.PerturbAddAmbiguity,
		perturbation.PerturbContradict,
		perturbation.PerturbAddConstraint,
		perturbation.PerturbDropContext,
		perturbation.PerturbRepeatRequest,
	}

	for _, rt := range requiredTypes {
		if types[rt] == 0 {
			t.Errorf("expected at least one perturbation of type %q, got 0", rt)
		}
	}
}

// TestGeneratePerturbations_EmptyTrace produces no perturbations.
func TestGeneratePerturbations_EmptyTrace(t *testing.T) {
	trace := perturbation.ConversationTrace{ID: "empty"}
	perturbs := perturbation.GeneratePerturbations(trace)
	if len(perturbs) != 0 {
		t.Errorf("expected 0 perturbations for empty trace, got %d", len(perturbs))
	}
}

// TestGeneratePerturbations_SingleUserTurn produces only non-contextual
// perturbation types.
func TestGeneratePerturbations_SingleUserTurn(t *testing.T) {
	trace := perturbation.ConversationTrace{
		ID: "single-turn",
		Turns: []perturbation.Turn{
			{Role: "user", Content: "What is diabetes?"},
		},
	}
	perturbs := perturbation.GeneratePerturbations(trace)

	// Single user turn at index 0: goal_change, add_ambiguity, add_constraint = 3
	// No contradict (no preceding assistant), no drop_context/repeat (index < 2).
	if len(perturbs) != 3 {
		t.Errorf("expected 3 perturbations for single user turn, got %d", len(perturbs))
		for _, p := range perturbs {
			t.Logf("  type=%s turn=%d", p.Type, p.TurnIndex)
		}
	}
}

// TestApplyPerturbation_TruncatesTrace verifies that applying a perturbation
// truncates the trace at the perturbed turn.
func TestApplyPerturbation_TruncatesTrace(t *testing.T) {
	trace := sampleMedicalTrace()
	p := perturbation.Perturbation{
		Type:      perturbation.PerturbGoalChange,
		TurnIndex: 2,
		Original:  "And diabetes? What are the symptoms?",
		Perturbed: "Actually, forget diabetes. What about asthma?",
	}

	result := perturbation.ApplyPerturbation(trace, p)

	// The result should have turns 0, 1, and the perturbed turn 2.
	if len(result.Turns) != 3 {
		t.Fatalf("expected 3 turns after perturbation, got %d", len(result.Turns))
	}

	// Turn 0 and 1 should be unchanged.
	if result.Turns[0].Content != trace.Turns[0].Content {
		t.Errorf("turn 0 changed: got %q, want %q", result.Turns[0].Content, trace.Turns[0].Content)
	}
	if result.Turns[1].Content != trace.Turns[1].Content {
		t.Errorf("turn 1 changed: got %q, want %q", result.Turns[1].Content, trace.Turns[1].Content)
	}

	// Turn 2 should be the perturbed message.
	if result.Turns[2].Content != p.Perturbed {
		t.Errorf("turn 2 not perturbed: got %q, want %q", result.Turns[2].Content, p.Perturbed)
	}

	// Turn 2 should be role "user".
	if result.Turns[2].Role != "user" {
		t.Errorf("perturbed turn role: got %q, want %q", result.Turns[2].Role, "user")
	}
}

// TestApplyPerturbation_AtFirstTurn verifies that perturbing turn 0 returns a
// single-turn trace.
func TestApplyPerturbation_AtFirstTurn(t *testing.T) {
	trace := sampleMedicalTrace()
	p := perturbation.Perturbation{
		Type:      perturbation.PerturbGoalChange,
		TurnIndex: 0,
		Perturbed: "What are the symptoms of asthma?",
	}

	result := perturbation.ApplyPerturbation(trace, p)
	if len(result.Turns) != 1 {
		t.Fatalf("expected 1 turn after perturbation at index 0, got %d", len(result.Turns))
	}
	if result.Turns[0].Content != p.Perturbed {
		t.Errorf("turn content: got %q, want %q", result.Turns[0].Content, p.Perturbed)
	}
}

// TestApplyPerturbation_OutOfRange returns the original trace unchanged.
func TestApplyPerturbation_OutOfRange(t *testing.T) {
	trace := sampleMedicalTrace()

	// Index too large.
	p := perturbation.Perturbation{TurnIndex: 100, Perturbed: "ignored"}
	result := perturbation.ApplyPerturbation(trace, p)
	if len(result.Turns) != len(trace.Turns) {
		t.Errorf("out-of-range high: expected %d turns, got %d", len(trace.Turns), len(result.Turns))
	}

	// Negative index.
	p2 := perturbation.Perturbation{TurnIndex: -1, Perturbed: "ignored"}
	result2 := perturbation.ApplyPerturbation(trace, p2)
	if len(result2.Turns) != len(trace.Turns) {
		t.Errorf("negative index: expected %d turns, got %d", len(trace.Turns), len(result2.Turns))
	}
}

// TestApplyPerturbation_IDSuffix verifies that the perturbed trace ID has a
// "/perturbed" suffix.
func TestApplyPerturbation_IDSuffix(t *testing.T) {
	trace := sampleMedicalTrace()
	p := perturbation.Perturbation{
		TurnIndex: 0,
		Perturbed: "test",
	}
	result := perturbation.ApplyPerturbation(trace, p)
	want := trace.ID + "/perturbed"
	if result.ID != want {
		t.Errorf("perturbed trace ID: got %q, want %q", result.ID, want)
	}
}

// TestApplyPerturbation_DoesNotMutateOriginal verifies that the original
// trace is not modified.
func TestApplyPerturbation_DoesNotMutateOriginal(t *testing.T) {
	trace := sampleMedicalTrace()
	origContent := trace.Turns[2].Content

	p := perturbation.Perturbation{
		TurnIndex: 2,
		Perturbed: "COMPLETELY DIFFERENT MESSAGE",
	}
	_ = perturbation.ApplyPerturbation(trace, p)

	if trace.Turns[2].Content != origContent {
		t.Errorf("original trace was mutated: got %q, want %q", trace.Turns[2].Content, origContent)
	}
}

// TestGoalChangePerturbation checks the goal change perturbation generator.
func TestGoalChangePerturbation(t *testing.T) {
	turn := perturbation.Turn{Role: "user", Content: "What causes diabetes?"}
	p := perturbation.GoalChangePerturbation(turn, 0)

	if p.Type != perturbation.PerturbGoalChange {
		t.Errorf("type: got %q, want %q", p.Type, perturbation.PerturbGoalChange)
	}
	if p.TurnIndex != 0 {
		t.Errorf("turn index: got %d, want 0", p.TurnIndex)
	}
	if !strings.Contains(p.Perturbed, "forget") {
		t.Errorf("perturbed message should contain 'forget': %q", p.Perturbed)
	}
	if !strings.Contains(p.Perturbed, "asthma") {
		t.Errorf("perturbed message should contain 'asthma': %q", p.Perturbed)
	}
	if p.Original != turn.Content {
		t.Errorf("original: got %q, want %q", p.Original, turn.Content)
	}
}

// TestAmbiguityPerturbation checks the ambiguity perturbation generator.
func TestAmbiguityPerturbation(t *testing.T) {
	turn := perturbation.Turn{Role: "user", Content: "What is the normal blood pressure range for an adult?"}
	p := perturbation.AmbiguityPerturbation(turn, 2)

	if p.Type != perturbation.PerturbAddAmbiguity {
		t.Errorf("type: got %q, want %q", p.Type, perturbation.PerturbAddAmbiguity)
	}
	if p.TurnIndex != 2 {
		t.Errorf("turn index: got %d, want 2", p.TurnIndex)
	}
	// The perturbed message should be vague (not contain the original specifics).
	if strings.Contains(p.Perturbed, "blood pressure") {
		t.Errorf("ambiguous message should not contain 'blood pressure': %q", p.Perturbed)
	}
}

// TestAmbiguityPerturbation_ShortMessage checks ambiguity for short messages.
func TestAmbiguityPerturbation_ShortMessage(t *testing.T) {
	turn := perturbation.Turn{Role: "user", Content: "What is it?"}
	p := perturbation.AmbiguityPerturbation(turn, 0)

	if p.Type != perturbation.PerturbAddAmbiguity {
		t.Errorf("type: got %q, want %q", p.Type, perturbation.PerturbAddAmbiguity)
	}
	if p.Perturbed == "" {
		t.Error("perturbed message should not be empty")
	}
}

// TestContradictionPerturbation checks the contradiction perturbation.
func TestContradictionPerturbation(t *testing.T) {
	assistant := perturbation.Turn{
		Role:    "assistant",
		Content: "Diabetes is caused by insulin resistance. There are two main types.",
	}
	user := perturbation.Turn{
		Role:    "user",
		Content: "What about treatment?",
	}

	p := perturbation.ContradictionPerturbation(assistant, user, 2)

	if p.Type != perturbation.PerturbContradict {
		t.Errorf("type: got %q, want %q", p.Type, perturbation.PerturbContradict)
	}
	if !strings.Contains(p.Perturbed, "not right") {
		t.Errorf("contradiction should contain 'not right': %q", p.Perturbed)
	}
}

// TestAddConstraintPerturbation checks the constraint perturbation.
func TestAddConstraintPerturbation(t *testing.T) {
	turn := perturbation.Turn{Role: "user", Content: "What medications treat hypertension?"}
	p := perturbation.AddConstraintPerturbation(turn, 2)

	if p.Type != perturbation.PerturbAddConstraint {
		t.Errorf("type: got %q, want %q", p.Type, perturbation.PerturbAddConstraint)
	}
	// The perturbed message should start with the original.
	if !strings.HasPrefix(p.Perturbed, turn.Content) {
		t.Errorf("perturbed should start with original: %q", p.Perturbed)
	}
	// Should contain "Also".
	if !strings.Contains(p.Perturbed, "Also") {
		t.Errorf("constraint should contain 'Also': %q", p.Perturbed)
	}
}

// TestAddConstraintPerturbation_DeterministicSelection verifies that the
// constraint selected varies by turn index.
func TestAddConstraintPerturbation_DeterministicSelection(t *testing.T) {
	turn := perturbation.Turn{Role: "user", Content: "What medications?"}
	p0 := perturbation.AddConstraintPerturbation(turn, 0)
	p1 := perturbation.AddConstraintPerturbation(turn, 1)

	if p0.Perturbed == p1.Perturbed {
		t.Errorf("constraints at different turn indices should differ: %q == %q", p0.Perturbed, p1.Perturbed)
	}
}

// TestDropContextPerturbation checks the drop context perturbation.
func TestDropContextPerturbation(t *testing.T) {
	turn := perturbation.Turn{Role: "user", Content: "What medications are used to treat it?"}
	p := perturbation.DropContextPerturbation(turn, 4)

	if p.Type != perturbation.PerturbDropContext {
		t.Errorf("type: got %q, want %q", p.Type, perturbation.PerturbDropContext)
	}
	if !strings.Contains(p.Perturbed, "mentioned before") {
		t.Errorf("drop context should reference earlier context: %q", p.Perturbed)
	}
}

// TestRepeatRequestPerturbation checks the repeat request perturbation.
func TestRepeatRequestPerturbation(t *testing.T) {
	earlier := perturbation.Turn{Role: "user", Content: "What causes hypertension?"}
	p := perturbation.RepeatRequestPerturbation(earlier, 4)

	if p.Type != perturbation.PerturbRepeatRequest {
		t.Errorf("type: got %q, want %q", p.Type, perturbation.PerturbRepeatRequest)
	}
	if p.Perturbed != earlier.Content {
		t.Errorf("repeat should be identical: got %q, want %q", p.Perturbed, earlier.Content)
	}
}

// TestRunPerturbationSuite_NilExecutor marks all results as failed with
// "no executor provided".
func TestRunPerturbationSuite_NilExecutor(t *testing.T) {
	traces := []perturbation.ConversationTrace{sampleMedicalTrace()}
	results := perturbation.RunPerturbationSuite(traces, nil, nil)

	if len(results) == 0 {
		t.Fatal("expected results, got 0")
	}

	for _, r := range results {
		if r.StillCorrect {
			t.Errorf("result for %s should not be correct with nil executor", r.Perturbation.Type)
		}
		if r.FailureMode != "no executor provided" {
			t.Errorf("failure mode: got %q, want %q", r.FailureMode, "no executor provided")
		}
	}
}

// TestRunPerturbationSuite_ExecutorError marks results as failed when the
// executor returns an error.
func TestRunPerturbationSuite_ExecutorError(t *testing.T) {
	executor := func(_ context.Context, _ []perturbation.Turn) (string, error) {
		return "", fmt.Errorf("LLM unavailable")
	}

	traces := []perturbation.ConversationTrace{sampleMedicalTrace()}
	results := perturbation.RunPerturbationSuite(traces, executor, nil)

	for _, r := range results {
		if r.StillCorrect {
			t.Errorf("result for %s should not be correct on executor error", r.Perturbation.Type)
		}
		if !strings.Contains(r.FailureMode, "executor error") {
			t.Errorf("failure mode should contain 'executor error': %q", r.FailureMode)
		}
	}
}

// TestRunPerturbationSuite_WithMockExecutor runs the suite with a mock
// executor that always returns a valid response about asthma.
func TestRunPerturbationSuite_WithMockExecutor(t *testing.T) {
	executor := func(_ context.Context, turns []perturbation.Turn) (string, error) {
		// Return a response that mentions asthma (for goal_change checks)
		// and uses acknowledgment language (for contradiction checks).
		return "I understand your concern. Let me reconsider. Asthma is a chronic " +
			"respiratory condition affecting the airways. It can be managed with " +
			"inhalers and medications suitable for pediatric patients.", nil
	}

	traces := []perturbation.ConversationTrace{sampleMedicalTrace()}
	results := perturbation.RunPerturbationSuite(traces, executor, nil)

	if len(results) == 0 {
		t.Fatal("expected results, got 0")
	}

	passCount := 0
	for _, r := range results {
		if r.StillCorrect {
			passCount++
		}
	}

	// With a well-crafted mock response, most perturbations should pass.
	if passCount == 0 {
		t.Error("expected at least some perturbations to pass with mock executor")
	}

	t.Logf("passed %d / %d perturbations", passCount, len(results))
}

// TestRunPerturbationSuite_CustomChecker uses a custom checker that always
// passes.
func TestRunPerturbationSuite_CustomChecker(t *testing.T) {
	executor := func(_ context.Context, _ []perturbation.Turn) (string, error) {
		return "any response", nil
	}
	checker := func(_ perturbation.Perturbation, _ string) (bool, string) {
		return true, ""
	}

	traces := []perturbation.ConversationTrace{sampleMedicalTrace()}
	results := perturbation.RunPerturbationSuite(traces, executor, checker)

	for _, r := range results {
		if !r.StillCorrect {
			t.Errorf("custom checker should pass all: %s failed with %q",
				r.Perturbation.Type, r.FailureMode)
		}
	}
}

// TestRunPerturbationSuite_MedicalGoalChange tests the specific scenario from
// the spec: user changes from diabetes to asthma mid-conversation.
func TestRunPerturbationSuite_MedicalGoalChange(t *testing.T) {
	trace := perturbation.ConversationTrace{
		ID: "goal-change-test",
		Turns: []perturbation.Turn{
			{Role: "user", Content: "What is the capital for Ottawa?"},
			{Role: "assistant", Content: "Ottawa is the capital of Canada."},
			{Role: "user", Content: "And diabetes? What are the symptoms?"},
			{Role: "assistant", Content: "Common symptoms include increased thirst and frequent urination."},
		},
	}

	// Apply a goal change at turn 2.
	p := perturbation.Perturbation{
		Type:      perturbation.PerturbGoalChange,
		TurnIndex: 2,
		Original:  "And diabetes? What are the symptoms?",
		Perturbed: "Actually, forget diabetes. What about asthma?",
	}

	perturbed := perturbation.ApplyPerturbation(trace, p)

	// Verify the perturbed trace.
	if len(perturbed.Turns) != 3 {
		t.Fatalf("expected 3 turns, got %d", len(perturbed.Turns))
	}
	if perturbed.Turns[2].Content != "Actually, forget diabetes. What about asthma?" {
		t.Errorf("turn 2 not perturbed correctly: %q", perturbed.Turns[2].Content)
	}

	// Verify the assistant's earlier turn is preserved.
	if perturbed.Turns[1].Content != "Ottawa is the capital of Canada." {
		t.Errorf("turn 1 should be preserved: %q", perturbed.Turns[1].Content)
	}
}

// TestDefaultChecker_EmptyResponse fails for empty responses.
func TestDefaultChecker_EmptyResponse(t *testing.T) {
	p := perturbation.Perturbation{Type: perturbation.PerturbGoalChange}
	ok, failMode := perturbation.DefaultChecker(p, "")
	if ok {
		t.Error("empty response should fail")
	}
	if failMode != "empty response" {
		t.Errorf("failure mode: got %q, want %q", failMode, "empty response")
	}
}

// TestDefaultChecker_WhitespaceResponse fails for whitespace-only responses.
func TestDefaultChecker_WhitespaceResponse(t *testing.T) {
	p := perturbation.Perturbation{Type: perturbation.PerturbGoalChange}
	ok, _ := perturbation.DefaultChecker(p, "   \n\t  ")
	if ok {
		t.Error("whitespace-only response should fail")
	}
}

// TestDefaultChecker_GoalChange_MissingTopic fails when new topic is absent.
func TestDefaultChecker_GoalChange_MissingTopic(t *testing.T) {
	p := perturbation.Perturbation{Type: perturbation.PerturbGoalChange}
	ok, failMode := perturbation.DefaultChecker(p, "Here is information about diabetes.")
	if ok {
		t.Error("goal change response without new topic should fail")
	}
	if !strings.Contains(failMode, "asthma") {
		t.Errorf("failure mode should mention asthma: %q", failMode)
	}
}

// TestDefaultChecker_GoalChange_WithTopic passes when new topic is present.
func TestDefaultChecker_GoalChange_WithTopic(t *testing.T) {
	p := perturbation.Perturbation{Type: perturbation.PerturbGoalChange}
	ok, failMode := perturbation.DefaultChecker(p, "Asthma is a chronic respiratory condition.")
	if !ok {
		t.Errorf("goal change response with asthma should pass: %q", failMode)
	}
}

// TestDefaultChecker_Contradict_NoAcknowledgment fails without acknowledgment.
func TestDefaultChecker_Contradict_NoAcknowledgment(t *testing.T) {
	p := perturbation.Perturbation{Type: perturbation.PerturbContradict}
	ok, failMode := perturbation.DefaultChecker(p, "The temperature is 38 degrees.")
	if ok {
		t.Error("contradiction without acknowledgment should fail")
	}
	if !strings.Contains(failMode, "acknowledge") {
		t.Errorf("failure mode should mention acknowledgment: %q", failMode)
	}
}

// TestDefaultChecker_Contradict_WithAcknowledgment passes with ack words.
func TestDefaultChecker_Contradict_WithAcknowledgment(t *testing.T) {
	ackResponses := []string{
		"I understand your concern. Let me reconsider.",
		"You're right, I apologize for the error.",
		"Let me correct that information.",
	}

	p := perturbation.Perturbation{Type: perturbation.PerturbContradict}
	for _, resp := range ackResponses {
		ok, failMode := perturbation.DefaultChecker(p, resp)
		if !ok {
			t.Errorf("contradiction with ack %q should pass: %q", resp, failMode)
		}
	}
}

// TestDefaultChecker_OtherTypes_NonEmpty passes for non-empty responses.
func TestDefaultChecker_OtherTypes_NonEmpty(t *testing.T) {
	otherTypes := []perturbation.PerturbationType{
		perturbation.PerturbAddAmbiguity,
		perturbation.PerturbAddConstraint,
		perturbation.PerturbDropContext,
		perturbation.PerturbRepeatRequest,
	}

	for _, pt := range otherTypes {
		p := perturbation.Perturbation{Type: pt}
		ok, failMode := perturbation.DefaultChecker(p, "Some non-empty response.")
		if !ok {
			t.Errorf("type %q with non-empty response should pass: %q", pt, failMode)
		}
	}
}

// TestRunPerturbationSuite_MultipleTraces runs against multiple traces.
func TestRunPerturbationSuite_MultipleTraces(t *testing.T) {
	executor := func(_ context.Context, turns []perturbation.Turn) (string, error) {
		return "I understand. Asthma is a condition. Let me reconsider the treatment options.", nil
	}

	traces := []perturbation.ConversationTrace{
		sampleMedicalTrace(),
		longerMedicalTrace(),
	}

	results := perturbation.RunPerturbationSuite(traces, executor, nil)

	// We should have perturbations from both traces.
	traceIDs := make(map[string]bool)
	for _, r := range results {
		traceIDs[r.OriginalTrace.ID] = true
	}

	if len(traceIDs) != 2 {
		t.Errorf("expected results from 2 traces, got %d", len(traceIDs))
	}
}

// TestRunPerturbationSuite_PerturbedTraceHasResponse verifies that the
// perturbed trace includes the executor's response as the last turn.
func TestRunPerturbationSuite_PerturbedTraceHasResponse(t *testing.T) {
	const agentResponse = "I understand. Asthma is a chronic condition. Let me reconsider."

	executor := func(_ context.Context, _ []perturbation.Turn) (string, error) {
		return agentResponse, nil
	}

	traces := []perturbation.ConversationTrace{sampleMedicalTrace()}
	results := perturbation.RunPerturbationSuite(traces, executor, nil)

	for _, r := range results {
		lastTurn := r.PerturbedTrace.Turns[len(r.PerturbedTrace.Turns)-1]
		if lastTurn.Role != "assistant" {
			t.Errorf("last turn should be assistant, got %q", lastTurn.Role)
		}
		if lastTurn.Content != agentResponse {
			t.Errorf("last turn content: got %q, want %q", lastTurn.Content, agentResponse)
		}
	}
}

// TestPrintReport_NoResults does not panic on empty input.
func TestPrintReport_NoResults(t *testing.T) {
	perturbation.PrintReport(nil)
	perturbation.PrintReport([]perturbation.PerturbationResult{})
}

// TestPrintReport_WithResults does not panic on non-empty input.
func TestPrintReport_WithResults(t *testing.T) {
	results := []perturbation.PerturbationResult{
		{
			Perturbation: perturbation.Perturbation{
				Type:      perturbation.PerturbGoalChange,
				TurnIndex: 0,
			},
			StillCorrect: true,
		},
		{
			Perturbation: perturbation.Perturbation{
				Type:      perturbation.PerturbContradict,
				TurnIndex: 2,
			},
			StillCorrect: false,
			FailureMode:  "contradict: response does not acknowledge disagreement",
		},
	}

	perturbation.PrintReport(results)
}

// TestPerturbationTypes_StringValues verifies the string constants.
func TestPerturbationTypes_StringValues(t *testing.T) {
	tests := []struct {
		pt   perturbation.PerturbationType
		want string
	}{
		{perturbation.PerturbGoalChange, "goal_change"},
		{perturbation.PerturbAddAmbiguity, "add_ambiguity"},
		{perturbation.PerturbContradict, "contradict_agent"},
		{perturbation.PerturbAddConstraint, "add_constraint"},
		{perturbation.PerturbDropContext, "drop_context"},
		{perturbation.PerturbRepeatRequest, "repeat_request"},
	}

	for _, tc := range tests {
		if string(tc.pt) != tc.want {
			t.Errorf("PerturbationType %q: got %q, want %q", tc.pt, string(tc.pt), tc.want)
		}
	}
}

// TestGeneratePerturbations_OnlyUserTurnsPerturbed verifies that all
// generated perturbations target user turns only.
func TestGeneratePerturbations_OnlyUserTurnsPerturbed(t *testing.T) {
	trace := longerMedicalTrace()
	perturbs := perturbation.GeneratePerturbations(trace)

	for _, p := range perturbs {
		if p.TurnIndex < 0 || p.TurnIndex >= len(trace.Turns) {
			t.Errorf("perturbation at index %d is out of range", p.TurnIndex)
			continue
		}
		if trace.Turns[p.TurnIndex].Role != "user" {
			t.Errorf("perturbation at index %d targets %q, want 'user'",
				p.TurnIndex, trace.Turns[p.TurnIndex].Role)
		}
	}
}

// TestGeneratePerturbations_DescriptionNonEmpty verifies every perturbation
// has a non-empty description.
func TestGeneratePerturbations_DescriptionNonEmpty(t *testing.T) {
	trace := longerMedicalTrace()
	perturbs := perturbation.GeneratePerturbations(trace)

	for i, p := range perturbs {
		if p.Description == "" {
			t.Errorf("perturbation %d (type %q, turn %d) has empty description",
				i, p.Type, p.TurnIndex)
		}
	}
}

// TestRunPerturbationSuite_EmptyTraces returns no results.
func TestRunPerturbationSuite_EmptyTraces(t *testing.T) {
	results := perturbation.RunPerturbationSuite(nil, nil, nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil traces, got %d", len(results))
	}

	results2 := perturbation.RunPerturbationSuite([]perturbation.ConversationTrace{}, nil, nil)
	if len(results2) != 0 {
		t.Errorf("expected 0 results for empty traces, got %d", len(results2))
	}
}

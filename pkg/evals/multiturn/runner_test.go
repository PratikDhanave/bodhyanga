package multiturn

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// ---------- NewRunner ----------

func TestNewRunner_Defaults(t *testing.T) {
	r := NewRunner(RunConfig{})
	if r.cfg.PassThreshold != 0.6 {
		t.Errorf("PassThreshold: got %f, want 0.6", r.cfg.PassThreshold)
	}
	if r.cfg.Output == nil {
		t.Error("Output should default to os.Stdout")
	}
}

func TestNewRunner_CustomThreshold(t *testing.T) {
	r := NewRunner(RunConfig{PassThreshold: 0.8})
	if r.cfg.PassThreshold != 0.8 {
		t.Errorf("PassThreshold: got %f, want 0.8", r.cfg.PassThreshold)
	}
}

func TestNewRunner_CustomOutput(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(RunConfig{Output: &buf})
	if r.cfg.Output != &buf {
		t.Error("Output should be the custom writer")
	}
}

// ---------- LoadDataset ----------

func TestLoadDataset_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dataset.json")

	cases := []EvalCase{
		{
			Data: MultiTurnEvalData{
				Prompt: "Read config.json",
				MockTools: map[string]MockTool{
					"readFile": {Description: "Read file", Result: `{"key":"val"}`},
				},
			},
			Target: MultiTurnTarget{
				ExpectedToolOrder: []string{"readFile"},
				OriginalTask:      "Read config.json",
			},
		},
	}
	b, _ := json.Marshal(cases)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, err := LoadDataset(path)
	if err != nil {
		t.Fatalf("LoadDataset: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("len: got %d, want 1", len(loaded))
	}
	if loaded[0].Data.Prompt != "Read config.json" {
		t.Errorf("Prompt: got %q", loaded[0].Data.Prompt)
	}
}

func TestLoadDataset_FileNotFound(t *testing.T) {
	_, err := LoadDataset("/no/such/file.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadDataset_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not-json"), 0o644)

	_, err := LoadDataset(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------- Run + runOne + printReport ----------

func TestRunner_Run_AllPass(t *testing.T) {
	client := &seqMock{
		responses: []*llm.Response{
			newToolCallResp("toolu_1", "readFile"),
			newTextResp("The answer is 42."),
		},
	}

	var buf bytes.Buffer
	r := NewRunner(RunConfig{
		AgentClient:   client,
		PassThreshold: 0.6,
		Output:        &buf,
	})

	cases := []EvalCase{
		{
			Data: MultiTurnEvalData{
				Prompt: "Read config.json",
				MockTools: map[string]MockTool{
					"readFile": {Description: "Read file", Result: `{"answer":42}`},
				},
			},
			Target: MultiTurnTarget{
				ExpectedToolOrder: []string{"readFile"},
				OriginalTask:      "Read config.json",
			},
		},
	}

	scores, allPass := r.Run(context.Background(), cases)
	if !allPass {
		t.Error("expected allPass=true")
	}
	if len(scores) != 1 {
		t.Fatalf("scores: got %d, want 1", len(scores))
	}
	if scores[0].Overall < 0.6 {
		t.Errorf("Overall: got %f, want >= 0.6", scores[0].Overall)
	}

	// Verify the report was printed.
	output := buf.String()
	if !strings.Contains(output, "ToolOrder") {
		t.Errorf("report missing header; got %q", output)
	}
	if !strings.Contains(output, "Average") {
		t.Errorf("report missing average; got %q", output)
	}
}

func TestRunner_Run_Failure(t *testing.T) {
	// Agent calls wrong tool order.
	client := &seqMock{
		responses: []*llm.Response{
			newToolCallResp("toolu_1", "writeFile"),
			newTextResp("Done."),
		},
	}

	var buf bytes.Buffer
	r := NewRunner(RunConfig{
		AgentClient:   client,
		PassThreshold: 0.9,
		Output:        &buf,
	})

	cases := []EvalCase{
		{
			Data: MultiTurnEvalData{
				Prompt: "Read config.json",
				MockTools: map[string]MockTool{
					"readFile":  {Description: "Read file", Result: "data"},
					"writeFile": {Description: "Write file", Result: "ok"},
				},
			},
			Target: MultiTurnTarget{
				ExpectedToolOrder: []string{"readFile"},
				ForbiddenTools:    []string{"writeFile"},
				OriginalTask:      "Read config.json",
			},
		},
	}

	scores, allPass := r.Run(context.Background(), cases)
	if allPass {
		t.Error("expected allPass=false because wrong tool order + forbidden tool called")
	}
	if scores[0].ToolOrder != 0.0 {
		t.Errorf("ToolOrder: got %f, want 0.0", scores[0].ToolOrder)
	}
	if scores[0].ToolsAvoided != 0.0 {
		t.Errorf("ToolsAvoided: got %f, want 0.0", scores[0].ToolsAvoided)
	}
}

func TestRunner_Run_AgentError(t *testing.T) {
	// seqMock with no responses should not error at seqMock level but
	// we test the path where MultiTurnWithMocks itself errors.
	client := &seqMock{responses: []*llm.Response{newTextResp("ok")}}

	var buf bytes.Buffer
	r := NewRunner(RunConfig{
		AgentClient:   client,
		PassThreshold: 0.6,
		Output:        &buf,
	})

	// No prompt or messages → will error.
	cases := []EvalCase{
		{
			Data:   MultiTurnEvalData{MockTools: map[string]MockTool{}},
			Target: MultiTurnTarget{OriginalTask: "broken"},
		},
	}

	scores, allPass := r.Run(context.Background(), cases)
	if allPass {
		t.Error("expected allPass=false because of error")
	}
	if scores[0].Error == "" {
		t.Error("expected non-empty Error field")
	}
	if scores[0].Overall != 0 {
		t.Errorf("Overall on error: got %f, want 0", scores[0].Overall)
	}
}

func TestRunner_Run_WithJudge(t *testing.T) {
	// Agent client: just returns text directly.
	agentClient := &seqMock{
		responses: []*llm.Response{
			newTextResp("The answer is 42."),
		},
	}

	// Judge client: returns a valid judgeOutput JSON.
	judgeResp := `{"score":8,"reason":"good answer"}`
	judgeClient := &seqMock{
		responses: []*llm.Response{
			{Content: judgeResp, StopReason: "end_turn"},
		},
	}

	var buf bytes.Buffer
	r := NewRunner(RunConfig{
		AgentClient:   agentClient,
		JudgeClient:   judgeClient,
		PassThreshold: 0.5,
		Output:        &buf,
	})

	cases := []EvalCase{
		{
			Data: MultiTurnEvalData{
				Prompt:    "What is the answer?",
				MockTools: map[string]MockTool{},
			},
			Target: MultiTurnTarget{
				OriginalTask: "What is the answer?",
			},
		},
	}

	scores, allPass := r.Run(context.Background(), cases)
	if !allPass {
		t.Error("expected allPass=true")
	}
	if scores[0].OutputQuality != 0.8 {
		t.Errorf("OutputQuality: got %f, want 0.8 (score 8/10)", scores[0].OutputQuality)
	}
}

func TestRunner_Run_JudgeError(t *testing.T) {
	agentClient := &seqMock{
		responses: []*llm.Response{newTextResp("The answer.")},
	}
	// Judge returns invalid JSON.
	judgeClient := &seqMock{
		responses: []*llm.Response{
			{Content: "not json", StopReason: "end_turn"},
		},
	}

	var buf bytes.Buffer
	r := NewRunner(RunConfig{
		AgentClient:   agentClient,
		JudgeClient:   judgeClient,
		PassThreshold: 0.3,
		Output:        &buf,
	})

	cases := []EvalCase{
		{
			Data: MultiTurnEvalData{
				Prompt:    "Question?",
				MockTools: map[string]MockTool{},
			},
			Target: MultiTurnTarget{OriginalTask: "Question?"},
		},
	}

	scores, _ := r.Run(context.Background(), cases)
	// Judge error should set quality to 0.
	if scores[0].OutputQuality != 0 {
		t.Errorf("OutputQuality on judge error: got %f, want 0", scores[0].OutputQuality)
	}
	// Should have a warning in output.
	if !strings.Contains(buf.String(), "judge error") {
		t.Errorf("expected judge error warning in output; got %q", buf.String())
	}
}

// ---------- printReport with long task name ----------

func TestRunner_printReport_LongTaskName(t *testing.T) {
	agentClient := &seqMock{
		responses: []*llm.Response{newTextResp("ok")},
	}

	var buf bytes.Buffer
	r := NewRunner(RunConfig{
		AgentClient:   agentClient,
		PassThreshold: 0.6,
		Output:        &buf,
	})

	cases := []EvalCase{
		{
			Data: MultiTurnEvalData{
				Prompt:    "A question",
				MockTools: map[string]MockTool{},
			},
			Target: MultiTurnTarget{
				OriginalTask: "This is a very long task description that exceeds forty characters in length and should be truncated",
			},
		},
	}

	r.Run(context.Background(), cases)
	output := buf.String()
	// The long task name should be truncated to 40 chars with "..."
	if !strings.Contains(output, "...") {
		t.Errorf("expected truncated task name; got %q", output)
	}
}

// ---------- printReport with zero scores ----------

func TestRunner_printReport_EmptyScores(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(RunConfig{Output: &buf})
	r.printReport(nil, nil)
	// Should not panic and should print the header.
	if !strings.Contains(buf.String(), "ToolOrder") {
		t.Error("expected header in output")
	}
}

// ---------- SaveScores ----------

func TestSaveScores_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scores.json")

	scores := []EvalScore{
		{CaseIndex: 0, ToolOrder: 1.0, ToolsAvoided: 1.0, OutputQuality: 0.8, Overall: 0.93},
		{CaseIndex: 1, ToolOrder: 0.0, ToolsAvoided: 1.0, OutputQuality: 0.5, Overall: 0.5, Error: "tool order wrong"},
	}

	if err := SaveScores(path, scores); err != nil {
		t.Fatalf("SaveScores: %v", err)
	}

	// Read back and verify.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var loaded []EvalScore
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("len: got %d, want 2", len(loaded))
	}
	if loaded[0].Overall != 0.93 {
		t.Errorf("Overall: got %f, want 0.93", loaded[0].Overall)
	}
}

func TestSaveScores_BadPath(t *testing.T) {
	err := SaveScores("/no/such/dir/scores.json", nil)
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}

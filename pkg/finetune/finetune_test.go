package finetune_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/finetune"
)

// writeTempJSONL writes lines to a temp JSONL file and returns its path.
func writeTempJSONL(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dataset.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
	f.Close()
	return path
}

// sftJSON builds a valid SFT JSONL line.
func sftJSON(system, user, assistant string) string {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type line struct {
		Messages []msg `json:"messages"`
	}
	l := line{Messages: []msg{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
		{Role: "assistant", Content: assistant},
	}}
	b, _ := json.Marshal(l)
	return string(b)
}

// countNonEmptyLines counts lines that contain at least one non-whitespace character.
func countNonEmptyLines(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("countNonEmptyLines read %s: %v", path, err)
	}
	count := 0
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) != "" {
			count++
		}
	}
	return count
}

// ---------- ValidateDataset ----------

func TestValidateDataset_Valid(t *testing.T) {
	lines := []string{
		sftJSON("sys", "hello", "world"),
		sftJSON("sys", "what is this?", "a test"),
		sftJSON("sys", "another question", "another answer"),
	}
	path := writeTempJSONL(t, lines)

	stats, errs, err := finetune.ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("expected no validation errors, got: %v", errs)
	}
	if stats.Total != 3 {
		t.Errorf("Total = %d, want 3", stats.Total)
	}
	if stats.AvgPromptLen <= 0 {
		t.Errorf("AvgPromptLen should be positive, got %d", stats.AvgPromptLen)
	}
	if stats.AvgCompletionLen <= 0 {
		t.Errorf("AvgCompletionLen should be positive, got %d", stats.AvgCompletionLen)
	}
}

func TestValidateDataset_EmptyMessages(t *testing.T) {
	path := writeTempJSONL(t, []string{`{"messages":[]}`})

	_, errs, err := finetune.ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected I/O error: %v", err)
	}
	if len(errs) == 0 {
		t.Error("expected validation error for empty messages, got none")
	}
}

func TestValidateDataset_MissingAssistant(t *testing.T) {
	path := writeTempJSONL(t, []string{
		`{"messages":[{"role":"user","content":"hi"}]}`,
	})

	_, errs, err := finetune.ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected I/O error: %v", err)
	}
	if len(errs) == 0 {
		t.Error("expected validation error for missing assistant turn")
	}
}

func TestValidateDataset_InvalidJSON(t *testing.T) {
	path := writeTempJSONL(t, []string{`not-json`, sftJSON("s", "u", "a")})

	stats, errs, err := finetune.ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected I/O error: %v", err)
	}
	if len(errs) == 0 {
		t.Error("expected validation error for invalid JSON line")
	}
	if stats.Total != 2 {
		t.Errorf("Total = %d, want 2", stats.Total)
	}
}

func TestValidateDataset_FileNotFound(t *testing.T) {
	_, _, err := finetune.ValidateDataset("/no/such/file.jsonl")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestValidateDataset_UnknownRole(t *testing.T) {
	path := writeTempJSONL(t, []string{
		`{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"},{"role":"robot","content":"beep"}]}`,
	})
	_, errs, err := finetune.ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected I/O error: %v", err)
	}
	if len(errs) == 0 {
		t.Error("expected validation error for unknown role 'robot'")
	}
}

// ---------- SplitDataset ----------

func TestSplitDataset_Basic(t *testing.T) {
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, sftJSON("sys", "question", "answer"))
	}
	path := writeTempJSONL(t, lines)

	dir := t.TempDir()
	trainPath := filepath.Join(dir, "train.jsonl")
	valPath := filepath.Join(dir, "val.jsonl")

	if err := finetune.SplitDataset(path, trainPath, valPath, 0.1); err != nil {
		t.Fatalf("SplitDataset error: %v", err)
	}

	trainLines := countNonEmptyLines(t, trainPath)
	valLines := countNonEmptyLines(t, valPath)

	if trainLines+valLines != 10 {
		t.Errorf("train(%d)+val(%d) = %d, want 10", trainLines, valLines, trainLines+valLines)
	}
	if valLines != 1 {
		t.Errorf("valLines = %d, want 1", valLines)
	}
	if trainLines != 9 {
		t.Errorf("trainLines = %d, want 9", trainLines)
	}
}

func TestSplitDataset_LargerRatio(t *testing.T) {
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, sftJSON("sys", "question", "answer"))
	}
	path := writeTempJSONL(t, lines)

	dir := t.TempDir()
	trainPath := filepath.Join(dir, "train.jsonl")
	valPath := filepath.Join(dir, "val.jsonl")

	if err := finetune.SplitDataset(path, trainPath, valPath, 0.2); err != nil {
		t.Fatalf("SplitDataset error: %v", err)
	}

	trainLines := countNonEmptyLines(t, trainPath)
	valLines := countNonEmptyLines(t, valPath)

	if trainLines+valLines != 20 {
		t.Errorf("train(%d)+val(%d) = %d, want 20", trainLines, valLines, trainLines+valLines)
	}
	if valLines != 4 {
		t.Errorf("valLines = %d, want 4 (20*0.2)", valLines)
	}
}

func TestSplitDataset_InvalidRatio(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, []string{sftJSON("s", "u", "a")})

	for _, r := range []float64{0, 1, -0.1, 1.5} {
		if err := finetune.SplitDataset(path,
			filepath.Join(dir, "train.jsonl"),
			filepath.Join(dir, "val.jsonl"), r); err == nil {
			t.Errorf("valRatio=%f: expected error, got nil", r)
		}
	}
}

func TestSplitDataset_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	os.WriteFile(path, []byte{}, 0o644)

	if err := finetune.SplitDataset(path,
		filepath.Join(dir, "train.jsonl"),
		filepath.Join(dir, "val.jsonl"), 0.1); err == nil {
		t.Error("expected error for empty input file, got nil")
	}
}

// ---------- EstimateCost ----------

func TestEstimateCost_Positive(t *testing.T) {
	stats := finetune.DatasetStats{
		Total:            100,
		AvgPromptLen:     400,
		AvgCompletionLen: 100,
	}
	cost := finetune.EstimateCost(stats, "openai", "gpt-4o", 3)
	if cost <= 0 {
		t.Errorf("expected positive cost, got %f", cost)
	}
}

func TestEstimateCost_ZeroEpochs(t *testing.T) {
	stats := finetune.DatasetStats{
		Total:            10,
		AvgPromptLen:     400,
		AvgCompletionLen: 100,
	}
	cost := finetune.EstimateCost(stats, "openai", "gpt-3.5-turbo", 0)
	if cost <= 0 {
		t.Errorf("expected positive cost with 0 epochs (defaults to 1), got %f", cost)
	}
}

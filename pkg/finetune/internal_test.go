package finetune

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------- pricePerMillion ----------

func TestPricePerMillion_OpenAI_GPT4o(t *testing.T) {
	p := pricePerMillion("openai", "gpt-4o-mini")
	if p != 25.00 {
		t.Errorf("got %f, want 25.00", p)
	}
}

func TestPricePerMillion_OpenAI_GPT4(t *testing.T) {
	p := pricePerMillion("openai", "gpt-4-turbo")
	if p != 8.00 {
		t.Errorf("got %f, want 8.00", p)
	}
}

func TestPricePerMillion_OpenAI_GPT35(t *testing.T) {
	p := pricePerMillion("openai", "gpt-3.5-turbo")
	if p != 0.80 {
		t.Errorf("got %f, want 0.80", p)
	}
}

func TestPricePerMillion_OpenAI_Unknown(t *testing.T) {
	p := pricePerMillion("openai", "o1-preview")
	if p != 8.00 {
		t.Errorf("got %f, want 8.00 (default for unknown openai model)", p)
	}
}

func TestPricePerMillion_Anthropic(t *testing.T) {
	p := pricePerMillion("anthropic", "claude-3")
	if p != 15.00 {
		t.Errorf("got %f, want 15.00", p)
	}
}

func TestPricePerMillion_Unknown(t *testing.T) {
	p := pricePerMillion("cohere", "command-r")
	if p != 10.00 {
		t.Errorf("got %f, want 10.00 (default for unknown provider)", p)
	}
}

// ---------- mapOpenAIStatus ----------

func TestMapOpenAIStatus_AllBranches(t *testing.T) {
	cases := []struct{ in, want string }{
		{"validating_files", "queued"},
		{"queued", "queued"},
		{"running", "running"},
		{"succeeded", "succeeded"},
		{"failed", "failed"},
		{"cancelled", "failed"},
		{"whatever", "whatever"},
	}
	for _, tc := range cases {
		got := mapOpenAIStatus(tc.in)
		if got != tc.want {
			t.Errorf("mapOpenAIStatus(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------- parseOpenAIJob ----------

func TestParseOpenAIJob_HappyPath(t *testing.T) {
	body := []byte(`{"id":"ftjob-1","status":"running","model":"gpt-4o","training_file":"file-1","created_at":1700000000}`)
	job, err := parseOpenAIJob("openai", "", "", "", body)
	if err != nil {
		t.Fatalf("parseOpenAIJob: %v", err)
	}
	if job.JobID != "ftjob-1" {
		t.Errorf("JobID: %q", job.JobID)
	}
	if job.Status != "running" {
		t.Errorf("Status: %q", job.Status)
	}
	if job.Model != "gpt-4o" {
		t.Errorf("Model: %q (should come from response when override is empty)", job.Model)
	}
	if job.TrainFile != "file-1" {
		t.Errorf("TrainFile: %q", job.TrainFile)
	}
	if job.CreatedAt == "" {
		t.Error("CreatedAt should not be empty for non-zero timestamp")
	}
}

func TestParseOpenAIJob_OverrideModelAndTrainFile(t *testing.T) {
	body := []byte(`{"id":"ftjob-1","status":"queued","model":"gpt-3.5","training_file":"file-remote","created_at":0}`)
	job, err := parseOpenAIJob("openai", "gpt-4o", "file-local", "val-local", body)
	if err != nil {
		t.Fatalf("parseOpenAIJob: %v", err)
	}
	if job.Model != "gpt-4o" {
		t.Errorf("Model: %q, want override 'gpt-4o'", job.Model)
	}
	if job.TrainFile != "file-local" {
		t.Errorf("TrainFile: %q, want override 'file-local'", job.TrainFile)
	}
	if job.ValFile != "val-local" {
		t.Errorf("ValFile: %q, want 'val-local'", job.ValFile)
	}
	if job.CreatedAt != "" {
		t.Errorf("CreatedAt should be empty for 0, got %q", job.CreatedAt)
	}
}

func TestParseOpenAIJob_InvalidJSON(t *testing.T) {
	_, err := parseOpenAIJob("openai", "", "", "", []byte("not-json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseOpenAIJob_EmptyModelUsesResponse(t *testing.T) {
	body := []byte(`{"id":"ftjob-1","status":"queued","model":"gpt-4o","training_file":"file-1","created_at":0}`)
	job, err := parseOpenAIJob("openai", "", "", "", body)
	if err != nil {
		t.Fatal(err)
	}
	if job.Model != "gpt-4o" {
		t.Errorf("Model: %q, want 'gpt-4o' from response", job.Model)
	}
}

func TestParseOpenAIJob_EmptyTrainFileUsesResponse(t *testing.T) {
	body := []byte(`{"id":"ftjob-1","status":"queued","model":"gpt-4o","training_file":"file-resp","created_at":0}`)
	job, err := parseOpenAIJob("openai", "", "", "", body)
	if err != nil {
		t.Fatal(err)
	}
	if job.TrainFile != "file-resp" {
		t.Errorf("TrainFile: %q, want 'file-resp' from response", job.TrainFile)
	}
}

// ---------- writeLines ----------

func TestWriteLines_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.jsonl")
	lines := [][]byte{[]byte("line1"), []byte("line2")}
	if err := writeLines(path, lines); err != nil {
		t.Fatalf("writeLines: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "line1\nline2\n" {
		t.Errorf("got %q, want 'line1\\nline2\\n'", got)
	}
}

func TestWriteLines_BadPath(t *testing.T) {
	err := writeLines("/no/such/dir/out.jsonl", [][]byte{[]byte("x")})
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}

func TestWriteLines_EmptySlice(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := writeLines(path, nil); err != nil {
		t.Fatalf("writeLines: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ---------- tokenEstimate ----------

func TestTokenEstimate(t *testing.T) {
	got := tokenEstimate(400)
	if got != 100 {
		t.Errorf("got %f, want 100", got)
	}
}

func TestTokenEstimate_Zero(t *testing.T) {
	got := tokenEstimate(0)
	if got != 0 {
		t.Errorf("got %f, want 0", got)
	}
}

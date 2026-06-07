package finetune

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// ---------- helper: create temp JSONL for internal tests ----------

func writeInternalTempJSONL(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dataset.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp JSONL: %v", err)
	}
	return path
}

// ---------- helper: swap openAIBaseURL for a test server ----------

func setBaseURL(t *testing.T, url string) {
	t.Helper()
	old := openAIBaseURL
	openAIBaseURL = url
	t.Cleanup(func() { openAIBaseURL = old })
}

// ---------- UploadToOpenAI ----------

func TestUploadToOpenAI_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v1/files") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("Authorization header: %q", auth)
		}
		ct := r.Header.Get("Content-Type")
		if !strings.Contains(ct, "multipart/form-data") {
			t.Errorf("Content-Type: %q, want multipart/form-data", ct)
		}

		// Verify multipart fields.
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		purpose := r.FormValue("purpose")
		if purpose != "fine-tune" {
			t.Errorf("purpose field: %q, want 'fine-tune'", purpose)
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("FormFile: %v", err)
		}
		defer file.Close()
		if hdr.Filename != "train.jsonl" {
			t.Errorf("filename: %q, want 'train.jsonl'", hdr.Filename)
		}
		body, _ := io.ReadAll(file)
		if !strings.Contains(string(body), "hello") {
			t.Errorf("file content missing expected data")
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "file-abc123"})
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	path := writeInternalTempJSONL(t, `{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"world"}]}`+"\n")
	// Rename to train.jsonl to check filename propagation.
	trainPath := filepath.Join(filepath.Dir(path), "train.jsonl")
	os.Rename(path, trainPath)

	id, err := UploadToOpenAI("test-key", trainPath)
	if err != nil {
		t.Fatalf("UploadToOpenAI: %v", err)
	}
	if id != "file-abc123" {
		t.Errorf("file ID: %q, want 'file-abc123'", id)
	}
}

func TestUploadToOpenAI_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal server error"}`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	path := writeInternalTempJSONL(t, `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}]}`+"\n")
	_, err := UploadToOpenAI("test-key", path)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code 500: %v", err)
	}
}

func TestUploadToOpenAI_InvalidResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not-json`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	path := writeInternalTempJSONL(t, `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}]}`+"\n")
	_, err := UploadToOpenAI("test-key", path)
	if err == nil {
		t.Fatal("expected error for invalid JSON response, got nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parse: %v", err)
	}
}

func TestUploadToOpenAI_EmptyFileID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": ""})
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	path := writeInternalTempJSONL(t, `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}]}`+"\n")
	_, err := UploadToOpenAI("test-key", path)
	if err == nil {
		t.Fatal("expected error for empty file ID, got nil")
	}
	if !strings.Contains(err.Error(), "empty file ID") {
		t.Errorf("error should mention empty file ID: %v", err)
	}
}

func TestUploadToOpenAI_ConnectionRefused(t *testing.T) {
	// Point at a closed server to trigger client.Do error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // close immediately so connection is refused
	setBaseURL(t, url)

	path := writeInternalTempJSONL(t, `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}]}`+"\n")
	_, err := UploadToOpenAI("test-key", path)
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
}

func TestUploadToOpenAI_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	path := writeInternalTempJSONL(t, `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}]}`+"\n")
	_, err := UploadToOpenAI("test-key", path)
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention 429: %v", err)
	}
}

// ---------- CreateOpenAIJob ----------

func TestCreateOpenAIJob_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v1/fine_tuning/jobs") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("Authorization: %q", auth)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("Content-Type: %q", ct)
		}

		// Verify request body.
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		if payload["training_file"] != "file-abc" {
			t.Errorf("training_file: %q", payload["training_file"])
		}
		if payload["model"] != "gpt-4o" {
			t.Errorf("model: %q", payload["model"])
		}

		w.WriteHeader(http.StatusOK)
		resp := `{"id":"ftjob-xyz","status":"queued","model":"gpt-4o","training_file":"file-abc","created_at":1700000000}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	job, err := CreateOpenAIJob("test-key", "file-abc", "gpt-4o")
	if err != nil {
		t.Fatalf("CreateOpenAIJob: %v", err)
	}
	if job.JobID != "ftjob-xyz" {
		t.Errorf("JobID: %q", job.JobID)
	}
	if job.Status != "queued" {
		t.Errorf("Status: %q, want 'queued'", job.Status)
	}
	if job.Provider != "openai" {
		t.Errorf("Provider: %q", job.Provider)
	}
	if job.Model != "gpt-4o" {
		t.Errorf("Model: %q", job.Model)
	}
	if job.CreatedAt == "" {
		t.Error("CreatedAt should not be empty")
	}
}

func TestCreateOpenAIJob_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid model"}`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	_, err := CreateOpenAIJob("test-key", "file-abc", "invalid-model")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention 400: %v", err)
	}
}

func TestCreateOpenAIJob_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	setBaseURL(t, url)

	_, err := CreateOpenAIJob("test-key", "file-abc", "gpt-4o")
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestCreateOpenAIJob_InvalidResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid-json`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	_, err := CreateOpenAIJob("test-key", "file-abc", "gpt-4o")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

// ---------- PollOpenAIJob ----------

func TestPollOpenAIJob_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v1/fine_tuning/jobs/ftjob-123") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("Authorization: %q", auth)
		}

		w.WriteHeader(http.StatusOK)
		resp := `{"id":"ftjob-123","status":"running","model":"gpt-4o","training_file":"file-abc","created_at":1700000000}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	job, err := PollOpenAIJob("test-key", "ftjob-123")
	if err != nil {
		t.Fatalf("PollOpenAIJob: %v", err)
	}
	if job.JobID != "ftjob-123" {
		t.Errorf("JobID: %q", job.JobID)
	}
	if job.Status != "running" {
		t.Errorf("Status: %q, want 'running'", job.Status)
	}
	if job.Provider != "openai" {
		t.Errorf("Provider: %q", job.Provider)
	}
}

func TestPollOpenAIJob_Succeeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"ftjob-456","status":"succeeded","model":"gpt-4o","training_file":"file-abc","created_at":1700000000}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	job, err := PollOpenAIJob("test-key", "ftjob-456")
	if err != nil {
		t.Fatalf("PollOpenAIJob: %v", err)
	}
	if job.Status != "succeeded" {
		t.Errorf("Status: %q, want 'succeeded'", job.Status)
	}
}

func TestPollOpenAIJob_Failed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"ftjob-789","status":"failed","model":"gpt-4o","training_file":"file-abc","created_at":1700000000}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	job, err := PollOpenAIJob("test-key", "ftjob-789")
	if err != nil {
		t.Fatalf("PollOpenAIJob: %v", err)
	}
	if job.Status != "failed" {
		t.Errorf("Status: %q, want 'failed'", job.Status)
	}
}

func TestPollOpenAIJob_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server error"}`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	_, err := PollOpenAIJob("test-key", "ftjob-123")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500: %v", err)
	}
}

func TestPollOpenAIJob_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	setBaseURL(t, url)

	_, err := PollOpenAIJob("test-key", "ftjob-123")
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestPollOpenAIJob_InvalidResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not-json`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	_, err := PollOpenAIJob("test-key", "ftjob-123")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

// ---------- UploadToOpenAI — various status codes ----------

func TestUploadToOpenAI_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	path := writeInternalTempJSONL(t, `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}]}`+"\n")
	_, err := UploadToOpenAI("bad-key", path)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention 401: %v", err)
	}
}

// ---------- UploadToOpenAI — response with extra fields ----------

func TestUploadToOpenAI_ResponseWithExtraFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"file-extra","object":"file","bytes":1234,"purpose":"fine-tune"}`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	path := writeInternalTempJSONL(t, `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}]}`+"\n")
	id, err := UploadToOpenAI("test-key", path)
	if err != nil {
		t.Fatalf("UploadToOpenAI: %v", err)
	}
	if id != "file-extra" {
		t.Errorf("file ID: %q, want 'file-extra'", id)
	}
}

// ---------- CreateOpenAIJob — status mapping through the stack ----------

func TestCreateOpenAIJob_ValidatingFilesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"ftjob-vf","status":"validating_files","model":"gpt-4o","training_file":"file-abc","created_at":1700000000}`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	job, err := CreateOpenAIJob("test-key", "file-abc", "gpt-4o")
	if err != nil {
		t.Fatalf("CreateOpenAIJob: %v", err)
	}
	if job.Status != "queued" {
		t.Errorf("Status: %q, want 'queued' (mapped from validating_files)", job.Status)
	}
}

func TestCreateOpenAIJob_CancelledStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"ftjob-cn","status":"cancelled","model":"gpt-4o","training_file":"file-abc","created_at":1700000000}`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	job, err := CreateOpenAIJob("test-key", "file-abc", "gpt-4o")
	if err != nil {
		t.Fatalf("CreateOpenAIJob: %v", err)
	}
	if job.Status != "failed" {
		t.Errorf("Status: %q, want 'failed' (mapped from cancelled)", job.Status)
	}
}

// ---------- SplitDataset — blank lines in source ----------

func TestSplitDataset_BlankLinesInSource(t *testing.T) {
	content := `{"messages":[{"role":"user","content":"q1"},{"role":"assistant","content":"a1"}]}

{"messages":[{"role":"user","content":"q2"},{"role":"assistant","content":"a2"}]}

{"messages":[{"role":"user","content":"q3"},{"role":"assistant","content":"a3"}]}
`
	path := writeInternalTempJSONL(t, content)
	dir := t.TempDir()
	trainPath := filepath.Join(dir, "train.jsonl")
	valPath := filepath.Join(dir, "val.jsonl")

	err := SplitDataset(path, trainPath, valPath, 0.5)
	if err != nil {
		t.Fatalf("SplitDataset: %v", err)
	}

	trainN := countLinesInternal(t, trainPath)
	valN := countLinesInternal(t, valPath)
	if trainN+valN != 3 {
		t.Errorf("total: %d+%d=%d, want 3 (blank lines excluded)", trainN, valN, trainN+valN)
	}
}

func countLinesInternal(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	count := 0
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) != "" {
			count++
		}
	}
	return count
}

// ---------- SplitDataset — two lines with small ratio forces valN=1 ----------

func TestSplitDataset_TwoLinesSmallRatio(t *testing.T) {
	content := `{"messages":[{"role":"user","content":"q1"},{"role":"assistant","content":"a1"}]}
{"messages":[{"role":"user","content":"q2"},{"role":"assistant","content":"a2"}]}
`
	path := writeInternalTempJSONL(t, content)
	dir := t.TempDir()
	trainPath := filepath.Join(dir, "train.jsonl")
	valPath := filepath.Join(dir, "val.jsonl")

	// 2 * 0.01 = 0.02 -> int(0.02) = 0 -> clamped to 1
	err := SplitDataset(path, trainPath, valPath, 0.01)
	if err != nil {
		t.Fatalf("SplitDataset: %v", err)
	}

	valN := countLinesInternal(t, valPath)
	if valN != 1 {
		t.Errorf("valN: %d, want 1 (clamped from 0)", valN)
	}
	trainN := countLinesInternal(t, trainPath)
	if trainN != 1 {
		t.Errorf("trainN: %d, want 1", trainN)
	}
}

// ---------- ValidateDataset — unicode and escaped characters ----------

func TestValidateDataset_UnicodeContent(t *testing.T) {
	content := `{"messages":[{"role":"user","content":"Qual é o diagnóstico?"},{"role":"assistant","content":"É uma condição sintética."}]}
{"messages":[{"role":"user","content":"你好"},{"role":"assistant","content":"你好，请问有什么可以帮到您？"}]}
`
	path := writeInternalTempJSONL(t, content)
	stats, errs, err := ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("expected no validation errors for unicode, got: %v", errs)
	}
	if stats.Total != 2 {
		t.Errorf("Total: %d, want 2", stats.Total)
	}
}

func TestValidateDataset_EscapedCharacters(t *testing.T) {
	content := `{"messages":[{"role":"user","content":"line1\nline2\ttab"},{"role":"assistant","content":"response with \"quotes\" and \\backslashes"}]}
`
	path := writeInternalTempJSONL(t, content)
	stats, errs, err := ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("expected no validation errors, got: %v", errs)
	}
	if stats.Total != 1 {
		t.Errorf("Total: %d, want 1", stats.Total)
	}
}

// ---------- ValidateDataset — multiple errors in one line ----------

func TestValidateDataset_UnknownRoleOnly(t *testing.T) {
	// All messages have unknown roles: neither user/system nor assistant.
	content := `{"messages":[{"role":"bot","content":"hi"},{"role":"agent","content":"bye"}]}
`
	path := writeInternalTempJSONL(t, content)
	_, errs, err := ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have errors for: unknown role "bot", unknown role "agent",
	// missing user turn, missing assistant turn.
	if len(errs) < 3 {
		t.Errorf("expected at least 3 validation errors, got %d: %v", len(errs), errs)
	}
}

// ---------- ValidateDataset — negative score ----------

func TestValidateDataset_NegativeScore(t *testing.T) {
	content := `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}],"score":-1.5}
{"messages":[{"role":"user","content":"q2"},{"role":"assistant","content":"a2"}],"score":3.0}
`
	path := writeInternalTempJSONL(t, content)
	stats, errs, err := ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("expected no validation errors, got: %v", errs)
	}
	if stats.MinScore != -1.5 {
		t.Errorf("MinScore: %f, want -1.5", stats.MinScore)
	}
	if stats.MaxScore != 3.0 {
		t.Errorf("MaxScore: %f, want 3.0", stats.MaxScore)
	}
}

// ---------- ValidateDataset — single score ----------

func TestValidateDataset_SingleScore(t *testing.T) {
	content := `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}],"score":0.75}
`
	path := writeInternalTempJSONL(t, content)
	stats, _, err := ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.MinScore != 0.75 || stats.MaxScore != 0.75 {
		t.Errorf("Min/MaxScore: %f/%f, want 0.75/0.75", stats.MinScore, stats.MaxScore)
	}
}

// ---------- ValidateDataset — score zero value ----------

func TestValidateDataset_ZeroScore(t *testing.T) {
	content := `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}],"score":0.0}
`
	path := writeInternalTempJSONL(t, content)
	stats, _, err := ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.MinScore != 0.0 {
		t.Errorf("MinScore: %f, want 0.0", stats.MinScore)
	}
	if stats.MaxScore != 0.0 {
		t.Errorf("MaxScore: %f, want 0.0", stats.MaxScore)
	}
}

// ---------- ValidateDataset — no score fields ----------

func TestValidateDataset_NoScoreFields(t *testing.T) {
	content := `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}]}
`
	path := writeInternalTempJSONL(t, content)
	stats, _, err := ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When no scores are present, MinScore and MaxScore should be zero values.
	if stats.MinScore != 0.0 || stats.MaxScore != 0.0 {
		t.Errorf("Min/MaxScore: %f/%f, want 0.0/0.0 when no scores", stats.MinScore, stats.MaxScore)
	}
}

// ---------- ValidateDataset — system-only turn (no user) ----------

func TestValidateDataset_SystemOnlyCountsAsUser(t *testing.T) {
	// "system" role satisfies the "hasUser" check, so system + assistant is valid.
	content := `{"messages":[{"role":"system","content":"sys prompt"},{"role":"assistant","content":"response"}]}
`
	path := writeInternalTempJSONL(t, content)
	_, errs, err := ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("expected no validation errors for system+assistant, got: %v", errs)
	}
}

// ---------- EstimateCost — edge cases ----------

func TestEstimateCost_ZeroTotal(t *testing.T) {
	stats := DatasetStats{Total: 0, AvgPromptLen: 100, AvgCompletionLen: 50}
	cost := EstimateCost(stats, "openai", "gpt-4o", 3)
	if cost != 0 {
		t.Errorf("expected zero cost for zero total, got %f", cost)
	}
}

func TestEstimateCost_NegativeEpochs(t *testing.T) {
	stats := DatasetStats{Total: 10, AvgPromptLen: 400, AvgCompletionLen: 100}
	costNeg := EstimateCost(stats, "openai", "gpt-4o", -5)
	costOne := EstimateCost(stats, "openai", "gpt-4o", 1)
	if costNeg != costOne {
		t.Errorf("negative epochs should default to 1: costNeg=%f, costOne=%f", costNeg, costOne)
	}
}

func TestEstimateCost_MultipleEpochs(t *testing.T) {
	stats := DatasetStats{Total: 100, AvgPromptLen: 400, AvgCompletionLen: 100}
	cost1 := EstimateCost(stats, "openai", "gpt-4o", 1)
	cost3 := EstimateCost(stats, "openai", "gpt-4o", 3)
	// cost3 should be exactly 3x cost1.
	if cost3 != cost1*3 {
		t.Errorf("cost3=%f, want 3*cost1=%f", cost3, cost1*3)
	}
}

// ---------- PollOpenAIJob — validating_files status mapped to queued ----------

func TestPollOpenAIJob_ValidatingFilesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"ftjob-vf","status":"validating_files","model":"gpt-4o","training_file":"file-abc","created_at":1700000000}`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	job, err := PollOpenAIJob("test-key", "ftjob-vf")
	if err != nil {
		t.Fatalf("PollOpenAIJob: %v", err)
	}
	if job.Status != "queued" {
		t.Errorf("Status: %q, want 'queued' (mapped from validating_files)", job.Status)
	}
}

// ---------- PollOpenAIJob — cancelled status mapped to failed ----------

func TestPollOpenAIJob_CancelledStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"ftjob-cn","status":"cancelled","model":"gpt-4o","training_file":"file-abc","created_at":1700000000}`))
	}))
	defer srv.Close()
	setBaseURL(t, srv.URL)

	job, err := PollOpenAIJob("test-key", "ftjob-cn")
	if err != nil {
		t.Fatalf("PollOpenAIJob: %v", err)
	}
	if job.Status != "failed" {
		t.Errorf("Status: %q, want 'failed' (mapped from cancelled)", job.Status)
	}
}

// ---------- writeLines — verify each line gets a trailing newline ----------

func TestWriteLines_MultipleLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.jsonl")
	lines := [][]byte{
		[]byte(`{"a":1}`),
		[]byte(`{"b":2}`),
		[]byte(`{"c":3}`),
	}
	if err := writeLines(path, lines); err != nil {
		t.Fatalf("writeLines: %v", err)
	}
	got, _ := os.ReadFile(path)
	want := "{\"a\":1}\n{\"b\":2}\n{\"c\":3}\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ---------- DatasetStats and FineTuneJob struct fields ----------

func TestDatasetStats_Defaults(t *testing.T) {
	var s DatasetStats
	if s.Total != 0 || s.TrainCount != 0 || s.ValCount != 0 {
		t.Errorf("zero-value DatasetStats should have all zero fields")
	}
}

func TestFineTuneJob_Fields(t *testing.T) {
	j := FineTuneJob{
		Provider:  "openai",
		Model:     "gpt-4o",
		TrainFile: "train.jsonl",
		ValFile:   "val.jsonl",
		JobID:     "ftjob-test",
		Status:    "running",
		CreatedAt: "2025-06-01T00:00:00Z",
	}
	if j.Provider != "openai" || j.JobID != "ftjob-test" {
		t.Errorf("FineTuneJob fields not set correctly")
	}
}

// ---------- ValidateDataset — large content (exercises buffer size) ----------

func TestValidateDataset_LargeContent(t *testing.T) {
	// Create a line with a large content field (>1MB) to test buffer sizing.
	bigContent := strings.Repeat("x", 2*1024*1024) // 2MB
	content := `{"messages":[{"role":"user","content":"` + bigContent + `"},{"role":"assistant","content":"ok"}]}` + "\n"
	path := writeInternalTempJSONL(t, content)
	stats, errs, err := ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error with large content: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("unexpected validation errors: %v", errs)
	}
	if stats.Total != 1 {
		t.Errorf("Total: %d, want 1", stats.Total)
	}
	if stats.AvgPromptLen <= 0 {
		t.Errorf("AvgPromptLen should be positive for large content, got %d", stats.AvgPromptLen)
	}
}

// ---------- SplitDataset — all lines go to val when ratio is very high ----------

func TestSplitDataset_HighRatio(t *testing.T) {
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}]}`)
	}
	content := strings.Join(lines, "\n") + "\n"
	path := writeInternalTempJSONL(t, content)
	dir := t.TempDir()
	trainPath := filepath.Join(dir, "train.jsonl")
	valPath := filepath.Join(dir, "val.jsonl")

	// 5 * 0.99 = 4.95 -> valN=4, trainN=1
	err := SplitDataset(path, trainPath, valPath, 0.99)
	if err != nil {
		t.Fatalf("SplitDataset: %v", err)
	}
	valN := countLinesInternal(t, valPath)
	trainN := countLinesInternal(t, trainPath)
	if valN+trainN != 5 {
		t.Errorf("total: %d+%d=%d, want 5", trainN, valN, trainN+valN)
	}
	if valN != 4 {
		t.Errorf("valN: %d, want 4", valN)
	}
}

// ---------- ValidateDataset — mixed valid and invalid lines ----------

func TestValidateDataset_MixedValidInvalid(t *testing.T) {
	content := `{"messages":[{"role":"user","content":"q1"},{"role":"assistant","content":"a1"}]}
not-json-at-all
{"messages":[]}
{"messages":[{"role":"user","content":"q2"}]}
{"messages":[{"role":"user","content":"q3"},{"role":"assistant","content":"a3"}]}
`
	path := writeInternalTempJSONL(t, content)
	stats, errs, err := ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 5 non-empty lines total.
	if stats.Total != 5 {
		t.Errorf("Total: %d, want 5", stats.Total)
	}
	// Errors: line 2 (invalid JSON), line 3 (empty messages), line 4 (missing assistant).
	if len(errs) != 3 {
		t.Errorf("expected 3 validation errors, got %d: %v", len(errs), errs)
	}
}

// ---------- ValidateDataset — only system and assistant (valid) ----------

func TestValidateDataset_SystemAndAssistant(t *testing.T) {
	content := `{"messages":[{"role":"system","content":"You are a synthetic clinical assistant."},{"role":"assistant","content":"Understood."}]}
`
	path := writeInternalTempJSONL(t, content)
	_, errs, err := ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("system+assistant should be valid, got: %v", errs)
	}
}

// ---------- ValidateDataset — deeply nested JSON (valid) ----------

func TestValidateDataset_ExtraFieldsIgnored(t *testing.T) {
	content := `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}],"metadata":{"case":"SYNTH-001"},"extra":true}
`
	path := writeInternalTempJSONL(t, content)
	stats, errs, err := ValidateDataset(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("extra fields should be ignored: %v", errs)
	}
	if stats.Total != 1 {
		t.Errorf("Total: %d, want 1", stats.Total)
	}
}

// ---------- SplitDataset — writeLines failure via read-only directory ----------

func TestSplitDataset_TrainWriteError(t *testing.T) {
	content := `{"messages":[{"role":"user","content":"q1"},{"role":"assistant","content":"a1"}]}
{"messages":[{"role":"user","content":"q2"},{"role":"assistant","content":"a2"}]}
`
	path := writeInternalTempJSONL(t, content)
	dir := t.TempDir()

	// Create a read-only directory for train output so writeLines fails.
	roDir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(roDir, 0o555); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { os.Chmod(roDir, 0o755) })

	trainPath := filepath.Join(roDir, "train.jsonl")
	valPath := filepath.Join(dir, "val.jsonl")

	err := SplitDataset(path, trainPath, valPath, 0.5)
	if err == nil {
		// On some systems (e.g., root user), this may succeed. Skip rather than fail.
		t.Skip("read-only directory did not prevent write (may be running as root)")
	}
}

// ---------- SplitDataset — MkdirAll failure via unwritable parent ----------

func TestSplitDataset_MkdirAllError(t *testing.T) {
	content := `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}]}
`
	path := writeInternalTempJSONL(t, content)
	dir := t.TempDir()

	// Create a read-only directory, then try to create subdirs under it.
	roDir := filepath.Join(dir, "noperm")
	if err := os.MkdirAll(roDir, 0o555); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { os.Chmod(roDir, 0o755) })

	trainPath := filepath.Join(roDir, "sub", "train.jsonl")
	valPath := filepath.Join(dir, "val.jsonl")

	err := SplitDataset(path, trainPath, valPath, 0.5)
	if err == nil {
		t.Skip("MkdirAll did not fail (may be running as root)")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Errorf("error should mention mkdir: %v", err)
	}
}

// ---------- writeLines — write to a path where file already exists as directory ----------

func TestWriteLines_PathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	// Create a directory where the file should go, so os.Create fails.
	badPath := filepath.Join(dir, "isdir")
	os.MkdirAll(badPath, 0o755)
	err := writeLines(badPath, [][]byte{[]byte("x")})
	if err == nil {
		t.Fatal("expected error writing to directory path, got nil")
	}
}

// ---------- ValidateDataset — line exceeds scanner buffer (triggers scanner.Err) ----------

func TestValidateDataset_LineTooLong(t *testing.T) {
	// The scanner buffer max is 10 MB. Write a single line >10 MB to trigger
	// scanner.Err() returning bufio.ErrTooLong.
	if testing.Short() {
		t.Skip("skipping large allocation test in short mode")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Write a line that exceeds 10MB without a newline.
	bigLine := strings.Repeat("x", 11*1024*1024) // 11 MB, no newline
	f.WriteString(bigLine)
	f.Close()

	_, _, err = ValidateDataset(path)
	if err == nil {
		t.Fatal("expected scanner error for line exceeding buffer, got nil")
	}
	if !strings.Contains(err.Error(), "scan") {
		t.Errorf("error should mention scan: %v", err)
	}
}

// ---------- SplitDataset — line exceeds scanner buffer (triggers scanner.Err) ----------

func TestSplitDataset_LineTooLong(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large allocation test in short mode")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	bigLine := strings.Repeat("x", 11*1024*1024)
	f.WriteString(bigLine)
	f.Close()

	trainPath := filepath.Join(dir, "train.jsonl")
	valPath := filepath.Join(dir, "val.jsonl")

	err = SplitDataset(path, trainPath, valPath, 0.2)
	if err == nil {
		t.Fatal("expected scanner error for line exceeding buffer, got nil")
	}
	if !strings.Contains(err.Error(), "scan") {
		t.Errorf("error should mention scan: %v", err)
	}
}

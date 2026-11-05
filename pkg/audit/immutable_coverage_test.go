package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestNewImmutableRecorder_EmptyPath checks that an empty filePath is rejected.
func TestNewImmutableRecorder_EmptyPath(t *testing.T) {
	_, err := NewImmutableRecorder("", nil, nil)
	if err == nil {
		t.Fatal("expected error for empty filePath")
	}
	if !strings.Contains(err.Error(), "empty filePath") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestNewImmutableRecorder_BadPath checks that an invalid filePath is rejected.
func TestNewImmutableRecorder_BadPath(t *testing.T) {
	_, err := NewImmutableRecorder("/nonexistent/dir/immutable.jsonl", nil, nil)
	if err == nil {
		t.Fatal("expected error for unwritable path")
	}
}

// TestImmutableRecorder_RecordAndVerifyChain records events and verifies the chain.
func TestImmutableRecorder_RecordAndVerifyChain(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "immutable-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()

	rec, err := NewImmutableRecorder(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	// Record multiple events.
	for i := 0; i < 5; i++ {
		if err := rec.Record(Event{Kind: KindMessage, AgentID: "test"}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	// Verify intact chain.
	ok, brokenAt, err := rec.VerifyChain(path)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatalf("chain broken at seq %d", brokenAt)
	}
}

// TestImmutableRecorder_TamperedChain detects tampered events.
func TestImmutableRecorder_TamperedChain(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/tampered.jsonl"

	rec, err := NewImmutableRecorder(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Record 3 events.
	for i := 0; i < 3; i++ {
		if err := rec.Record(Event{Kind: KindMessage, AgentID: "agent"}); err != nil {
			t.Fatal(err)
		}
	}
	rec.Close()

	// Tamper with the middle line — change the agentID.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	var ce ChainedEvent
	if err := json.Unmarshal([]byte(lines[1]), &ce); err != nil {
		t.Fatal(err)
	}
	ce.Event.AgentID = "tampered-agent"
	tampered, _ := json.Marshal(ce)
	lines[1] = string(tampered)

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Re-open just to verify.
	rec2, err := NewImmutableRecorder(dir+"/verify.jsonl", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec2.Close()

	ok, brokenAt, err := rec2.VerifyChain(path)
	if err != nil {
		t.Fatalf("verify err: %v", err)
	}
	if ok {
		t.Fatal("expected tampered chain to be detected")
	}
	if brokenAt != 1 {
		t.Fatalf("expected brokenAt 1, got %d", brokenAt)
	}
}

// TestImmutableRecorder_PrevHashMismatch tests prev_hash break detection.
func TestImmutableRecorder_PrevHashMismatch(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/prevhash.jsonl"

	rec, err := NewImmutableRecorder(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if err := rec.Record(Event{Kind: KindMessage, AgentID: "a"}); err != nil {
			t.Fatal(err)
		}
	}
	rec.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var ce ChainedEvent
	json.Unmarshal([]byte(lines[2]), &ce)
	ce.PrevHash = "0000000000000000000000000000000000000000000000000000000000000000"
	fixed, _ := json.Marshal(ce)
	lines[2] = string(fixed)
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)

	rec2, _ := NewImmutableRecorder(dir+"/v.jsonl", nil, nil)
	defer rec2.Close()
	ok, _, _ := rec2.VerifyChain(path)
	if ok {
		t.Fatal("expected chain break from PrevHash mismatch")
	}
}

// TestImmutableRecorder_RestoreChain tests resume from existing file.
func TestImmutableRecorder_RestoreChain(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/restore.jsonl"

	// Write 2 events.
	rec, err := NewImmutableRecorder(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec.Record(Event{Kind: KindMessage, AgentID: "first"})
	rec.Record(Event{Kind: KindMessage, AgentID: "second"})
	rec.Close()

	// Reopen — should restore chain at seq=2.
	rec2, err := NewImmutableRecorder(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec2.Close()
	rec2.Record(Event{Kind: KindMessage, AgentID: "third"})

	// Verify full chain of 3.
	ok, brokenAt, err := rec2.VerifyChain(path)
	if err != nil || !ok {
		t.Fatalf("chain verification failed: ok=%v brokenAt=%d err=%v", ok, brokenAt, err)
	}
}

// TestImmutableRecorder_RestoreCorrupt tests restore from corrupt file.
func TestImmutableRecorder_RestoreCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/corrupt.jsonl"
	os.WriteFile(path, []byte("not valid json\n"), 0o600)

	// Non-fatal — starts fresh chain.
	rec, err := NewImmutableRecorder(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	// Should be able to record.
	if err := rec.Record(Event{Kind: KindMessage, AgentID: "fresh"}); err != nil {
		t.Fatal(err)
	}
}

// TestImmutableRecorder_EmptyFileRestore tests restore from empty file.
func TestImmutableRecorder_EmptyFileRestore(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/empty.jsonl"
	os.WriteFile(path, []byte(""), 0o600)

	rec, err := NewImmutableRecorder(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	if err := rec.Record(Event{Kind: KindMessage}); err != nil {
		t.Fatal(err)
	}
}

// TestImmutableRecorder_RecordWithInner tests forwarding to inner recorder.
func TestImmutableRecorder_RecordWithInner(t *testing.T) {
	dir := t.TempDir()
	inner := NewInMemory()
	rec, err := NewImmutableRecorder(dir+"/inner.jsonl", inner, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	rec.Record(Event{Kind: KindMessage, AgentID: "test"})
	events := inner.List()
	if len(events) != 1 {
		t.Fatalf("expected 1 inner event, got %d", len(events))
	}
}

// TestImmutableRecorder_List tests List delegates to inner.
func TestImmutableRecorder_ListDelegatesToInner(t *testing.T) {
	dir := t.TempDir()
	inner := NewInMemory()
	rec, err := NewImmutableRecorder(dir+"/list.jsonl", inner, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	rec.Record(Event{Kind: KindMessage})
	list := rec.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 via List(), got %d", len(list))
	}
}

// TestImmutableRecorder_ListNoInner tests List with no inner recorder.
func TestImmutableRecorder_ListNoInner(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewImmutableRecorder(dir+"/noinner.jsonl", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	if list := rec.List(); list != nil {
		t.Fatalf("expected nil List() with no inner, got %v", list)
	}
}

// TestImmutableRecorder_CloseIdempotent tests double-close is safe.
func TestImmutableRecorder_CloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewImmutableRecorder(dir+"/close.jsonl", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("second close should be nil, got: %v", err)
	}
}

// TestImmutableRecorder_VerifyChainBadFile tests verify on nonexistent file.
func TestImmutableRecorder_VerifyChainBadFile(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewImmutableRecorder(dir+"/vf.jsonl", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	ok, brokenAt, err := rec.VerifyChain("/nonexistent/file.jsonl")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if ok {
		t.Fatal("expected not ok")
	}
	if brokenAt != -1 {
		t.Fatalf("expected brokenAt=-1, got %d", brokenAt)
	}
}

// TestImmutableRecorder_VerifyChainParseError tests verify with unparseable lines.
func TestImmutableRecorder_VerifyChainParseError(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/parse.jsonl"
	os.WriteFile(path, []byte("{bad json\n"), 0o600)

	rec, _ := NewImmutableRecorder(dir+"/v.jsonl", nil, nil)
	defer rec.Close()

	ok, _, err := rec.VerifyChain(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if ok {
		t.Fatal("expected not ok")
	}
}

// TestImmutableRecorder_ExportToS3_NoSink tests ExportToS3 with no S3 sink.
func TestImmutableRecorder_ExportToS3_NoSink(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewImmutableRecorder(dir+"/nos3.jsonl", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	err = rec.ExportToS3(context.Background(), 10)
	if err == nil || !strings.Contains(err.Error(), "S3 not configured") {
		t.Fatalf("expected S3 not configured error, got: %v", err)
	}
}

// TestImmutableRecorder_ExportToS3_EmptyFile tests ExportToS3 with an empty file.
func TestImmutableRecorder_ExportToS3_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/empty_s3.jsonl"
	sink := &S3Sink{Bucket: "test", Prefix: "pfx/", Region: "us-east-1"}
	rec, err := NewImmutableRecorder(path, nil, sink)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	// Empty file — nothing to upload, should return nil.
	err = rec.ExportToS3(context.Background(), 0)
	if err != nil {
		t.Fatalf("expected nil for empty export, got: %v", err)
	}
}

// TestImmutableRecorder_ExportToS3_WithMockServer tests ExportToS3 with an httptest mock.
func TestImmutableRecorder_ExportToS3_WithMockServer(t *testing.T) {
	// Mock S3 server that accepts PUT.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "expected PUT", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	path := dir + "/s3export.jsonl"
	sink := &S3Sink{Bucket: "test-bucket", Prefix: "audit/", Region: "us-east-1", Endpoint: server.URL}

	t.Setenv("BODHYANGA_S3_ACCESS_KEY", "testkey")
	t.Setenv("BODHYANGA_S3_SECRET_KEY", "testsecret")

	rec, err := NewImmutableRecorder(path, nil, sink)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	// Record some events.
	for i := 0; i < 3; i++ {
		rec.Record(Event{Kind: KindMessage, AgentID: "export-test"})
	}

	// Export all lines.
	err = rec.ExportToS3(context.Background(), 0)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Export with line limit.
	err = rec.ExportToS3(context.Background(), 2)
	if err != nil {
		t.Fatalf("export limited: %v", err)
	}
}

// TestImmutableRecorder_ExportToS3_ServerError tests ExportToS3 when S3 returns error.
func TestImmutableRecorder_ExportToS3_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("access denied"))
	}))
	defer server.Close()

	dir := t.TempDir()
	path := dir + "/s3err.jsonl"
	sink := &S3Sink{Bucket: "b", Prefix: "", Region: "us-east-1", Endpoint: server.URL}

	t.Setenv("BODHYANGA_S3_ACCESS_KEY", "k")
	t.Setenv("BODHYANGA_S3_SECRET_KEY", "s")

	rec, _ := NewImmutableRecorder(path, nil, sink)
	defer rec.Close()

	rec.Record(Event{Kind: KindMessage})

	err := rec.ExportToS3(context.Background(), 0)
	if err == nil {
		t.Fatal("expected error from S3")
	}
}

// TestImmutableRecorder_RecordWithS3Fire tests the fire-and-forget S3 path.
func TestImmutableRecorder_RecordWithS3Fire(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	path := dir + "/s3fire.jsonl"
	sink := &S3Sink{Bucket: "b", Prefix: "p/", Region: "us-east-1", Endpoint: server.URL}

	t.Setenv("BODHYANGA_S3_ACCESS_KEY", "k")
	t.Setenv("BODHYANGA_S3_SECRET_KEY", "s")

	rec, _ := NewImmutableRecorder(path, nil, sink)
	defer rec.Close()

	// Record with S3 configured — this fires a goroutine.
	if err := rec.Record(Event{Kind: KindMessage}); err != nil {
		t.Fatal(err)
	}

	// Give the goroutine time to finish.
	time.Sleep(100 * time.Millisecond)
}

// TestS3PutHelpers tests helper functions: sha256Hex, hmacSHA256, deriveSigningKey, hostFromEndpoint.
func TestS3PutHelpers(t *testing.T) {
	// sha256Hex
	h := sha256Hex([]byte("hello"))
	if len(h) != 64 {
		t.Fatalf("sha256Hex length: %d", len(h))
	}

	// hmacSHA256
	mac := hmacSHA256([]byte("key"), []byte("data"))
	if len(mac) != 32 {
		t.Fatalf("hmacSHA256 length: %d", len(mac))
	}

	// deriveSigningKey
	dk := deriveSigningKey("secret", "20240101", "us-east-1", "s3")
	if len(dk) != 32 {
		t.Fatalf("deriveSigningKey length: %d", len(dk))
	}

	// hostFromEndpoint
	tests := []struct {
		endpoint, want string
	}{
		{"https://s3.amazonaws.com", "s3.amazonaws.com"},
		{"http://localhost:9000", "localhost:9000"},
		{"minio.local", "minio.local"},
	}
	for _, tt := range tests {
		got := hostFromEndpoint(tt.endpoint, "bucket")
		if got != tt.want {
			t.Errorf("hostFromEndpoint(%q) = %q, want %q", tt.endpoint, got, tt.want)
		}
	}
}

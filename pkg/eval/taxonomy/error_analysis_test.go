package taxonomy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewErrorAnalysisSession(t *testing.T) {
	s := NewErrorAnalysisSession()
	if s == nil {
		t.Fatal("NewErrorAnalysisSession returned nil")
	}
	if s.ID == "" {
		t.Error("session ID should not be empty")
	}
	if s.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if len(s.OpenCodes) != 0 {
		t.Error("OpenCodes should be empty")
	}
}

func TestAddOpenCode(t *testing.T) {
	s := NewErrorAnalysisSession()

	// First acceptable code.
	s.AddOpenCode("trace-1", "looks good", true)
	if len(s.OpenCodes) != 1 {
		t.Fatalf("expected 1 open code, got %d", len(s.OpenCodes))
	}
	if s.OpenCodes[0].FirstFailure {
		t.Error("acceptable code should not be marked FirstFailure")
	}

	// First unacceptable code should get FirstFailure.
	s.AddOpenCode("trace-2", "diagnosis wrong", false)
	if len(s.OpenCodes) != 2 {
		t.Fatalf("expected 2 open codes, got %d", len(s.OpenCodes))
	}
	if !s.OpenCodes[1].FirstFailure {
		t.Error("first unacceptable code should have FirstFailure=true")
	}

	// Second unacceptable code should NOT be FirstFailure.
	s.AddOpenCode("trace-3", "another wrong diagnosis", false)
	if s.OpenCodes[2].FirstFailure {
		t.Error("second unacceptable code should not be FirstFailure")
	}
}

func TestGroupIntoAxialCodes(t *testing.T) {
	s := NewErrorAnalysisSession()

	// Add some codes with similar observations (same 40-char prefix after lowering).
	// "hallucinated diagnosis in the patient re" is the shared 40-char key.
	s.AddOpenCode("t1", "hallucinated diagnosis in the patient record for cardiology", false)
	s.AddOpenCode("t2", "hallucinated diagnosis in the patient record for neurology", false)
	s.AddOpenCode("t3", "missing differential", false)
	s.AddOpenCode("t4", "all good", true) // should be excluded

	axial := s.GroupIntoAxialCodes()
	if len(axial) != 2 {
		t.Fatalf("expected 2 axial codes, got %d", len(axial))
	}

	// First group should have 2 traces (both share the first 40 chars).
	if len(axial[0].TraceIDs) != 2 {
		t.Errorf("first group should have 2 traces, got %d", len(axial[0].TraceIDs))
	}

	// Second group should have 1 trace.
	if len(axial[1].TraceIDs) != 1 {
		t.Errorf("second group should have 1 trace, got %d", len(axial[1].TraceIDs))
	}

	// AxialCodes should be stored on the session.
	if len(s.AxialCodes) != 2 {
		t.Errorf("session AxialCodes = %d, want 2", len(s.AxialCodes))
	}
}

func TestGroupIntoAxialCodesEmpty(t *testing.T) {
	s := NewErrorAnalysisSession()
	s.AddOpenCode("t1", "all good", true)

	axial := s.GroupIntoAxialCodes()
	if axial != nil {
		t.Errorf("expected nil axial codes when all acceptable, got %v", axial)
	}
}

func TestCheckSaturation(t *testing.T) {
	s := NewErrorAnalysisSession()

	// Not enough data for a window of 3.
	if s.CheckSaturation(3) {
		t.Error("should not be saturated with no data")
	}

	// Add the same type of failure repeatedly.
	for i := 0; i < 10; i++ {
		s.AddOpenCode("trace-pre", "repeated failure pattern", false)
	}

	// Now add a window of the same pattern.
	for i := 0; i < 5; i++ {
		s.AddOpenCode("trace-window", "repeated failure pattern", false)
	}

	// All codes in the window match existing keys, so saturated.
	if !s.CheckSaturation(5) {
		t.Error("expected saturation when window codes match existing patterns")
	}
	if !s.Saturated {
		t.Error("Saturated field should be true")
	}

	// Add a new type of failure.
	s.AddOpenCode("trace-new", "completely novel failure type", false)
	if s.CheckSaturation(1) {
		t.Error("should not be saturated after a novel failure in the window")
	}
	if s.Saturated {
		t.Error("Saturated field should be false")
	}
}

func TestCheckSaturationAllAcceptable(t *testing.T) {
	s := NewErrorAnalysisSession()
	for i := 0; i < 20; i++ {
		s.AddOpenCode("trace", "looks fine", true)
	}
	// When all codes are acceptable, the window contains no failing keys,
	// so no new failing keys appear. This should count as saturated.
	if !s.CheckSaturation(5) {
		t.Error("expected saturation when all codes are acceptable")
	}
}

func TestExportImportJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	// Build a session with some data.
	s := NewErrorAnalysisSession()
	s.AddOpenCode("t1", "observation one", true)
	s.AddOpenCode("t2", "observation two", false)
	s.GroupIntoAxialCodes()

	if err := s.ExportJSON(path); err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("exported file missing: %v", err)
	}

	// Import into a fresh session.
	s2 := NewErrorAnalysisSession()
	if err := s2.ImportJSON(path); err != nil {
		t.Fatalf("ImportJSON: %v", err)
	}

	if s2.ID != s.ID {
		t.Errorf("imported ID = %q, want %q", s2.ID, s.ID)
	}
	if len(s2.OpenCodes) != 2 {
		t.Errorf("imported OpenCodes = %d, want 2", len(s2.OpenCodes))
	}
	if len(s2.AxialCodes) != 1 {
		t.Errorf("imported AxialCodes = %d, want 1", len(s2.AxialCodes))
	}
}

func TestExportJSONCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dir", "session.json")

	s := NewErrorAnalysisSession()
	if err := s.ExportJSON(path); err != nil {
		t.Fatalf("ExportJSON to nested path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("nested file missing: %v", err)
	}
}

func TestImportJSONErrors(t *testing.T) {
	s := NewErrorAnalysisSession()

	// Non-existent file.
	if err := s.ImportJSON("/no/such/file.json"); err == nil {
		t.Error("expected error for non-existent file")
	}

	// Invalid JSON.
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportJSON(bad); err == nil {
		t.Error("expected error for invalid JSON")
	}

	// Valid JSON but no session.
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportJSON(empty); err == nil {
		t.Error("expected error for missing session data")
	}
}

func TestAnalysisStats(t *testing.T) {
	s := NewErrorAnalysisSession()
	s.AddOpenCode("t1", "ok", true)
	s.AddOpenCode("t2", "fail", false)
	s.AddOpenCode("t3", "ok2", true)
	s.GroupIntoAxialCodes()

	st := s.Stats()
	if st.TotalOpenCodes != 3 {
		t.Errorf("TotalOpenCodes = %d, want 3", st.TotalOpenCodes)
	}
	if st.AcceptableCount != 2 {
		t.Errorf("AcceptableCount = %d, want 2", st.AcceptableCount)
	}
	if st.UnacceptableCount != 1 {
		t.Errorf("UnacceptableCount = %d, want 1", st.UnacceptableCount)
	}
	if st.FirstFailureCount != 1 {
		t.Errorf("FirstFailureCount = %d, want 1", st.FirstFailureCount)
	}
	if st.TotalAxialCodes != 1 {
		t.Errorf("TotalAxialCodes = %d, want 1", st.TotalAxialCodes)
	}

	expectedRate := 2.0 / 3.0
	if st.AcceptableRate < expectedRate-0.01 || st.AcceptableRate > expectedRate+0.01 {
		t.Errorf("AcceptableRate = %f, want ~%f", st.AcceptableRate, expectedRate)
	}
}

func TestAnalysisStatsEmpty(t *testing.T) {
	s := NewErrorAnalysisSession()
	st := s.Stats()
	if st.TotalOpenCodes != 0 {
		t.Errorf("TotalOpenCodes = %d, want 0", st.TotalOpenCodes)
	}
	if st.AcceptableRate != 0 {
		t.Errorf("AcceptableRate = %f, want 0", st.AcceptableRate)
	}
}

func TestNormaliseKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello world"},
		{"  spaces  ", "spaces"},
		{"a very long observation that exceeds forty characters easily", "a very long observation that exceeds for"},
		{"", ""},
	}
	for _, tc := range tests {
		got := normaliseKey(tc.input)
		if got != tc.want {
			t.Errorf("normaliseKey(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

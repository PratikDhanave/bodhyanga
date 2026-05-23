package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// writeTinyPNG creates a 2x2 PNG at path and returns the path. Real PNG bytes
// so http.DetectContentType reports image/png.
func writeTinyPNG(t *testing.T, path string) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	img.Set(1, 1, color.RGBA{0, 0, 255, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	return path
}

func TestVisionAttachesImageWhenPathSet(t *testing.T) {
	state, encoded := newCaseStateJSON(t)
	imgPath := writeTinyPNG(t, filepath.Join(t.TempDir(), "cxr.png"))

	respJSON := `{"diagnosis":"Community-acquired pneumonia","confidence":0.9,"rationale":"infiltrate visible"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	base := &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}}
	v := &LLMDiagnosticianVision{Base: base}

	meta := map[string]any{"case_id": state.CaseID, "image_path": imgPath}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, meta)

	out, err := v.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mock.Calls))
	}
	if len(mock.Calls[0].Messages) == 0 {
		t.Fatalf("no messages on LLM request")
	}
	imgs := mock.Calls[0].Messages[0].Images
	if len(imgs) != 1 {
		t.Fatalf("expected 1 image attached, got %d", len(imgs))
	}
	if imgs[0].MediaType != "image/png" {
		t.Fatalf("MediaType=%q, want image/png", imgs[0].MediaType)
	}
	if len(imgs[0].Data) == 0 {
		t.Fatalf("image Data empty")
	}
	if !strings.Contains(mock.Calls[0].System, "medical image") {
		t.Fatalf("vision system addendum missing")
	}

	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Diagnosis == "" {
		t.Fatalf("empty diagnosis emitted")
	}
}

func TestVisionFallsThroughWhenNoImagePath(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	respJSON := `{"diagnosis":"CAP","confidence":0.9,"rationale":"r"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	v := &LLMDiagnosticianVision{Base: &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}}}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	if _, err := v.HandleMessage(context.Background(), msg, newFakeEnv()); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mock.Calls))
	}
	if len(mock.Calls[0].Messages[0].Images) != 0 {
		t.Fatalf("no images expected without image_path")
	}
	// Base prompt only — vision addendum should NOT have been appended.
	if strings.Contains(mock.Calls[0].System, "medical image") {
		t.Fatalf("vision system addendum should not appear when no image is supplied")
	}
}

func TestVisionHandlesMissingFileGracefully(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	respJSON := `{"diagnosis":"CAP","confidence":0.9,"rationale":"r"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	v := &LLMDiagnosticianVision{Base: &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}}}
	env := newFakeEnv()

	meta := map[string]any{"case_id": "x", "image_path": "/does/not/exist.png"}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, meta)
	out, err := v.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 proposal emitted")
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 LLM call (text-only path), got %d", len(mock.Calls))
	}
	if len(mock.Calls[0].Messages[0].Images) != 0 {
		t.Fatalf("expected no images attached when file missing")
	}
	if !containsLog(env, "image read failed") {
		t.Fatalf("expected fallthrough log, got %v", env.logs)
	}
}

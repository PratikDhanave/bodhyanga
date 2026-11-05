package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// LLMDiagnosticianVision is an LLM diagnostician that, when the incoming
// message carries an "image_path" entry in its Metadata, reads the referenced
// file and attaches it as an inline image on the user message. This lets
// vision-capable models inspect chest X-rays, ECG strips, etc. alongside the
// CaseState text.
//
// It wraps an LLMDiagnostician (the Base field) and reuses its prompt
// construction, RAG retrieval, fallback, and citation-reconciliation logic.
// The agent's ID remains "diagnostician" so routing is unchanged.
//
// On any image read / decode failure, the vision agent logs and delegates
// straight to the base diagnostician — so RAG and the rule-based fallback
// continue to work exactly as if the image had not been requested.
type LLMDiagnosticianVision struct {
	Base *LLMDiagnostician
}

func (a *LLMDiagnosticianVision) ID() string   { return "diagnostician" }
func (a *LLMDiagnosticianVision) Name() string { return "Diagnostician (LLM + Vision)" }
func (a *LLMDiagnosticianVision) Capabilities() []string {
	return []string{"provide_diagnosis", "differential_diagnosis", "llm_backed", "vision"}
}

func (a *LLMDiagnosticianVision) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.Base == nil {
		return nil, errors.New("vision diagnostician: nil Base")
	}
	imagePath, _ := msg.Metadata["image_path"].(string)
	if imagePath == "" {
		// No image attached — behave exactly like the base diagnostician.
		return a.Base.HandleMessage(ctx, msg, env)
	}

	img, err := readImage(imagePath)
	if err != nil {
		env.Logf("[diagnostician/vision] image read failed for %q: %v — falling through to text-only", imagePath, err)
		return a.Base.HandleMessage(ctx, msg, env)
	}

	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}

	proposal, llmErr := a.callLLMWithImage(ctx, state, img, env)
	if llmErr != nil {
		env.Logf("[diagnostician/vision] %s — falling back to rule-based", llmErr)
		return a.Base.runFallback(ctx, msg, env)
	}

	state.WorkingDiagnosis = proposal.Diagnosis
	env.Logf("[diagnostician/vision] provider=%s model=%s diagnosis=%q confidence=%.0f%% image=%s",
		a.Base.Client.Provider(), a.Base.Client.Model(), proposal.Diagnosis, proposal.Confidence*100, img.MediaType)
	env.Logf("[diagnostician/vision] rationale: %s", proposal.Rationale)

	content, _ := json.Marshal(proposal)
	stateContent := medical.EncodeState(state)
	verify := agent.NewMessage(
		a.ID(),
		"reasoning_verifier",
		agent.RoleAgent,
		"diagnosis_proposal",
		string(content),
		mergeStateMeta(msg.Metadata, stateContent),
	)
	return []agent.Message{verify}, nil
}

func (a *LLMDiagnosticianVision) callLLMWithImage(ctx context.Context, state medical.CaseState, img llm.Image, env agent.Environment) (medical.DiagnosisProposal, error) {
	if a.Base.Client == nil {
		return medical.DiagnosisProposal{}, errors.New("llm client not configured")
	}
	timeout := a.Base.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	minConfidence := a.Base.MinConfidence
	if minConfidence == 0 {
		minConfidence = 0.3
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, retrieved, _ := a.Base.buildRequest(cctx, state, env)
	// Mention the image in the system prompt so the model knows to inspect it.
	req.System = req.System + "\n\nA medical image is attached to the user message. Inspect it for findings (e.g. infiltrate, cardiomegaly, ST changes) and weave its features into your rationale."
	// Attach the image to the (single) user message.
	if len(req.Messages) > 0 {
		req.Messages[0].Images = append(req.Messages[0].Images, img)
	}
	return a.Base.completeAndDecode(cctx, req, retrieved, minConfidence, env)
}

// readImage loads the file at path and returns an llm.Image with a sniffed
// MIME type. We restrict to PNG and JPEG — what the providers reliably support.
func readImage(path string) (llm.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return llm.Image{}, fmt.Errorf("read %q: %w", path, err)
	}
	mt := sniffMedia(path, data)
	if mt == "" {
		return llm.Image{}, fmt.Errorf("unsupported image format for %q", path)
	}
	return llm.Image{MediaType: mt, Data: data}, nil
}

// sniffMedia detects PNG/JPEG from the file's magic bytes, falling back to
// the path's extension. Returns "" when neither matches.
func sniffMedia(path string, data []byte) string {
	switch http.DetectContentType(data) {
	case "image/png":
		return "image/png"
	case "image/jpeg":
		return "image/jpeg"
	}
	switch strings.ToLower(extOf(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	}
	return ""
}

// extOf returns the lowercased extension of path including the leading dot,
// or "" if there is none.
func extOf(path string) string {
	i := strings.LastIndex(path, ".")
	if i < 0 {
		return ""
	}
	return path[i:]
}

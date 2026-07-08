// Command export-hitl-feedback turns the HITL review queue into fine-tuning
// training data. Clinicians decide on LLM-generated proposals via cmd/care's
// HITL gate; every decided review is a labelled example we can replay into a
// fine-tune. This binary reads a JSON dump of []medical.ReviewRequest and
// emits two formats side-by-side:
//
//   - SFT (supervised fine-tuning) JSONL — one approved (or modified)
//     example per line, in chat-messages shape (system / user / assistant).
//   - DPO (preference) JSONL — one {prompt, chosen, rejected} per line,
//     constructed either from an approved_with_modifications review (original
//     proposal = rejected; modification = chosen) or from an approved
//     paired with a rejected for the same case_id.
//
// Usage:
//
//	export-hitl-feedback -in queue.json -format both -out-dir ./fine-tune-data
//
// Flags:
//
//	-in              path to JSON queue dump ([]medical.ReviewRequest).
//	                 Defaults to $HOME/.bodh/hitl-queue.json when unset.
//	-format          one of "sft" | "dpo" | "both" (default "both").
//	-out-dir         directory to write sft.jsonl / dpo.jsonl (default "./fine-tune-data").
//	-min-reviews     warn (not fail) when fewer than N decided reviews are found.
//	-include-rejected
//	                 also emit rejected reviews as SFT hard negatives (with empty
//	                 assistant content). Default true.
//
// Exit codes: 0 on success, 2 on bad flag input, 3 on file-not-found.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

// DiagnosticianSystemPrompt mirrors the production LLM diagnostician's
// system prompt (agents/diagnostician_llm.go: diagnosticianSystemPrompt).
// Kept in sync by hand — see docs/ai-features.md ("Fine-tuning from HITL
// decisions"). A fine-tuned model trained on these SFT examples should behave
// like the LLM diagnostician under the same prompt.
const DiagnosticianSystemPrompt = `You are the Diagnostician agent in a virtual physician panel modelled on Microsoft's MAI-DxO sequential diagnosis architecture.

Your role:
  - Read the structured CaseState (presentation, vitals, history answers, ordered tests with results, hypotheses, RPM observations).
  - Propose the single most likely working diagnosis.
  - Provide a 1-3 sentence clinical rationale grounded only in evidence already in the CaseState — do not invent labs, vitals, or history that is not present.
  - Provide a confidence score between 0.0 and 1.0.
  - List up to five differential diagnoses, ordered by likelihood.

Constraints:
  - This is a research / educational system. Do not provide treatment plans.
  - If the evidence is insufficient for a confident diagnosis, return a working label such as "Undifferentiated <complaint> — further workup needed" with confidence below 0.5.
  - Return ONLY JSON matching the supplied tool schema. No prose outside the schema fields.`

// config is the parsed CLI configuration. Held in a struct so tests can build
// one directly without round-tripping through flag parsing.
type config struct {
	in              string
	format          string
	outDir          string
	minReviews      int
	includeRejected bool
}

// chatMessage matches the OpenAI / Anthropic SFT format: a list of role/content
// turns. The output is messages-shaped JSONL, which is what Together, OpenAI,
// and the HuggingFace `trl` SFTTrainer all consume.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type sftLine struct {
	Messages []chatMessage  `json:"messages"`
	Meta     map[string]any `json:"meta,omitempty"`
}

type dpoPrompt struct {
	System string `json:"system"`
	User   string `json:"user"`
}

type dpoLine struct {
	Prompt   dpoPrompt      `json:"prompt"`
	Chosen   string         `json:"chosen"`
	Rejected string         `json:"rejected"`
	Meta     map[string]any `json:"meta,omitempty"`
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if code, err := run(cfg, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(code)
	}
}

// parseFlags is split out so tests can construct a config without invoking
// flag.CommandLine (which would call os.Exit on -h).
func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("export-hitl-feedback", flag.ContinueOnError)
	in := fs.String("in", "", "path to HITL queue JSON dump ([]medical.ReviewRequest)")
	format := fs.String("format", "both", "output format: sft, dpo, or both")
	outDir := fs.String("out-dir", "./fine-tune-data", "directory to write JSONL files")
	minReviews := fs.Int("min-reviews", 1, "warn when fewer than N decided reviews are found")
	includeRejected := fs.Bool("include-rejected", true, "include rejected reviews as SFT hard negatives")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	switch *format {
	case "sft", "dpo", "both":
	default:
		return config{}, fmt.Errorf("export-hitl-feedback: invalid -format %q (want sft|dpo|both)", *format)
	}
	if *minReviews < 0 {
		return config{}, fmt.Errorf("export-hitl-feedback: -min-reviews must be >= 0")
	}
	return config{
		in:              *in,
		format:          *format,
		outDir:          *outDir,
		minReviews:      *minReviews,
		includeRejected: *includeRejected,
	}, nil
}

// resolveInputPath returns the configured input path, falling back to
// $HOME/.bodh/hitl-queue.json. Returns ("", err) when neither exists so the
// caller can map that to exit code 3.
func resolveInputPath(in string) (string, error) {
	if in != "" {
		if _, err := os.Stat(in); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("export-hitl-feedback: input file not found: %s", in)
			}
			return "", err
		}
		return in, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("export-hitl-feedback: no -in flag and could not resolve $HOME: %w", err)
	}
	candidate := filepath.Join(home, ".bodh", "hitl-queue.json")
	if _, err := os.Stat(candidate); err != nil {
		return "", fmt.Errorf("export-hitl-feedback: no -in flag and default %s not found", candidate)
	}
	return candidate, nil
}

// run is the testable entry point. It returns (exitCode, err); exitCode is
// meaningful only when err != nil.
func run(cfg config, stdout, stderr io.Writer) (int, error) {
	path, err := resolveInputPath(cfg.in)
	if err != nil {
		return 3, err
	}

	reviews, err := readReviews(path)
	if err != nil {
		return 1, err
	}

	if err := os.MkdirAll(cfg.outDir, 0o755); err != nil {
		return 1, fmt.Errorf("export-hitl-feedback: mkdir %s: %w", cfg.outDir, err)
	}

	decided, skipped := filterDecided(reviews)

	if len(decided) < cfg.minReviews {
		fmt.Fprintf(stderr, "warning: only %d decided reviews found (-min-reviews=%d)\n", len(decided), cfg.minReviews)
	}

	if len(decided) == 0 {
		// Still write empty files so downstream pipelines have stable paths.
		if cfg.format == "sft" || cfg.format == "both" {
			if err := writeJSONL(filepath.Join(cfg.outDir, "sft.jsonl"), nil); err != nil {
				return 1, err
			}
		}
		if cfg.format == "dpo" || cfg.format == "both" {
			if err := writeJSONL(filepath.Join(cfg.outDir, "dpo.jsonl"), nil); err != nil {
				return 1, err
			}
		}
		fmt.Fprintln(stdout, "no decisions found — wrote empty output files")
		return 0, nil
	}

	var sftCount, dpoCount int

	if cfg.format == "sft" || cfg.format == "both" {
		lines := buildSFT(decided, cfg.includeRejected)
		if err := writeJSONL(filepath.Join(cfg.outDir, "sft.jsonl"), toAnySlice(lines)); err != nil {
			return 1, err
		}
		sftCount = len(lines)
	}

	if cfg.format == "dpo" || cfg.format == "both" {
		lines := buildDPO(decided)
		if err := writeJSONL(filepath.Join(cfg.outDir, "dpo.jsonl"), toAnySlice(lines)); err != nil {
			return 1, err
		}
		dpoCount = len(lines)
	}

	fmt.Fprintf(stdout, "Exported %d SFT lines, %d DPO pairs, %d reviews skipped (no decision).\n",
		sftCount, dpoCount, skipped)
	return 0, nil
}

func readReviews(path string) ([]medical.ReviewRequest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("export-hitl-feedback: read %s: %w", path, err)
	}
	if len(b) == 0 {
		return nil, nil
	}
	// Accept either a bare array or the persistence wrapper {"reviews": [...]}.
	trimmed := strings.TrimSpace(string(b))
	if strings.HasPrefix(trimmed, "[") {
		var out []medical.ReviewRequest
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, fmt.Errorf("export-hitl-feedback: parse JSON array: %w", err)
		}
		return out, nil
	}
	var wrap struct {
		Reviews []medical.ReviewRequest `json:"reviews"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		return nil, fmt.Errorf("export-hitl-feedback: parse JSON object: %w", err)
	}
	return wrap.Reviews, nil
}

// filterDecided splits reviews into (decided, skipped-count). Pending and
// expired reviews are skipped — they carry no labelled signal.
func filterDecided(in []medical.ReviewRequest) ([]medical.ReviewRequest, int) {
	out := make([]medical.ReviewRequest, 0, len(in))
	skipped := 0
	for _, r := range in {
		switch r.Status {
		case medical.ReviewApproved, medical.ReviewModified, medical.ReviewRejected:
			out = append(out, r)
		default:
			skipped++
		}
	}
	return out, skipped
}

// buildSFT renders approved + approved_with_modifications reviews as
// supervised-fine-tuning examples. When includeRejected is true, each rejected
// review is added with an empty assistant content as a hard negative.
func buildSFT(decided []medical.ReviewRequest, includeRejected bool) []sftLine {
	out := make([]sftLine, 0, len(decided))
	for _, r := range decided {
		switch r.Status {
		case medical.ReviewApproved:
			out = append(out, makeSFT(r, r.Payload))
		case medical.ReviewModified:
			content := r.Modification
			if strings.TrimSpace(content) == "" {
				// No explicit modification text — treat as approval of payload.
				content = r.Payload
			}
			out = append(out, makeSFT(r, content))
		case medical.ReviewRejected:
			if !includeRejected {
				continue
			}
			out = append(out, makeSFT(r, ""))
		}
	}
	return out
}

func makeSFT(r medical.ReviewRequest, assistant string) sftLine {
	return sftLine{
		Messages: []chatMessage{
			{Role: "system", Content: DiagnosticianSystemPrompt},
			{Role: "user", Content: r.Payload /* placeholder; overwritten below */},
			{Role: "assistant", Content: assistant},
		},
		Meta: map[string]any{
			"review_id":   r.ID,
			"kind":        r.Kind,
			"case_id":     r.CaseID,
			"patient_id":  r.PatientID,
			"status":      string(r.Status),
			"decided_at":  r.DecidedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			"reviewer_id": r.ReviewerID,
		},
	}.withUser(r)
}

// withUser populates the user-turn content with the case-state JSON drawn from
// ReviewRequest.Meta["case_state"] when present, else falls back to the raw
// payload. Encapsulated as a method so makeSFT stays readable.
func (s sftLine) withUser(r medical.ReviewRequest) sftLine {
	user := caseStateContent(r)
	for i := range s.Messages {
		if s.Messages[i].Role == "user" {
			s.Messages[i].Content = user
		}
	}
	return s
}

// caseStateContent extracts the user-turn content for the SFT/DPO prompt.
// Preference order:
//  1. r.Meta["case_state"] — what the upstream agent passed to the LLM
//  2. r.Detail — falls back to the human-readable summary
//  3. r.Payload — last resort
func caseStateContent(r medical.ReviewRequest) string {
	if r.Meta != nil {
		if cs, ok := r.Meta["case_state"]; ok {
			switch v := cs.(type) {
			case string:
				return v
			default:
				if b, err := json.Marshal(v); err == nil {
					return string(b)
				}
			}
		}
	}
	if strings.TrimSpace(r.Detail) != "" {
		return r.Detail
	}
	return r.Payload
}

// buildDPO emits one {chosen, rejected} pair per decided review where a pair
// can be reasonably constructed:
//
//   - approved_with_modifications → chosen = modification, rejected = original payload
//   - approved + rejected for the same case_id → chosen = approved payload,
//     rejected = rejected payload
//
// Lone approvals or rejections (no counterpart) produce no DPO line.
func buildDPO(decided []medical.ReviewRequest) []dpoLine {
	out := make([]dpoLine, 0)

	// Modifications are self-contained pairs.
	for _, r := range decided {
		if r.Status != medical.ReviewModified {
			continue
		}
		modified := strings.TrimSpace(r.Modification)
		original := strings.TrimSpace(r.Payload)
		if modified == "" || original == "" || modified == original {
			continue
		}
		out = append(out, dpoLine{
			Prompt:   dpoPrompt{System: DiagnosticianSystemPrompt, User: caseStateContent(r)},
			Chosen:   modified,
			Rejected: original,
			Meta: map[string]any{
				"source":     "approved_with_modifications",
				"review_id":  r.ID,
				"case_id":    r.CaseID,
				"decided_at": r.DecidedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			},
		})
	}

	// Pair approved with rejected by case_id (skip pairs without a case_id —
	// they cannot be reliably matched).
	approvedByCase := map[string][]medical.ReviewRequest{}
	rejectedByCase := map[string][]medical.ReviewRequest{}
	for _, r := range decided {
		if r.CaseID == "" {
			continue
		}
		switch r.Status {
		case medical.ReviewApproved:
			approvedByCase[r.CaseID] = append(approvedByCase[r.CaseID], r)
		case medical.ReviewRejected:
			rejectedByCase[r.CaseID] = append(rejectedByCase[r.CaseID], r)
		}
	}
	for caseID, approvals := range approvedByCase {
		rejections, ok := rejectedByCase[caseID]
		if !ok {
			continue
		}
		for _, a := range approvals {
			for _, rej := range rejections {
				out = append(out, dpoLine{
					Prompt:   dpoPrompt{System: DiagnosticianSystemPrompt, User: caseStateContent(a)},
					Chosen:   a.Payload,
					Rejected: rej.Payload,
					Meta: map[string]any{
						"source":           "approved_vs_rejected",
						"case_id":          caseID,
						"approved_review":  a.ID,
						"rejected_review":  rej.ID,
						"approved_decided": a.DecidedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
						"rejected_decided": rej.DecidedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
					},
				})
			}
		}
	}

	return out
}

// writeJSONL writes one JSON object per line. lines may be nil → empty file.
func writeJSONL(path string, lines []any) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("export-hitl-feedback: create %s: %w", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, l := range lines {
		if err := enc.Encode(l); err != nil {
			return fmt.Errorf("export-hitl-feedback: encode line: %w", err)
		}
	}
	return nil
}

// toAnySlice is a tiny shim so writeJSONL can stay generic without Go 1.18+
// generics noise at every call site.
func toAnySlice[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

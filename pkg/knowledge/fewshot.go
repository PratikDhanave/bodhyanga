package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

// LoadFewShotCorpus reads a JSON dump of HITL review records and returns one
// Document per clinician-approved decision. The on-disk shape is the same one
// accepted by cmd/export-hitl-feedback: either a bare JSON array of
// medical.ReviewRequest or the persistence wrapper {"reviews": [...]}.
//
// Only reviews with Status == "approved" or "approved_with_modifications" are
// retained — rejected and pending records carry no positive label and would
// pollute the in-context examples. Records that resolve to an empty case-state
// summary or empty approved diagnosis are skipped silently.
//
// The returned Documents are ready to be Index'd into any Store. Callers
// typically wire this through LoadDefaultFewShotStore in the agents package.
func LoadFewShotCorpus(path string) ([]Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("knowledge: read fewshot %q: %w", path, err)
	}
	if len(b) == 0 {
		return nil, nil
	}
	reviews, err := parseHITLDump(b)
	if err != nil {
		return nil, fmt.Errorf("knowledge: parse fewshot %q: %w", path, err)
	}

	out := make([]Document, 0, len(reviews))
	for _, r := range reviews {
		doc, ok := documentFromReview(r)
		if !ok {
			continue
		}
		out = append(out, doc)
	}
	return out, nil
}

// parseHITLDump accepts either a bare JSON array or the {"reviews": [...]}
// wrapper, mirroring readReviews in cmd/export-hitl-feedback so a single file
// drives both fine-tune export and in-context retrieval.
func parseHITLDump(b []byte) ([]medical.ReviewRequest, error) {
	trimmed := strings.TrimSpace(string(b))
	if strings.HasPrefix(trimmed, "[") {
		var arr []medical.ReviewRequest
		if err := json.Unmarshal(b, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var wrap struct {
		Reviews []medical.ReviewRequest `json:"reviews"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		return nil, err
	}
	return wrap.Reviews, nil
}

// documentFromReview turns one approved (or approved-with-modifications)
// ReviewRequest into a few-shot Document. Returns (zero, false) when the
// review's status disqualifies it or when the resulting Content would be
// empty.
func documentFromReview(r medical.ReviewRequest) (Document, bool) {
	var diagnosis string
	switch r.Status {
	case medical.ReviewApproved:
		diagnosis = strings.TrimSpace(r.Payload)
	case medical.ReviewModified:
		diagnosis = strings.TrimSpace(r.Modification)
		if diagnosis == "" {
			diagnosis = strings.TrimSpace(r.Payload)
		}
	default:
		return Document{}, false
	}
	if diagnosis == "" {
		return Document{}, false
	}

	caseSummary := summariseCaseState(r)
	if caseSummary == "" {
		return Document{}, false
	}

	source := "hitl-decision"
	if r.ReviewerID != "" {
		source = source + "/" + r.ReviewerID
	}
	if !r.DecidedAt.IsZero() {
		source = source + "/" + r.DecidedAt.Format(time.RFC3339)
	}

	title := r.Subject
	if title == "" {
		title = "HITL decision " + r.ID
	}

	return Document{
		ID:      "fewshot-" + r.ID,
		Source:  source,
		Title:   title,
		Content: "Case: " + caseSummary + "\n\nApproved diagnosis: " + diagnosis,
		Metadata: map[string]string{
			"hitl":        "true",
			"decision":    string(r.Status),
			"reviewer_id": r.ReviewerID,
		},
	}, true
}

// summariseCaseState produces the 2-3 line case summary that opens each
// few-shot Document. Preference order for the source material:
//  1. r.Meta["case_state"] as either a JSON-string or a nested object — this
//     is what the diagnostician originally fed the LLM.
//  2. r.Detail — the clinician-facing summary.
//  3. r.Payload — last resort.
//
// Parsing as CaseState is best-effort: when the source is a plain string
// (e.g. r.Detail), we return it as-is so callers still get something
// retrievable.
func summariseCaseState(r medical.ReviewRequest) string {
	raw := extractCaseStateJSON(r)
	if raw == "" {
		return strings.TrimSpace(r.Detail)
	}

	var state medical.CaseState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		// Not parsable as CaseState — fall back to the raw text trimmed to a
		// single line so retrieval still works.
		return strings.TrimSpace(raw)
	}

	parts := make([]string, 0, 3)
	if state.Presentation != "" {
		parts = append(parts, "Presentation: "+state.Presentation)
	}
	if state.Condition != "" {
		parts = append(parts, "Condition: "+state.Condition)
	}
	if n := len(state.TestResults); n > 0 {
		parts = append(parts, fmt.Sprintf("Test results: %d", n))
	}
	if len(parts) == 0 {
		return strings.TrimSpace(raw)
	}
	return strings.Join(parts, "\n")
}

// extractCaseStateJSON returns the case-state JSON blob to summarise. Meta
// may carry it as a JSON-encoded string (the most common path; see how
// cmd/care marshals the state before submission) or as a nested object.
func extractCaseStateJSON(r medical.ReviewRequest) string {
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
	if strings.TrimSpace(r.Payload) != "" {
		return r.Payload
	}
	return ""
}

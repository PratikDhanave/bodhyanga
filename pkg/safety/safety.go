package safety

import (
	"regexp"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/knowledge"
)

// Verdict is the outcome of a safety check. Reason explains why the check
// failed (empty when OK).
type Verdict struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// refusalPatterns are lowercased substrings that strongly indicate the model
// declined to answer. Tune for false-positive rate.
var refusalPatterns = []string{
	"i cannot provide",
	"i can't provide",
	"i'm unable to",
	"i am not able to",
	"i cannot diagnose",
	"i can't diagnose",
	"as an ai language model",
	"i'm just an ai",
	"please consult a healthcare professional",
	"i'm not qualified",
}

// IsRefusal returns true when the text matches a known refusal pattern.
// The check is case-insensitive substring match.
func IsRefusal(text string) bool {
	lower := strings.ToLower(text)
	for _, p := range refusalPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// CheckHallucination performs a heuristic grounding check: it splits the
// claim into significant terms (length ≥ 4, alphanumeric, not stopwords) and
// asserts that some fraction of them appear somewhere in the supplied
// citations. The default threshold is 0.5 — at least half the significant
// terms must be supported.
//
// Returns Verdict{OK:true} when grounded; OK:false with a reason otherwise.
func CheckHallucination(claim string, citations []knowledge.Citation) Verdict {
	return CheckHallucinationThreshold(claim, citations, 0.5)
}

// CheckHallucinationThreshold is CheckHallucination with a configurable
// minimum-grounding fraction (0.0–1.0).
func CheckHallucinationThreshold(claim string, citations []knowledge.Citation, threshold float64) Verdict {
	terms := significantTerms(claim)
	if len(terms) == 0 {
		return Verdict{OK: true}
	}
	if len(citations) == 0 {
		return Verdict{OK: false, Reason: "no citations provided to ground the claim"}
	}
	corpus := ""
	for _, c := range citations {
		corpus += " " + strings.ToLower(c.Snippet+" "+c.Title)
	}
	hit := 0
	for _, t := range terms {
		if strings.Contains(corpus, t) {
			hit++
		}
	}
	frac := float64(hit) / float64(len(terms))
	if frac < threshold {
		return Verdict{
			OK:     false,
			Reason: "low grounding fraction " + formatFloat(frac) + " (< " + formatFloat(threshold) + ")",
		}
	}
	return Verdict{OK: true}
}

// stopwords is a small set of high-frequency English terms excluded from
// grounding checks to reduce false negatives on rare clinical terms.
var stopwords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "this": {}, "that": {},
	"from": {}, "have": {}, "been": {}, "were": {}, "their": {}, "they": {},
	"would": {}, "could": {}, "should": {}, "patient": {}, "patients": {},
	"diagnosis": {}, "treatment": {}, "clinical": {}, "based": {}, "evidence": {},
}

func significantTerms(text string) []string {
	lower := strings.ToLower(text)
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, w := range strings.FieldsFunc(lower, func(r rune) bool {
		return !(r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if len(w) < 4 {
			continue
		}
		if _, skip := stopwords[w]; skip {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	return out
}

func formatFloat(f float64) string {
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return strings.TrimRight(strings.TrimRight(stringFromFloat(f), "0"), ".")
}

func stringFromFloat(f float64) string {
	// Three decimal places without depending on fmt for hot-path code.
	whole := int(f * 1000)
	major := whole / 1000
	minor := whole % 1000
	if minor < 0 {
		minor = -minor
	}
	out := itoa(major) + "."
	if minor < 10 {
		out += "00"
	} else if minor < 100 {
		out += "0"
	}
	out += itoa(minor)
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	out := ""
	for i > 0 {
		out = string(rune('0'+i%10)) + out
		i /= 10
	}
	if neg {
		out = "-" + out
	}
	return out
}

// phiPatterns are regexes for shapes that look like PHI in model output.
// These are intentionally conservative; pkg/phi.Redact is the primary defense
// applied to inputs before they reach an LLM.
var phiPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),           // SSN
	regexp.MustCompile(`\b\d{3}[-.\s]\d{3}[-.\s]\d{4}\b`), // US phone
	regexp.MustCompile(`\bMRN[:\s]*\d{6,10}\b`),           // MRN
}

// ContainsPHIPatterns returns Verdict{OK:false} if any PHI-shaped substring
// is found in the text. Reason names the first matching pattern.
func ContainsPHIPatterns(text string) Verdict {
	for i, p := range phiPatterns {
		if p.MatchString(text) {
			return Verdict{OK: false, Reason: "matched phi pattern " + itoa(i)}
		}
	}
	return Verdict{OK: true}
}

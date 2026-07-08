package medical

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ScribeInput is the input to the ambient clinical scribe pipeline.
type ScribeInput struct {
	Transcript string
	PatientID  string
	CaseID     string
}

// ScribeOutput is the structured extraction result from a clinical transcript.
type ScribeOutput struct {
	ChiefComplaint string          `json:"chief_complaint"`
	HPI            string          `json:"hpi"`
	Vitals         ExtractedVitals `json:"vitals"`
	Demographics   string          `json:"demographics"`
	Medications    []string        `json:"medications"`
	Allergies      []string        `json:"allergies"`
	Confidence     float64         `json:"confidence"`
	Method         string          `json:"method"` // "rule" or "llm"
}

// ExtractedVitals holds structured vital signs extracted from free text.
type ExtractedVitals struct {
	HR   int     `json:"hr,omitempty"`
	BP   string  `json:"bp,omitempty"`
	Temp float64 `json:"temp,omitempty"`
	RR   int     `json:"rr,omitempty"`
	SpO2 float64 `json:"spo2,omitempty"`
}

// ---------------------------------------------------------------------------
// Regex patterns — compiled once at init
// ---------------------------------------------------------------------------

var (
	// Blood pressure: "blood pressure 120 over 80", "BP 120/80", "bp is 130/85"
	reBP = regexp.MustCompile(`(?i)(?:blood\s+pressure|bp)\s+(?:is\s+)?(\d{2,3})\s*(?:over|/)\s*(\d{2,3})`)

	// Heart rate: "heart rate 88", "HR 88", "pulse 72"
	reHR = regexp.MustCompile(`(?i)(?:heart\s+rate|hr|pulse)\s+(?:is\s+|of\s+)?(\d{2,3})`)

	// Temperature: "temperature 38.5", "temp 101.2", "temperature of 99.1"
	reTemp = regexp.MustCompile(`(?i)(?:temperature|temp)\s+(?:is\s+|of\s+)?(\d{2,3}(?:\.\d+)?)`)

	// Respiratory rate: "respiratory rate 18", "RR 18", "resp rate 20"
	reRR = regexp.MustCompile(`(?i)(?:respiratory\s+rate|resp\s+rate|rr)\s+(?:is\s+|of\s+)?(\d{1,3})`)

	// SpO2: "oxygen sat 95", "SpO2 95%", "pulse ox 92", "O2 sat 97", "sat 96%"
	reSpO2 = regexp.MustCompile(`(?i)(?:oxygen\s+sat|spo2|pulse\s+ox|o2\s+sat|sat)\s+(?:is\s+|of\s+)?(\d{2,3})\s*%?`)
)

// knownMedications is the lookup set for medication extraction. All lower-case.
var knownMedications = map[string]bool{
	"metformin":           true,
	"lisinopril":          true,
	"atorvastatin":        true,
	"aspirin":             true,
	"metoprolol":          true,
	"amlodipine":          true,
	"omeprazole":          true,
	"albuterol":           true,
	"sertraline":          true,
	"levothyroxine":       true,
	"warfarin":            true,
	"insulin":             true,
	"losartan":            true,
	"hydrochlorothiazide": true,
	"gabapentin":          true,
	"prednisone":          true,
	"acetaminophen":       true,
	"ibuprofen":           true,
	"furosemide":          true,
	"pantoprazole":        true,
	"clopidogrel":         true,
	"amoxicillin":         true,
	"azithromycin":        true,
	"ciprofloxacin":       true,
	"doxycycline":         true,
	"hydrocodone":         true,
	"tramadol":            true,
	"montelukast":         true,
	"fluticasone":         true,
	"rosuvastatin":        true,
	"simvastatin":         true,
	"carvedilol":          true,
	"spironolactone":      true,
	"cephalexin":          true,
	"duloxetine":          true,
	"escitalopram":        true,
	"fluoxetine":          true,
	"trazodone":           true,
	"alprazolam":          true,
	"lorazepam":           true,
	"diazepam":            true,
	"clonazepam":          true,
	"methylphenidate":     true,
	"amphetamine":         true,
	"bupropion":           true,
	"venlafaxine":         true,
	"lithium":             true,
	"valproic":            true,
	"lamotrigine":         true,
	"sumatriptan":         true,
}

// reAllergyPhrase matches "allergic to X", "allergy to X", "allergies to X".
var reAllergyPhrase = regexp.MustCompile(`(?i)(?:allergic|allergy|allergies)\s+to\s+([^,.\n]+)`)

// reNKDA matches "NKDA" or "no known drug allergies".
var reNKDA = regexp.MustCompile(`(?i)\b(?:nkda|no\s+known\s+drug\s+allergies)\b`)

// ---------------------------------------------------------------------------
// Rule-based extractors
// ---------------------------------------------------------------------------

// ExtractVitalsFromText uses regex patterns to pull structured vital signs
// from free-form clinical text.
func ExtractVitalsFromText(text string) ExtractedVitals {
	var v ExtractedVitals

	if m := reBP.FindStringSubmatch(text); len(m) == 3 {
		v.BP = m[1] + "/" + m[2]
	}

	if m := reHR.FindStringSubmatch(text); len(m) == 2 {
		var hr int
		fmt.Sscanf(m[1], "%d", &hr)
		if hr > 0 && hr < 300 {
			v.HR = hr
		}
	}

	if m := reTemp.FindStringSubmatch(text); len(m) == 2 {
		var temp float64
		fmt.Sscanf(m[1], "%f", &temp)
		if temp > 0 {
			// If temperature looks like Fahrenheit (above 50), convert to Celsius.
			if temp > 50 {
				temp = fahrenheitToCelsius(temp)
			}
			// Round to one decimal place.
			v.Temp = math.Round(temp*10) / 10
		}
	}

	if m := reRR.FindStringSubmatch(text); len(m) == 2 {
		var rr int
		fmt.Sscanf(m[1], "%d", &rr)
		if rr > 0 && rr < 80 {
			v.RR = rr
		}
	}

	if m := reSpO2.FindStringSubmatch(text); len(m) == 2 {
		var spo2 float64
		fmt.Sscanf(m[1], "%f", &spo2)
		if spo2 > 0 && spo2 <= 100 {
			v.SpO2 = spo2
		}
	}

	return v
}

// fahrenheitToCelsius converts Fahrenheit to Celsius.
func fahrenheitToCelsius(f float64) float64 {
	return (f - 32) * 5.0 / 9.0
}

// ExtractMedicationsFromText scans the transcript for known medication names
// and returns a deduplicated list of matches.
func ExtractMedicationsFromText(text string) []string {
	lower := strings.ToLower(text)
	// Use a word-boundary tokeniser: split on non-alpha characters.
	words := regexp.MustCompile(`[a-z]+`).FindAllString(lower, -1)

	seen := make(map[string]bool)
	var meds []string
	for _, w := range words {
		if knownMedications[w] && !seen[w] {
			seen[w] = true
			meds = append(meds, w)
		}
	}
	return meds
}

// ExtractAllergiesFromText detects allergy mentions and NKDA (no known drug
// allergies) from clinical text.
func ExtractAllergiesFromText(text string) []string {
	// Check for NKDA first.
	if reNKDA.MatchString(text) {
		return []string{"NKDA"}
	}

	matches := reAllergyPhrase.FindAllStringSubmatch(text, -1)
	var allergies []string
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) >= 2 {
			allergen := strings.TrimSpace(m[1])
			allergen = strings.ToLower(allergen)
			if allergen != "" && !seen[allergen] {
				seen[allergen] = true
				allergies = append(allergies, allergen)
			}
		}
	}
	return allergies
}

// ---------------------------------------------------------------------------
// Rule-based scribe
// ---------------------------------------------------------------------------

// RuleBasedScribe extracts structured clinical data from a transcript using
// only regex and pattern matching. No LLM call is made. The chief complaint
// is taken from the first sentence; the HPI is everything that follows.
func RuleBasedScribe(input ScribeInput) ScribeOutput {
	text := strings.TrimSpace(input.Transcript)
	if text == "" {
		return ScribeOutput{Method: "rule", Confidence: 0}
	}

	cc, hpi := splitChiefComplaintHPI(text)

	vitals := ExtractVitalsFromText(text)
	meds := ExtractMedicationsFromText(text)
	allergies := ExtractAllergiesFromText(text)

	// Confidence heuristic: start at 0.5, add 0.1 for each non-empty field.
	conf := 0.5
	if vitals.BP != "" || vitals.HR > 0 || vitals.Temp > 0 || vitals.RR > 0 || vitals.SpO2 > 0 {
		conf += 0.1
	}
	if len(meds) > 0 {
		conf += 0.1
	}
	if len(allergies) > 0 {
		conf += 0.1
	}
	if cc != "" {
		conf += 0.1
	}
	if conf > 1.0 {
		conf = 1.0
	}

	return ScribeOutput{
		ChiefComplaint: cc,
		HPI:            hpi,
		Vitals:         vitals,
		Medications:    meds,
		Allergies:      allergies,
		Confidence:     conf,
		Method:         "rule",
	}
}

// splitChiefComplaintHPI splits text at the first sentence boundary. The
// first sentence becomes the chief complaint; the remainder is the HPI.
func splitChiefComplaintHPI(text string) (string, string) {
	// Look for first sentence-ending punctuation followed by whitespace.
	reSentenceEnd := regexp.MustCompile(`[.!?]\s+`)
	loc := reSentenceEnd.FindStringIndex(text)
	if loc == nil {
		// No sentence break found — the whole text is the chief complaint.
		return text, ""
	}
	cc := strings.TrimSpace(text[:loc[0]+1]) // include the period
	hpi := strings.TrimSpace(text[loc[0]+1:])
	return cc, hpi
}

// ---------------------------------------------------------------------------
// LLM-based scribe
// ---------------------------------------------------------------------------

// scribeLLMSystemPrompt is the system prompt sent to the LLM for structured
// extraction. It asks the model to return a JSON object matching ScribeOutput.
const scribeLLMSystemPrompt = `You are a clinical NLP extraction engine. Given a clinician-dictated transcript, extract structured data and return ONLY a JSON object with these fields:
{
  "chief_complaint": "string — the patient's primary reason for visit, one sentence",
  "hpi": "string — history of present illness narrative",
  "vitals": {
    "hr": 0,
    "bp": "120/80",
    "temp": 0.0,
    "rr": 0,
    "spo2": 0.0
  },
  "demographics": "string — age, sex, relevant background if mentioned",
  "medications": ["list of medication names mentioned"],
  "allergies": ["list of allergies mentioned, or NKDA"],
  "confidence": 0.0,
  "method": "llm"
}

Rules:
- Temperature must be in Celsius. Convert Fahrenheit to Celsius if needed.
- BP format: "systolic/diastolic" as a string.
- Omit fields with zero/empty values (use Go JSON omitempty semantics).
- Set confidence between 0.0 and 1.0 based on how complete the transcript is.
- Return ONLY the JSON object, no markdown fences, no explanation.`

// LLMScribe sends the transcript to an LLM for structured extraction. On
// LLM failure it falls back to RuleBasedScribe.
func LLMScribe(ctx context.Context, client llm.Client, input ScribeInput) (ScribeOutput, error) {
	if client == nil {
		return RuleBasedScribe(input), nil
	}

	req := llm.Request{
		System: scribeLLMSystemPrompt,
		Messages: []llm.Message{
			{
				Role:    llm.RoleUser,
				Content: fmt.Sprintf("Transcript:\n\n%s", input.Transcript),
			},
		},
		MaxTokens:   2048,
		Temperature: 0.1,
		CacheSystem: true,
	}

	resp, err := client.Complete(ctx, req)
	if err != nil {
		// LLM call failed — fall back to rule-based extraction.
		fallback := RuleBasedScribe(input)
		fallback.Method = "rule"
		return fallback, nil
	}

	var out ScribeOutput
	content := strings.TrimSpace(resp.Content)

	// Strip markdown code fences if the model wrapped the JSON.
	content = stripCodeFences(content)

	if err := json.Unmarshal([]byte(content), &out); err != nil {
		// JSON parse failed — fall back to rule-based extraction.
		fallback := RuleBasedScribe(input)
		fallback.Method = "rule"
		return fallback, nil
	}

	out.Method = "llm"
	return out, nil
}

// stripCodeFences removes optional ```json ... ``` wrapping from LLM output.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Remove first line (```json or ```)
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
	}
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// ---------------------------------------------------------------------------
// AutoScribe — main entry point
// ---------------------------------------------------------------------------

// AutoScribe is the primary entry point for the ambient clinical scribe. If
// an LLM client is provided it attempts LLM-based extraction with rule-based
// fallback. If client is nil it uses rule-based extraction only.
func AutoScribe(ctx context.Context, client llm.Client, input ScribeInput) (ScribeOutput, error) {
	if client == nil {
		return RuleBasedScribe(input), nil
	}
	return LLMScribe(ctx, client, input)
}

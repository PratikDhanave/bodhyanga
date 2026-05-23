package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// DrugInteraction is a Tool that checks a list of drug names against a small
// hard-coded interaction table and returns the pairs that are known to
// collide. The table is illustrative only — twelve canonical pairs from
// common clinical settings — and is NOT a substitute for a curated drug
// interaction database (Lexicomp, Micromedex, etc.). Outputs are tagged
// metadata.source = "demo-synth" so downstream consumers know the data is
// synthetic.
//
// Input shape (JSON):
//
//	{"drugs":["lisinopril","spironolactone"]}
//
// Output shape (JSON):
//
//	{
//	  "interactions":[
//	    {"pair":["lisinopril","spironolactone"],
//	     "severity":"moderate",
//	     "mechanism":"both elevate serum K+",
//	     "management":"monitor potassium"}
//	  ],
//	  "checked":true,
//	  "metadata":{"source":"demo-synth","pairs_scanned":1}
//	}
type DrugInteraction struct{}

// Name implements Tool.
func (DrugInteraction) Name() string { return "drug_interaction" }

// Description implements Tool.
func (DrugInteraction) Description() string {
	return "Check a list of drug names for known interactions. Demo data: ~12 hard-coded pairs covering common clinical collisions (ACE inhibitor + K-sparing diuretic, warfarin + NSAID, statins + macrolides, SSRIs + MAOIs, etc.). Returns pairs found, with severity, mechanism, and management."
}

// Schema implements Tool.
func (DrugInteraction) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "drugs": {
      "type": "array",
      "description": "List of generic drug names (lowercase, e.g. \"lisinopril\"). Order does not matter.",
      "items": { "type": "string" },
      "minItems": 1
    }
  },
  "required": ["drugs"],
  "additionalProperties": false
}`)
}

// drugInteractionInput mirrors the JSON shape Invoke accepts.
type drugInteractionInput struct {
	Drugs []string `json:"drugs"`
}

// drugInteractionPair is one entry in the output's interactions array.
type drugInteractionPair struct {
	Pair       [2]string `json:"pair"`
	Severity   string    `json:"severity"`
	Mechanism  string    `json:"mechanism"`
	Management string    `json:"management"`
}

// drugInteractionOutput is the JSON Invoke returns.
type drugInteractionOutput struct {
	Interactions []drugInteractionPair `json:"interactions"`
	Checked      bool                  `json:"checked"`
	Metadata     map[string]any        `json:"metadata"`
}

// drugInteractionTable is the illustrative pair list. Each entry maps a
// canonical (a, b) tuple — where a < b lexicographically — to its
// severity/mechanism/management triple. Lookups normalize the pair before
// indexing so callers can pass the drugs in either order.
//
// This list is deliberately small (~12 pairs) and is tagged as demo-synth
// in every output. Do not extend it with un-vetted clinical claims.
var drugInteractionTable = map[[2]string]drugInteractionPair{
	keyOf("lisinopril", "spironolactone"): {
		Severity:   "moderate",
		Mechanism:  "both elevate serum K+ (RAAS suppression + K-sparing diuresis)",
		Management: "monitor potassium and renal function",
	},
	keyOf("enalapril", "spironolactone"): {
		Severity:   "moderate",
		Mechanism:  "both elevate serum K+ (RAAS suppression + K-sparing diuresis)",
		Management: "monitor potassium and renal function",
	},
	keyOf("warfarin", "ibuprofen"): {
		Severity:   "major",
		Mechanism:  "NSAID antiplatelet + GI mucosal injury on anticoagulation",
		Management: "avoid combination; consider acetaminophen for analgesia",
	},
	keyOf("warfarin", "naproxen"): {
		Severity:   "major",
		Mechanism:  "NSAID antiplatelet + GI mucosal injury on anticoagulation",
		Management: "avoid combination; consider acetaminophen for analgesia",
	},
	keyOf("simvastatin", "clarithromycin"): {
		Severity:   "major",
		Mechanism:  "CYP3A4 inhibition → elevated statin levels → rhabdomyolysis risk",
		Management: "hold statin for duration of macrolide course",
	},
	keyOf("atorvastatin", "erythromycin"): {
		Severity:   "moderate",
		Mechanism:  "CYP3A4 inhibition → elevated statin levels",
		Management: "consider dose reduction or alternative antibiotic",
	},
	keyOf("metformin", "iodinated_contrast"): {
		Severity:   "moderate",
		Mechanism:  "contrast nephropathy + metformin accumulation → lactic acidosis risk",
		Management: "hold metformin 48h pre and post contrast; recheck creatinine",
	},
	keyOf("fluoxetine", "phenelzine"): {
		Severity:   "contraindicated",
		Mechanism:  "serotonin syndrome from SSRI + MAOI combination",
		Management: "do not co-administer; observe 14-day washout between agents",
	},
	keyOf("sertraline", "tranylcypromine"): {
		Severity:   "contraindicated",
		Mechanism:  "serotonin syndrome from SSRI + MAOI combination",
		Management: "do not co-administer; observe 14-day washout between agents",
	},
	keyOf("amiodarone", "digoxin"): {
		Severity:   "major",
		Mechanism:  "amiodarone raises digoxin levels (P-gp inhibition)",
		Management: "reduce digoxin dose ~50% and monitor levels",
	},
	keyOf("metoprolol", "verapamil"): {
		Severity:   "major",
		Mechanism:  "additive negative chronotropy / dromotropy",
		Management: "avoid combination; risk of bradycardia and AV block",
	},
	keyOf("clopidogrel", "omeprazole"): {
		Severity:   "moderate",
		Mechanism:  "CYP2C19 inhibition reduces clopidogrel activation",
		Management: "prefer pantoprazole when a PPI is required",
	},
}

// keyOf returns the canonical (a, b) tuple — lower-cased and lexicographically
// ordered — used to index the drugInteractionTable.
func keyOf(a, b string) [2]string {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a < b {
		return [2]string{a, b}
	}
	return [2]string{b, a}
}

// Invoke implements Tool. Parses the drugs list, scans every unordered pair
// against the interaction table, and returns the matches.
func (DrugInteraction) Invoke(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("drug_interaction: empty arguments")
	}
	var in drugInteractionInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("drug_interaction: decode arguments: %w", err)
	}
	if len(in.Drugs) == 0 {
		return nil, fmt.Errorf("drug_interaction: drugs list is empty")
	}

	// Normalize and dedupe input so a drug listed twice is only scanned once.
	seen := make(map[string]struct{}, len(in.Drugs))
	drugs := make([]string, 0, len(in.Drugs))
	for _, d := range in.Drugs {
		n := strings.ToLower(strings.TrimSpace(d))
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		drugs = append(drugs, n)
	}
	sort.Strings(drugs)

	var found []drugInteractionPair
	pairsScanned := 0
	for i := 0; i < len(drugs); i++ {
		for j := i + 1; j < len(drugs); j++ {
			pairsScanned++
			k := keyOf(drugs[i], drugs[j])
			if hit, ok := drugInteractionTable[k]; ok {
				hit.Pair = k
				found = append(found, hit)
			}
		}
	}

	out := drugInteractionOutput{
		Interactions: found,
		Checked:      true,
		Metadata: map[string]any{
			"source":        "demo-synth",
			"pairs_scanned": pairsScanned,
		},
	}
	if out.Interactions == nil {
		out.Interactions = []drugInteractionPair{}
	}
	return json.Marshal(out)
}

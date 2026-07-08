package agents

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// PharmacistAgent checks drug-drug interactions before a care plan is
// finalised and emits a medication_review_complete message with a safe flag,
// the list of detected interactions, and any clinical recommendations.
//
// Input message Type:  medication_review_request
//
//	Content: {"patient_id","case_id","medications":[{"name","dose","frequency"}]}
//
// Output message Type: medication_review_complete
//
//	Content: {"safe":bool,"interactions":[{"drug_a","drug_b","severity","description"}],"recommendations":[string]}
type PharmacistAgent struct{}

// medicationInput is one entry in the incoming medication list.
type medicationInput struct {
	Name      string `json:"name"`
	Dose      string `json:"dose,omitempty"`
	Frequency string `json:"frequency,omitempty"`
}

// medicationReviewRequest is the full incoming payload.
type medicationReviewRequest struct {
	PatientID   string            `json:"patient_id"`
	CaseID      string            `json:"case_id,omitempty"`
	Medications []medicationInput `json:"medications"`
}

// DrugInteraction describes a clinically significant pair.
type DrugInteraction struct {
	DrugA       string `json:"drug_a"`
	DrugB       string `json:"drug_b"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
}

// medicationReviewResult is the full outgoing payload.
type medicationReviewResult struct {
	Safe            bool              `json:"safe"`
	Interactions    []DrugInteraction `json:"interactions"`
	Recommendations []string          `json:"recommendations"`
}

func (a *PharmacistAgent) ID() string   { return "pharmacist" }
func (a *PharmacistAgent) Name() string { return "Clinical Pharmacist" }
func (a *PharmacistAgent) Capabilities() []string {
	return []string{"drug_interaction_check", "medication_review"}
}

// HandleMessage parses the medication list, runs the rule-based interaction
// checker, and returns a single medication_review_complete message.
func (a *PharmacistAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req medicationReviewRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[pharmacist] invalid request: %v", err)
		return nil, err
	}

	interactions, recommendations := checkInteractions(req.Medications)
	safe := len(interactions) == 0

	result := medicationReviewResult{
		Safe:            safe,
		Interactions:    interactions,
		Recommendations: recommendations,
	}

	env.Logf("[pharmacist] patient=%s medications=%d interactions=%d safe=%v",
		req.PatientID, len(req.Medications), len(interactions), safe)

	body, _ := json.Marshal(result)
	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = req.PatientID
	if req.CaseID != "" {
		meta["case_id"] = req.CaseID
	}

	out := agent.NewMessage(a.ID(), msg.From, agent.RoleAgent,
		"medication_review_complete", string(body), meta)
	return []agent.Message{out}, nil
}

// ── interaction rule database ────────────────────────────────────────────────

// interactionRule encodes a single known drug-drug interaction pair.
type interactionRule struct {
	// normalized lower-case tokens for each drug in the pair.
	a, b        []string
	severity    string
	description string
}

// qtDrugs lists QT-prolonging agents; any two of them together are flagged.
var qtDrugs = []string{"amiodarone", "haloperidol", "azithromycin"}

// knownPairs is the top-10 clinically significant interaction database.
// Matching is token-based (lower-case, trimmed) so "Warfarin 5 mg" matches
// the "warfarin" token.
var knownPairs = []interactionRule{
	{
		a: []string{"warfarin"}, b: []string{"aspirin"},
		severity:    "HIGH",
		description: "Concurrent use increases bleeding risk; warfarin anticoagulation potentiated by aspirin's antiplatelet and GI-irritant effects.",
	},
	{
		a: []string{"warfarin"}, b: []string{"nsaid", "ibuprofen", "naproxen", "diclofenac", "celecoxib", "indomethacin", "ketorolac", "meloxicam"},
		severity:    "HIGH",
		description: "Warfarin + NSAIDs: additive bleeding risk via antiplatelet action and gastric mucosal injury.",
	},
	{
		a:           []string{"ace inhibitor", "lisinopril", "enalapril", "ramipril", "captopril", "benazepril", "perindopril", "quinapril", "fosinopril"},
		b:           []string{"potassium", "potassium supplement", "kcl"},
		severity:    "HIGH",
		description: "ACE inhibitors reduce aldosterone-mediated potassium excretion; potassium supplements risk severe hyperkalemia.",
	},
	{
		a:           []string{"metformin"},
		b:           []string{"contrast dye", "iodinated contrast", "contrast media", "contrast agent"},
		severity:    "MODERATE",
		description: "Iodinated contrast media can transiently impair renal clearance of metformin, raising lactic acidosis risk; hold metformin 48 h post-contrast.",
	},
	{
		a:           []string{"digoxin"},
		b:           []string{"amiodarone"},
		severity:    "HIGH",
		description: "Amiodarone inhibits P-glycoprotein and CYP3A4, substantially increasing digoxin plasma levels and toxicity risk.",
	},
	{
		a:           []string{"simvastatin"},
		b:           []string{"amiodarone"},
		severity:    "MODERATE",
		description: "Amiodarone inhibits CYP3A4-mediated metabolism of simvastatin, increasing myopathy risk; consider dose reduction or switch to pravastatin/rosuvastatin.",
	},
	{
		a:           []string{"sildenafil", "tadalafil", "vardenafil"},
		b:           []string{"nitrate", "nitroglycerin", "isosorbide", "isosorbide mononitrate", "isosorbide dinitrate"},
		severity:    "CONTRAINDICATED",
		description: "PDE5 inhibitors + organic nitrates: synergistic hypotension can be severe or fatal; combination is absolutely contraindicated.",
	},
	{
		a:           []string{"ssri", "fluoxetine", "sertraline", "paroxetine", "escitalopram", "citalopram", "fluvoxamine"},
		b:           []string{"tramadol"},
		severity:    "HIGH",
		description: "SSRIs combined with tramadol markedly elevate serotonin syndrome risk via serotonin reuptake inhibition plus tramadol's weak serotonergic activity.",
	},
	{
		a:           []string{"lithium"},
		b:           []string{"nsaid", "ibuprofen", "naproxen", "diclofenac", "celecoxib", "indomethacin", "ketorolac", "meloxicam"},
		severity:    "MODERATE",
		description: "NSAIDs reduce renal lithium clearance by inhibiting prostaglandin-dependent GFR, increasing lithium serum levels toward toxicity.",
	},
}

// checkInteractions runs all interaction rules against the supplied medication
// list and returns detected interactions plus human-readable recommendations.
func checkInteractions(meds []medicationInput) ([]DrugInteraction, []string) {
	names := make([]string, len(meds))
	for i, m := range meds {
		names[i] = strings.ToLower(strings.TrimSpace(m.Name))
	}

	var found []DrugInteraction
	seen := map[string]bool{} // dedup key: "drugA|drugB"

	// ── named-pair rules ───────────────────────────────────────────────────
	for _, rule := range knownPairs {
		idxA := matchAny(names, rule.a)
		idxB := matchAny(names, rule.b)
		if idxA < 0 || idxB < 0 {
			continue
		}
		nameA := names[idxA]
		nameB := names[idxB]
		key := nameA + "|" + nameB
		if seen[key] {
			continue
		}
		seen[key] = true
		found = append(found, DrugInteraction{
			DrugA:       meds[idxA].Name,
			DrugB:       meds[idxB].Name,
			Severity:    rule.severity,
			Description: rule.description,
		})
	}

	// ── QT-prolonging drug combinations ───────────────────────────────────
	var presentQT []int // indices of QT drugs in names
	for i, n := range names {
		for _, qt := range qtDrugs {
			if strings.Contains(n, qt) {
				presentQT = append(presentQT, i)
				break
			}
		}
	}
	for i := 0; i < len(presentQT); i++ {
		for j := i + 1; j < len(presentQT); j++ {
			a, b := presentQT[i], presentQT[j]
			key := names[a] + "|" + names[b]
			if seen[key] {
				continue
			}
			seen[key] = true
			found = append(found, DrugInteraction{
				DrugA:       meds[a].Name,
				DrugB:       meds[b].Name,
				Severity:    "HIGH",
				Description: "Two or more QT-prolonging agents (amiodarone, haloperidol, azithromycin) co-prescribed; combination increases torsades de pointes risk.",
			})
		}
	}

	// ── recommendations ────────────────────────────────────────────────────
	var recs []string
	contras := 0
	highs := 0
	for _, ix := range found {
		switch ix.Severity {
		case "CONTRAINDICATED":
			contras++
			recs = append(recs, "CONTRAINDICATED: "+ix.DrugA+" and "+ix.DrugB+" must not be co-prescribed. Discontinue one agent immediately.")
		case "HIGH":
			highs++
			recs = append(recs, "HIGH severity interaction between "+ix.DrugA+" and "+ix.DrugB+": "+ix.Description)
		case "MODERATE":
			recs = append(recs, "MODERATE interaction between "+ix.DrugA+" and "+ix.DrugB+" — monitor closely: "+ix.Description)
		}
	}
	if contras > 0 {
		recs = append(recs, "Prescriber must review and resolve all CONTRAINDICATED combinations before dispensing.")
	}
	if highs > 0 {
		recs = append(recs, "Prescriber counselling and enhanced monitoring required for all HIGH-severity interactions.")
	}
	return found, recs
}

// matchAny returns the index of the first name in names that contains any of
// the tokens in tokens, or -1 if none match.
func matchAny(names []string, tokens []string) int {
	for i, n := range names {
		for _, tok := range tokens {
			if strings.Contains(n, tok) {
				return i
			}
		}
	}
	return -1
}

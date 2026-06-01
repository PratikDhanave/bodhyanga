// pharmacist.go — clinical pharmacist drug-drug interaction checker.
//
// Overview:
//   Implements a rule-based medication safety review that fires before a care
//   plan is finalised.  It covers the top 10 clinically significant drug-drug
//   interaction categories and flags each pair with a severity level
//   (CONTRAINDICATED / HIGH / MODERATE) together with a human-readable
//   clinical description and actionable recommendations.
//
// Key declarations:
//   - PharmacistAgent:          agent struct (stateless — all rules are package-level).
//   - PharmacistAgent.HandleMessage: parses the incoming medication list,
//                               calls checkInteractions, and returns a single
//                               medication_review_complete message.
//   - checkInteractions:        the core rule engine; iterates knownPairs plus
//                               QT-drug detection and deduplicates results.
//   - knownPairs:               top-10 named interaction rules.
//   - qtDrugs:                  amiodarone, haloperidol, azithromycin; any
//                               two of these together trigger torsades risk.
//   - matchAny:                 token-based substring matcher used by the rule
//                               engine so brand/generic name variants match.
//
// Interaction pairs covered:
//   1.  warfarin + aspirin                          → bleeding risk (HIGH)
//   2.  warfarin + NSAIDs                           → bleeding risk (HIGH)
//   3.  ACE inhibitors + potassium supplements      → hyperkalemia (HIGH)
//   4.  metformin + iodinated contrast media        → lactic acidosis note (MODERATE)
//   5.  digoxin + amiodarone                        → digoxin toxicity (HIGH)
//   6.  simvastatin + amiodarone                    → myopathy (MODERATE)
//   7.  PDE5 inhibitors + organic nitrates          → severe hypotension (CONTRAINDICATED)
//   8.  SSRIs + tramadol                            → serotonin syndrome (HIGH)
//   9.  lithium + NSAIDs                            → lithium toxicity (MODERATE)
//   10. QT-prolonging drug combinations             → torsades de pointes (HIGH)
//
// Message contract:
//   Input  type: medication_review_request
//     {"patient_id":"…","case_id":"…","medications":[{"name":"…","dose":"…","frequency":"…"}]}
//   Output type: medication_review_complete
//     {"safe":bool,"interactions":[{"drug_a","drug_b","severity","description"}],"recommendations":[…]}
//
// Thread safety:
//   PharmacistAgent is stateless; HandleMessage is safe for concurrent use.
//
// See doc.go for package-level documentation.

package agents

// infectious_disease.go — antibiotic stewardship agent.
//
// Overview:
//   Implements InfectiousDiseaseAgent, a rule-based antibiotic stewardship engine
//   modelled on a simplified Sanford Guide decision tree. Given an id_consult
//   message describing infection site, severity, allergies, and culture status,
//   the agent selects a first-line antibiotic regimen and emits an
//   id_recommendation message back to the requesting agent.
//
// Key declarations:
//   - InfectiousDiseaseAgent: agent with ID "infectious_disease".
//   - idConsult: wire format for the id_consult message Content.
//   - idRecommendation: wire format for the id_recommendation message Content.
//   - selectRegimen: top-level dispatch that routes to per-site helpers.
//   - selectCAP: CAP mild/moderate/severe with penicillin-allergy branching.
//   - selectUTI: uncomplicated vs complicated UTI.
//   - selectCellulitis: mild/severe SSTI with penicillin-allergy branching.
//   - selectSepsis: broad-spectrum empiric coverage (MRSA + gram-negative).
//   - selectMRSA: vancomycin with AUC/MIC guidance.
//   - hasPenicillinAllergy: substring scan of the allergy list.
//
// Antibiotic selection logic (simplified Sanford Guide):
//
//   CAP mild:       azithromycin 500 mg PO x5d
//                   → doxycycline if penicillin/macrolide allergy
//   CAP moderate:   amoxicillin-clavulanate + azithromycin PO x7d
//                   → levofloxacin if penicillin allergy
//   CAP severe:     piperacillin-tazobactam + azithromycin IV x7d
//                   → aztreonam + azithromycin if penicillin allergy
//   UTI mild:       nitrofurantoin 100 mg PO x5d
//   UTI complicated: ciprofloxacin 500 mg BID PO x7d
//   Cellulitis mild: cephalexin 500 mg QID PO x5d
//                    → clindamycin if penicillin allergy
//   Cellulitis mod/severe: cefazolin IV x7d
//                    → vancomycin IV if penicillin allergy
//   Sepsis:         vancomycin + piperacillin-tazobactam IV x7d
//                   → vancomycin + aztreonam if penicillin allergy
//   MRSA suspected: vancomycin 15–20 mg/kg IV q8–12h x14d
//
// When culture_pending is true, the de_escalation_criteria field always
// includes an explicit reminder to narrow therapy once sensitivity results
// are available, regardless of the site-specific default.
//
// See doc.go for package-level documentation.

package agents

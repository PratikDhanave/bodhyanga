// radiologist.go — rule-based radiology report interpreter.
//
// Overview:
//   Receives a radiology_request (patient_id, case_id, modality, report_text,
//   body_part) and emits a radiology_report (findings, impression,
//   critical_finding, follow_up_needed, urgency) back to the requesting agent.
//
//   Critical finding detection is rule-based, keyed on (modality, body_part)
//   and matched against lower-cased report_text. Each matching rule contributes
//   a human-readable finding label; the highest urgency tier across all matches
//   governs the output urgency field. Critical findings (urgency=emergent) force
//   critical_finding=true and follow_up_needed=true.
//
// Key declarations:
//   - RadiologistAgent: zero-value-usable agent struct.
//   - RadiologistAgent.HandleMessage: decodes request, calls interpretRadiologyReport, emits reply.
//   - interpretRadiologyReport: pure function; orchestrates finding extraction + urgency classification.
//   - radiologyRulesFor: returns the ordered rule table for a (modality, body_part) pair.
//   - extractRadiologyFindings: matches rules against report text; returns finding labels.
//   - classifyRadiologyUrgency: picks the highest urgency tier from matched findings.
//   - buildRadiologyImpression: formats the Impression section from findings and urgency.
//
// Supported rule groups:
//   CXR:        pneumothorax (emergent), large pleural effusion (urgent), infiltrate (routine)
//   CT chest:   pulmonary embolism (emergent), aortic dissection (emergent), pneumonia (urgent)
//   CT head:    hemorrhage/bleed (emergent), stroke/infarct (emergent), mass (urgent)
//   Abdomen:    free air (emergent), obstruction (urgent), appendicitis (urgent)
//
// See doc.go for package-level documentation.

package agents

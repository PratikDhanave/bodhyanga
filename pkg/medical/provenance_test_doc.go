// provenance_test.go — tests for HashPrompt and MakeProvenance.
//
// Overview:
//   Verifies HashPrompt is deterministic, length-stable (64-char SHA-256
//   hex), sensitive to changes in both system and user inputs, and uses a
//   delimiter that prevents naive (ab,c) / (a,bc) concatenation collisions.
//   Verifies MakeProvenance populates all required fields, copies citation
//   Source strings into CitationIDs, formats Timestamp as RFC3339, and
//   leaves CitationIDs nil (and JSON-omitted) when no citations are
//   supplied. Also asserts the Provenance field on DiagnosisProposal is
//   `omitempty` so rule-based JSON output stays compact.
//
// Key declarations:
//   - TestHashPrompt_DeterministicAndDifferentInputsDiffer
//   - TestMakeProvenance_FieldsPopulated
//   - TestMakeProvenance_OmitsEmptyCitations
//   - TestDiagnosisProposal_ProvenanceOmitemptyInJSON
//
// See doc.go for package-level documentation.

package medical

// adt.go — admit/discharge/transfer events and a LACE-inspired readmission score.
//
// Overview:
//   Defines hospital ADT message types and a transparent, rule-based
//   readmission-risk score derived from registry fields (age, condition
//   burden, recent discharge, BP/A1c control). Codecs let ADT events ride
//   the agent bus.
//
// Key declarations:
//   - ADTEventType: admit / discharge / transfer constants
//   - ADTEvent: single hospital ADT message
//   - EncodeADT / DecodeADT: JSON helpers for bus transport
//   - ReadmissionRisk: 0..1 score with low/medium/high band
//   - ScoreReadmissionRisk: LACE-inspired demo scoring function
//
// See doc.go for package-level documentation.

package medical

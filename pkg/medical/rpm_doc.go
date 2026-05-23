// rpm.go — remote patient monitoring observations and alert thresholds.
//
// Overview:
//   Models chronic-care RPM device readings (BP cuff, scale, pulse ox, heart
//   rate) and evaluates them against illustrative demo thresholds. Out-of-range
//   readings produce human-readable alert strings for downstream agents.
//
// Key declarations:
//   - RPMObservation: single device reading (metric, value, unit, alert flag)
//   - EvaluateRPMAlerts: returns alert strings for out-of-range vitals
//
// See doc.go for package-level documentation.

package medical

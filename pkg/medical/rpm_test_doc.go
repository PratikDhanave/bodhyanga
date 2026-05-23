// rpm_test.go — tests for RPM alert evaluation against demo thresholds.
//
// Overview:
//   Exercises EvaluateRPMAlerts using a CHF-style observation set
//   (elevated systolic BP, weight, low SpO2) and asserts that at least
//   one alert fires per out-of-range reading.
//
// Key declarations:
//   - TestEvaluateRPMAlerts_CHF: smoke test that multi-metric alerts fire
//
// See doc.go for package-level documentation.

package medical

// calibration_test.go — unit tests for temperature-scaling calibration.
//
// Overview:
//   Verifies that TemperatureScaler is the identity at T=1.0, that T>1
//   softens extremes (p=0.95 falls but stays >0.5; p=0.05 rises but
//   stays <0.5), and that edge probabilities (0 and 1) survive the
//   clamp without NaN or Inf. FitTemperature is exercised on a
//   synthetic over-confident dataset (raw=0.9, ~60% correct) and must
//   converge to T>1.5; the insufficient-data and uniform-label
//   degenerate paths are checked explicitly.
//
// Key declarations:
//   - TestTemperatureScaler_IdentityAtTOne
//   - TestTemperatureScaler_T2SoftensExtremes
//   - TestTemperatureScaler_HandlesEdgeProbabilities
//   - TestFitTemperature_Convergence
//   - TestFitTemperature_InsufficientData
//   - TestFitTemperature_UniformLabels
//
// See doc.go for package-level documentation.

package eval

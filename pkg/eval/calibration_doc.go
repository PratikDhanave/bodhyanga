// calibration.go — confidence calibration via temperature scaling.
//
// Overview:
//   Diagnosticians return a raw confidence per proposal, but raw LLM
//   probabilities are not well-calibrated: a "0.9" may correspond to
//   ~60% empirical correctness. TemperatureScaler is a one-parameter
//   post-hoc calibrator that applies sigmoid(logit(p)/T). T=1.0 is a
//   no-op, T>1 softens overconfident scores, T<1 sharpens them.
//   FitTemperature learns T from a held-out set of (score, label) pairs
//   by minimising mean negative log-likelihood with a golden-section
//   search on [0.1, 10.0] — the loss is smooth and 1-D so this beats
//   anything more elaborate.
//
// Key declarations:
//   - Calibrator: interface with Calibrate(raw) float64.
//   - TemperatureScaler: single-parameter calibrator.
//   - FitTemperature(scores, labels): golden-section search for T.
//   - ErrInsufficientData: returned when fewer than 2 pairs are supplied.
//
// Edge cases:
//   - Calibrate clamps raw into [eps, 1-eps] so 0 and 1 don't NaN.
//   - FitTemperature returns T=1.0 (and no error) for uniform labels.
//
// See doc.go for package-level documentation.

package eval

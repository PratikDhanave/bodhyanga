// risk_calculator.go — RiskCalculator tool with four sub-calculators.
//
// Overview:
//   Dispatch-style Tool that wraps four canonical clinical risk scores
//   (Wells PE, CURB-65, CHA2DS2-VASc, HAS-BLED). Each sub-calculator
//   implements the standard scoring rule and returns the numeric score, a
//   condensed risk band, and a per-component breakdown. Outputs carry
//   metadata.source = "demo-synth" so consumers know the banding text is
//   illustrative and not a substitute for vetted decision support.
//
// Key declarations:
//   - RiskCalculator: dispatch Tool implementation
//   - DefaultRiskCalculator() *RiskCalculator: factory wired to all four
//   - wellsPE, curb65, cha2ds2vasc, hasBled: per-score functions
//   - boolField / numField: lenient inputs decoders
//
// See doc.go for package-level documentation.

package tools

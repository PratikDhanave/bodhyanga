package eval

import (
	"errors"
	"math"
)

// Calibrator converts a raw confidence into a calibrated one.
type Calibrator interface {
	Calibrate(raw float64) float64
}

// TemperatureScaler is a single-parameter calibrator. The calibrated
// probability is sigmoid(logit(p)/T). T=1.0 is a no-op; T>1 softens
// extreme probabilities toward 0.5, T<1 sharpens them toward 0 or 1.
type TemperatureScaler struct {
	// Temperature controls the calibration. 1.0 is a no-op. Non-positive
	// values fall back to 1.0 so callers cannot accidentally invert or
	// divide by zero.
	Temperature float64
}

// epsCalib bounds the raw probability so logit(0) and logit(1) don't
// blow up to -Inf / +Inf. Picked small enough to be invisible at
// printing precision but large enough to stay finite under sigmoid.
const epsCalib = 1e-6

// Calibrate implements Calibrator.
func (t TemperatureScaler) Calibrate(raw float64) float64 {
	T := t.Temperature
	if T <= 0 {
		T = 1.0
	}
	// Clamp into (eps, 1-eps) so logit is finite.
	p := raw
	if p < epsCalib {
		p = epsCalib
	}
	if p > 1-epsCalib {
		p = 1 - epsCalib
	}
	logit := math.Log(p / (1 - p))
	return sigmoid(logit / T)
}

// sigmoid is the standard logistic function. Implemented to be stable
// for large negative or positive x by branching on the sign.
func sigmoid(x float64) float64 {
	if x >= 0 {
		e := math.Exp(-x)
		return 1.0 / (1.0 + e)
	}
	e := math.Exp(x)
	return e / (1.0 + e)
}

// ErrInsufficientData is returned by FitTemperature when fewer than two
// (score, label) pairs are supplied.
var ErrInsufficientData = errors.New("eval: insufficient data to fit temperature (need >= 2 pairs)")

// FitTemperature finds the T in [0.1, 10.0] that minimises the
// negative log-likelihood of labels under the temperature-scaled raw
// scores. Labels are 1.0 for "correct" and 0.0 for "incorrect"; any
// value in [0,1] is accepted (soft labels). Uses a golden-section
// search — the NLL is smooth in T on a 1-D bounded interval so this
// converges in ~30 iterations.
//
// Degenerate inputs are handled explicitly:
//   - len(scores) < 2 or mismatched lengths: ErrInsufficientData
//   - all labels identical (no signal to fit against): T=1.0, nil err
func FitTemperature(scores, labels []float64) (float64, error) {
	if len(scores) != len(labels) || len(scores) < 2 {
		return 0, ErrInsufficientData
	}
	// If labels are uniform (all 0 or all 1) the NLL is monotone in T
	// and the optimum runs off to the boundary; that's not informative,
	// so we return the identity temperature.
	first := labels[0]
	uniform := true
	for _, l := range labels[1:] {
		if l != first {
			uniform = false
			break
		}
	}
	if uniform {
		return 1.0, nil
	}

	const (
		lo        = 0.1
		hi        = 10.0
		tol       = 1e-4
		maxIter   = 60
	)
	// Golden ratio constant: phi = (sqrt(5)-1)/2.
	phi := (math.Sqrt(5) - 1) / 2

	a, b := lo, hi
	c := b - phi*(b-a)
	d := a + phi*(b-a)
	fc := nllAtT(scores, labels, c)
	fd := nllAtT(scores, labels, d)
	for i := 0; i < maxIter && (b-a) > tol; i++ {
		if fc < fd {
			b = d
			d = c
			fd = fc
			c = b - phi*(b-a)
			fc = nllAtT(scores, labels, c)
		} else {
			a = c
			c = d
			fc = fd
			d = a + phi*(b-a)
			fd = nllAtT(scores, labels, d)
		}
	}
	return (a + b) / 2, nil
}

// nllAtT returns the mean negative log-likelihood of labels under the
// temperature-scaled raw scores. Used by FitTemperature.
func nllAtT(scores, labels []float64, T float64) float64 {
	scaler := TemperatureScaler{Temperature: T}
	var sum float64
	for i, s := range scores {
		p := scaler.Calibrate(s)
		// Re-clamp because soft labels can still produce p exactly at the
		// boundary after sigmoid for very extreme inputs.
		if p < epsCalib {
			p = epsCalib
		}
		if p > 1-epsCalib {
			p = 1 - epsCalib
		}
		y := labels[i]
		sum += -(y*math.Log(p) + (1-y)*math.Log(1-p))
	}
	return sum / float64(len(scores))
}

package graduation

import "math"

// DefaultZ is the z-score for a 95% one-sided confidence level (1.96), the
// default used when Criteria.Z is unset.
const DefaultZ = 1.96

// WilsonLowerBound returns the lower bound of the Wilson score confidence
// interval for a binomial proportion — successes out of total trials, at the
// given z.
//
// Why Wilson and not the naive successes/total: a raw rate of 5/5 = 1.0 looks
// perfect but is backed by almost no evidence; Wilson penalizes small samples,
// so 5/5 yields a lower bound well under 1.0 while 950/1000 yields a bound near
// its rate. Gating promotion on the lower bound therefore demands BOTH a high
// success rate AND enough observations to trust it — exactly the property a
// patient-safety promotion gate needs. This is the same statistic used to rank
// "x of y positive" items by confidence.
//
// Edge cases: total <= 0 returns 0 (no evidence ⇒ no confidence). A negative or
// out-of-range successes is clamped to [0, total]. z <= 0 falls back to
// DefaultZ.
func WilsonLowerBound(successes, total int, z float64) float64 {
	if total <= 0 {
		return 0
	}
	if z <= 0 {
		z = DefaultZ
	}
	if successes < 0 {
		successes = 0
	}
	if successes > total {
		successes = total
	}

	n := float64(total)
	phat := float64(successes) / n
	z2 := z * z

	denom := 1 + z2/n
	center := phat + z2/(2*n)
	margin := z * math.Sqrt(phat*(1-phat)/n+z2/(4*n*n))

	lower := (center - margin) / denom
	if lower < 0 {
		return 0
	}
	return lower
}

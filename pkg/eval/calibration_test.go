package eval

import (
	"errors"
	"math"
	"math/rand"
	"testing"
)

func TestTemperatureScaler_IdentityAtTOne(t *testing.T) {
	s := TemperatureScaler{Temperature: 1.0}
	for _, p := range []float64{0.1, 0.3, 0.5, 0.7, 0.9} {
		got := s.Calibrate(p)
		if math.Abs(got-p) > 1e-9 {
			t.Fatalf("T=1.0 must be identity; p=%v got=%v", p, got)
		}
	}
}

func TestTemperatureScaler_T2SoftensExtremes(t *testing.T) {
	s := TemperatureScaler{Temperature: 2.0}
	// p=0.95 with T=2.0 should soften toward 0.5, i.e. be lower than 0.95.
	got := s.Calibrate(0.95)
	if got >= 0.95 {
		t.Fatalf("expected T=2 to soften 0.95 below 0.95; got %v", got)
	}
	if got <= 0.5 {
		t.Fatalf("expected softened value to remain above 0.5; got %v", got)
	}
	// And p=0.05 should rise toward 0.5 from below.
	low := s.Calibrate(0.05)
	if low <= 0.05 {
		t.Fatalf("expected T=2 to soften 0.05 above 0.05; got %v", low)
	}
	if low >= 0.5 {
		t.Fatalf("expected softened low value to remain below 0.5; got %v", low)
	}
}

func TestTemperatureScaler_HandlesEdgeProbabilities(t *testing.T) {
	for _, T := range []float64{0.5, 1.0, 2.0, 5.0} {
		s := TemperatureScaler{Temperature: T}
		for _, p := range []float64{0.0, 1.0} {
			got := s.Calibrate(p)
			if math.IsNaN(got) || math.IsInf(got, 0) {
				t.Fatalf("Calibrate(%v) with T=%v produced non-finite %v", p, T, got)
			}
			if got < 0 || got > 1 {
				t.Fatalf("Calibrate(%v) with T=%v out of [0,1]: %v", p, T, got)
			}
		}
	}
}

func TestFitTemperature_Convergence(t *testing.T) {
	// Build a synthetic dataset where the model is over-confident: raw=0.9
	// but only 60% of those samples are actually correct. A well-fit T
	// must be > 1.5 to soften the confidence toward the empirical rate.
	rng := rand.New(rand.NewSource(42))
	const n = 400
	scores := make([]float64, n)
	labels := make([]float64, n)
	for i := 0; i < n; i++ {
		scores[i] = 0.9
		if rng.Float64() < 0.6 {
			labels[i] = 1.0
		} else {
			labels[i] = 0.0
		}
	}
	// Sprinkle a few low-confidence samples that are mostly wrong so the
	// fit has both ends of the curve.
	for i := 0; i < n/4; i++ {
		scores = append(scores, 0.2)
		if rng.Float64() < 0.25 {
			labels = append(labels, 1.0)
		} else {
			labels = append(labels, 0.0)
		}
	}

	T, err := FitTemperature(scores, labels)
	if err != nil {
		t.Fatalf("FitTemperature: %v", err)
	}
	if T <= 1.5 {
		t.Fatalf("expected over-confidence to push T above 1.5; got T=%v", T)
	}
	if T > 10.0 {
		t.Fatalf("T must stay within [0.1, 10.0]; got %v", T)
	}
}

func TestFitTemperature_InsufficientData(t *testing.T) {
	if _, err := FitTemperature(nil, nil); !errors.Is(err, ErrInsufficientData) {
		t.Fatalf("nil/nil: want ErrInsufficientData, got %v", err)
	}
	if _, err := FitTemperature([]float64{0.5}, []float64{1.0}); !errors.Is(err, ErrInsufficientData) {
		t.Fatalf("len=1: want ErrInsufficientData, got %v", err)
	}
	if _, err := FitTemperature([]float64{0.5, 0.6}, []float64{1.0}); !errors.Is(err, ErrInsufficientData) {
		t.Fatalf("mismatched lens: want ErrInsufficientData, got %v", err)
	}
}

func TestFitTemperature_UniformLabels(t *testing.T) {
	scores := []float64{0.1, 0.3, 0.5, 0.7, 0.9}
	// All-ones.
	labels := []float64{1, 1, 1, 1, 1}
	T, err := FitTemperature(scores, labels)
	if err != nil {
		t.Fatalf("uniform 1s: %v", err)
	}
	if T != 1.0 {
		t.Fatalf("uniform labels should yield T=1.0, got %v", T)
	}
	// All-zeros.
	labels = []float64{0, 0, 0, 0, 0}
	T, err = FitTemperature(scores, labels)
	if err != nil {
		t.Fatalf("uniform 0s: %v", err)
	}
	if T != 1.0 {
		t.Fatalf("uniform labels should yield T=1.0, got %v", T)
	}
}

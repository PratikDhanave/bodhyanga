package safety

import (
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/knowledge"
)

// --- significantTerms edge cases ---

func TestSignificantTerms_DuplicatesRemoved(t *testing.T) {
	// "pneumonia pneumonia" should yield a single term.
	terms := significantTerms("pneumonia pneumonia pneumonia")
	if len(terms) != 1 {
		t.Errorf("expected 1 unique term, got %d: %v", len(terms), terms)
	}
}

func TestSignificantTerms_StopwordsFiltered(t *testing.T) {
	// All stopwords >= 4 chars should be filtered.
	terms := significantTerms("this that from have been were their they would could should patient patients diagnosis treatment clinical based evidence")
	if len(terms) != 0 {
		t.Errorf("expected 0 significant terms (all stopwords), got %d: %v", len(terms), terms)
	}
}

func TestSignificantTerms_ShortWordsFiltered(t *testing.T) {
	// Words shorter than 4 chars should be excluded.
	terms := significantTerms("the a an is to of in or by at on it")
	if len(terms) != 0 {
		t.Errorf("expected 0 terms (all too short), got %d: %v", len(terms), terms)
	}
}

func TestSignificantTerms_HyphensPreserved(t *testing.T) {
	terms := significantTerms("beta-blocker is recommended")
	found := false
	for _, t2 := range terms {
		if t2 == "beta-blocker" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'beta-blocker' in terms, got %v", terms)
	}
}

// --- formatFloat edge cases ---

func TestFormatFloat_NegativeClampedToZero(t *testing.T) {
	got := formatFloat(-0.5)
	if got != "0" {
		t.Errorf("formatFloat(-0.5) = %q, want %q", got, "0")
	}
}

func TestFormatFloat_AboveOneClampedToOne(t *testing.T) {
	got := formatFloat(1.5)
	if got != "1" {
		t.Errorf("formatFloat(1.5) = %q, want %q", got, "1")
	}
}

func TestFormatFloat_ZeroValue(t *testing.T) {
	got := formatFloat(0)
	if got != "0" {
		t.Errorf("formatFloat(0) = %q, want %q", got, "0")
	}
}

func TestFormatFloat_OneValue(t *testing.T) {
	got := formatFloat(1.0)
	if got != "1" {
		t.Errorf("formatFloat(1.0) = %q, want %q", got, "1")
	}
}

func TestFormatFloat_HalfValue(t *testing.T) {
	got := formatFloat(0.5)
	if got != "0.5" {
		t.Errorf("formatFloat(0.5) = %q, want %q", got, "0.5")
	}
}

// --- stringFromFloat edge cases ---

func TestStringFromFloat_SmallMinor(t *testing.T) {
	// minor < 10 path: 0.005 → whole=5, major=0, minor=5
	got := stringFromFloat(0.005)
	if got != "0.005" {
		t.Errorf("stringFromFloat(0.005) = %q, want %q", got, "0.005")
	}
}

func TestStringFromFloat_MediumMinor(t *testing.T) {
	// minor >= 10 && minor < 100 path: 0.05 → whole=50, major=0, minor=50
	got := stringFromFloat(0.05)
	if got != "0.050" {
		t.Errorf("stringFromFloat(0.05) = %q, want %q", got, "0.050")
	}
}

func TestStringFromFloat_LargeMinor(t *testing.T) {
	// minor >= 100 path: 0.5 → whole=500, major=0, minor=500
	got := stringFromFloat(0.5)
	if got != "0.500" {
		t.Errorf("stringFromFloat(0.5) = %q, want %q", got, "0.500")
	}
}

// --- itoa edge cases ---

func TestItoa_Zero(t *testing.T) {
	got := itoa(0)
	if got != "0" {
		t.Errorf("itoa(0) = %q, want %q", got, "0")
	}
}

func TestItoa_Negative(t *testing.T) {
	got := itoa(-42)
	if got != "-42" {
		t.Errorf("itoa(-42) = %q, want %q", got, "-42")
	}
}

func TestItoa_LargePositive(t *testing.T) {
	got := itoa(12345)
	if got != "12345" {
		t.Errorf("itoa(12345) = %q, want %q", got, "12345")
	}
}

// --- stringFromFloat: negative minor path (defensive code) ---

func TestStringFromFloat_NegativeMinorPath(t *testing.T) {
	// Calling stringFromFloat with a negative value triggers the minor < 0 branch.
	// This is defensive code that can't be reached through formatFloat
	// (which clamps to [0,1]), but we test it directly for coverage.
	got := stringFromFloat(-0.005)
	// -0.005 * 1000 = -5, major = -5/1000 = 0, minor = -5 % 1000 = -5
	// minor < 0 -> minor = 5
	// Result: "0.005" (the negative is lost because major is 0)
	if got != "0.005" {
		t.Errorf("stringFromFloat(-0.005) = %q", got)
	}
}

// --- CheckHallucination with threshold triggering the low grounding message ---

func TestCheckHallucinationThreshold_LowGroundingShowsFraction(t *testing.T) {
	// Use terms that won't be found in citations to trigger the formatFloat path.
	claim := "xylophone zebra quartz vortex"
	cites := []knowledge.Citation{
		{Title: "Nothing related", Snippet: "completely unrelated snippet"},
	}
	v := CheckHallucinationThreshold(claim, cites, 0.5)
	if v.OK {
		t.Errorf("expected failure for ungrounded claim")
	}
	if v.Reason == "" {
		t.Errorf("expected non-empty reason")
	}
}

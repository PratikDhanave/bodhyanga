package bench

import "testing"

func TestScore_TrimmedCaseInsensitive(t *testing.T) {
	cases := []struct {
		gold, produced string
		want           Status
	}{
		{"acute coronary syndrome", "acute coronary syndrome", StatusMatch},
		{"Heart Failure", "heart failure", StatusMatch},          // case-diff
		{"  heart failure  ", "heart failure", StatusMatch},       // whitespace-diff
		{"heart failure", "community-acquired pneumonia", StatusMiss},
		{"", "anything", StatusUnscored},                          // empty gold
		{"   ", "anything", StatusUnscored},                       // whitespace-only gold
	}
	for _, c := range cases {
		if got := Score(c.gold, c.produced); got != c.want {
			t.Errorf("Score(%q,%q) = %q, want %q", c.gold, c.produced, got, c.want)
		}
	}
}

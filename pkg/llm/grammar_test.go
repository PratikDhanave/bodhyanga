package llm

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
)

func TestGBNFJSONObjectShape(t *testing.T) {
	g := GBNFJSONObject()
	if g.Format != GrammarGBNF {
		t.Fatalf("format=%q want %q", g.Format, GrammarGBNF)
	}
	for _, want := range []string{"root", "::=", "object", "string", "number", `"true"`, `"false"`, `"null"`} {
		if !strings.Contains(g.Body, want) {
			t.Errorf("GBNFJSONObject body missing %q:\n%s", want, g.Body)
		}
	}
	// Root rule must be first.
	if !strings.HasPrefix(strings.TrimSpace(g.Body), "root") {
		t.Errorf("GBNFJSONObject body must start with root rule; got:\n%s", g.Body)
	}
}

func TestGBNFEnum(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		want   []string
	}{
		{
			name:   "triage categories",
			values: []string{"clinical_urgent", "clinical_routine", "admin", "spam"},
			want:   []string{`"\"clinical_urgent\""`, `"\"clinical_routine\""`, `"\"admin\""`, `"\"spam\""`},
		},
		{
			name:   "verifier verdicts",
			values: []string{"approve", "reject", "defer"},
			want:   []string{`"\"approve\""`, `"\"reject\""`, `"\"defer\""`},
		},
		{
			name:   "single value still alternation",
			values: []string{"yes"},
			want:   []string{`"\"yes\""`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := GBNFEnum(tc.values)
			if g.Format != GrammarGBNF {
				t.Fatalf("format=%q want %q", g.Format, GrammarGBNF)
			}
			if !strings.HasPrefix(g.Body, "root ::= ") {
				t.Errorf("body must start with root rule, got:\n%s", g.Body)
			}
			for _, want := range tc.want {
				if !strings.Contains(g.Body, want) {
					t.Errorf("body missing literal %q:\n%s", want, g.Body)
				}
			}
			// One alternation token per value minus one (n-1 pipes).
			gotPipes := strings.Count(g.Body, " | ")
			if gotPipes != len(tc.values)-1 {
				t.Errorf("expected %d ' | ' separators, got %d", len(tc.values)-1, gotPipes)
			}
		})
	}
}

func TestGBNFEnumPanicsOnEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("GBNFEnum([]) should panic")
		}
	}()
	_ = GBNFEnum(nil)
}

func TestGBNFEnumEscapesQuotesAndBackslashes(t *testing.T) {
	g := GBNFEnum([]string{`a"b`, `c\d`})
	// Value `a"b` becomes literal `"\"a\"b\""` inside the body — i.e. a
	// GBNF double-quoted string whose contents are a, \", b.
	if !strings.Contains(g.Body, `"\"a\"b\""`) {
		t.Errorf("expected escaped quote literal in body:\n%s", g.Body)
	}
	// Value `c\d` becomes literal `"\"c\\d\""` — the backslash is doubled.
	if !strings.Contains(g.Body, `"\"c\\d\""`) {
		t.Errorf("expected escaped backslash literal in body:\n%s", g.Body)
	}
}

func TestGBNFRegex(t *testing.T) {
	g := GBNFRegex(`[A-Z][0-9]+`)
	if g.Format != GrammarGBNF {
		t.Fatalf("format=%q", g.Format)
	}
	if !strings.Contains(g.Body, "root ::= [A-Z][0-9]+") {
		t.Errorf("body missing pattern:\n%s", g.Body)
	}
}

// TestProviderWarnsOnceWhenGrammarUnsupported pokes the warn-once helper
// indirectly through the mock and the warn-tracker reset hook.
func TestProviderWarnsOnceWhenGrammarUnsupported(t *testing.T) {
	resetGrammarWarnSeenForTest()

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	warnGrammarUnsupported("anthropic")
	warnGrammarUnsupported("anthropic")
	warnGrammarUnsupported("openai")

	out := buf.String()
	if strings.Count(out, "anthropic: provider does not support grammar") != 1 {
		t.Errorf("expected exactly one anthropic warning, got:\n%s", out)
	}
	if !strings.Contains(out, "openai: provider does not support grammar") {
		t.Errorf("expected openai warning, got:\n%s", out)
	}
}

// TestRequestGrammarRoundTripsThroughMock confirms the new field on Request
// is preserved end-to-end. The mock records the request verbatim, so this is
// also our contract test for "the agent set it, the provider can see it".
func TestRequestGrammarRoundTripsThroughMock(t *testing.T) {
	spec := GBNFEnum([]string{"approve", "reject"})
	m := &Mock{Resp: &Response{Content: `"approve"`}}
	_, err := m.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "decide"}},
		Grammar:  &spec,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(m.Calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(m.Calls))
	}
	got := m.Calls[0].Grammar
	if got == nil {
		t.Fatalf("Grammar was lost in Mock.Complete")
	}
	if got.Format != GrammarGBNF {
		t.Errorf("Format=%q want %q", got.Format, GrammarGBNF)
	}
	if !strings.Contains(got.Body, `"\"approve\""`) {
		t.Errorf("body missing approve literal:\n%s", got.Body)
	}
}

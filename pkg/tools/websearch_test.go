package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ddgStubResponse is a minimal DuckDuckGo JSON payload used by the mock server.
const ddgStubResponse = `{
  "RelatedTopics": [
    {
      "Text": "Go 1.22 Release Notes - Range over integers, fixes to loop variable capture.",
      "FirstURL": "https://tip.golang.org/doc/go1.22"
    },
    {
      "Text": "Go 1.21 Release Notes - New built-in functions min, max, clear.",
      "FirstURL": "https://tip.golang.org/doc/go1.21"
    },
    {
      "Text": "The Go Programming Language - Official homepage.",
      "FirstURL": "https://go.dev"
    }
  ]
}`

// newMockDDGServer returns an httptest.Server that responds with body to every
// request. The caller must call Close() when done.
func newMockDDGServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
}

// newTestWebSearchTool returns a WebSearchTool whose HTTP requests go to srv.
func newTestWebSearchTool(srv *httptest.Server) *WebSearchTool {
	return &WebSearchTool{
		httpClient: srv.Client(),
	}
}

// invokeWebSearch is a helper that calls Invoke and decodes results.
func invokeWebSearch(t *testing.T, tool *WebSearchTool, args string) []webSearchResult {
	t.Helper()
	raw, err := tool.Invoke(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var results []webSearchResult
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatalf("decode results: %v\nbody=%s", err, string(raw))
	}
	return results
}

// TestWebSearch_Interface verifies WebSearchTool satisfies the Tool interface
// at compile time (the assertion itself acts as the test).
func TestWebSearch_Interface(t *testing.T) {
	var _ Tool = &WebSearchTool{}
}

// TestWebSearch_NameDescriptionSchema verifies the static metadata.
func TestWebSearch_NameDescriptionSchema(t *testing.T) {
	tool := &WebSearchTool{}
	if tool.Name() != "web_search" {
		t.Fatalf("Name()=%q, want web_search", tool.Name())
	}
	if tool.Description() == "" {
		t.Fatal("Description() is empty")
	}
	var schema map[string]any
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatalf("Schema() is not valid JSON: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("Schema missing 'properties'")
	}
	if _, hasQuery := props["query"]; !hasQuery {
		t.Fatal("Schema missing 'query' property")
	}
	if _, hasMax := props["max_results"]; !hasMax {
		t.Fatal("Schema missing 'max_results' property")
	}
}

// TestWebSearch_BasicResults verifies results are parsed and returned correctly.
func TestWebSearch_BasicResults(t *testing.T) {
	srv := newMockDDGServer(t, http.StatusOK, ddgStubResponse)
	defer srv.Close()

	tool := newTestWebSearchTool(srv)
	// Redirect calls to our mock by overriding the API URL via the httpClient
	// already pointed at the test server; we need to make the tool call the
	// test server URL instead of api.duckduckgo.com. We do this by patching
	// the search method's URL via an override. Since the tool calls ddgAPIBase
	// directly we swap the base URL for the test by wrapping the client with a
	// redirecting transport.
	tool.httpClient = newRedirectClient(srv.URL)

	results := invokeWebSearch(t, tool, `{"query":"Go programming language"}`)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	r0 := results[0]
	if r0.URL != "https://tip.golang.org/doc/go1.22" {
		t.Fatalf("result[0].URL=%q, want https://tip.golang.org/doc/go1.22", r0.URL)
	}
	if r0.Title == "" {
		t.Fatal("result[0].Title is empty")
	}
	if r0.Snippet == "" {
		t.Fatal("result[0].Snippet is empty")
	}
}

// TestWebSearch_MaxResults verifies the max_results cap is honoured.
func TestWebSearch_MaxResults(t *testing.T) {
	srv := newMockDDGServer(t, http.StatusOK, ddgStubResponse)
	defer srv.Close()

	tool := &WebSearchTool{httpClient: newRedirectClient(srv.URL)}
	results := invokeWebSearch(t, tool, `{"query":"Go","max_results":2}`)
	if len(results) != 2 {
		t.Fatalf("expected 2 results (max_results=2), got %d", len(results))
	}
}

// TestWebSearch_DefaultMaxResults verifies omitting max_results defaults to 5.
func TestWebSearch_DefaultMaxResults(t *testing.T) {
	// Build a stub with 10 topics so we can observe the default cap in action.
	topics := make([]string, 10)
	for i := range topics {
		topics[i] = `{"Text":"Item - description","FirstURL":"https://example.com"}`
	}
	body := `{"RelatedTopics":[` + strings.Join(topics, ",") + `]}`

	srv := newMockDDGServer(t, http.StatusOK, body)
	defer srv.Close()

	tool := &WebSearchTool{httpClient: newRedirectClient(srv.URL)}
	results := invokeWebSearch(t, tool, `{"query":"test"}`)
	if len(results) != 5 {
		t.Fatalf("expected default 5 results, got %d", len(results))
	}
}

// TestWebSearch_EmptyQueryErrors verifies an empty query is rejected.
func TestWebSearch_EmptyQueryErrors(t *testing.T) {
	tool := &WebSearchTool{}
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":""}`))
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "query is empty") {
		t.Fatalf("error=%q, want mention of 'query is empty'", err.Error())
	}
}

// TestWebSearch_BadJSONErrors verifies malformed args return an error.
func TestWebSearch_BadJSONErrors(t *testing.T) {
	tool := &WebSearchTool{}
	_, err := tool.Invoke(context.Background(), json.RawMessage("not json"))
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode arguments") {
		t.Fatalf("error=%q, want decode mention", err.Error())
	}
}

// TestWebSearch_NonOKStatusErrors verifies non-200 HTTP responses are handled.
func TestWebSearch_NonOKStatusErrors(t *testing.T) {
	srv := newMockDDGServer(t, http.StatusServiceUnavailable, `{}`)
	defer srv.Close()

	tool := &WebSearchTool{httpClient: newRedirectClient(srv.URL)}
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"test"}`))
	if err == nil {
		t.Fatal("expected error on non-200 status")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("error=%q, want status 503 mention", err.Error())
	}
}

// TestWebSearch_EmptyTopicsReturnsEmpty verifies empty RelatedTopics produces [].
func TestWebSearch_EmptyTopicsReturnsEmpty(t *testing.T) {
	srv := newMockDDGServer(t, http.StatusOK, `{"RelatedTopics":[]}`)
	defer srv.Close()

	tool := &WebSearchTool{httpClient: newRedirectClient(srv.URL)}
	results := invokeWebSearch(t, tool, `{"query":"nothing"}`)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty RelatedTopics, got %d", len(results))
	}
}

// TestWebSearch_SubgroupTopicsFlattened verifies nested topic groups are expanded.
func TestWebSearch_SubgroupTopicsFlattened(t *testing.T) {
	body := `{
  "RelatedTopics": [
    {
      "Name": "A category",
      "Topics": [
        {"Text": "Sub A - Detail A.","FirstURL":"https://example.com/a"},
        {"Text": "Sub B - Detail B.","FirstURL":"https://example.com/b"}
      ]
    },
    {
      "Text": "Top-level - Direct result.",
      "FirstURL": "https://example.com/top"
    }
  ]
}`
	srv := newMockDDGServer(t, http.StatusOK, body)
	defer srv.Close()

	tool := &WebSearchTool{httpClient: newRedirectClient(srv.URL)}
	results := invokeWebSearch(t, tool, `{"query":"test","max_results":5}`)
	if len(results) != 3 {
		t.Fatalf("expected 3 results after flattening, got %d", len(results))
	}
	urls := map[string]bool{}
	for _, r := range results {
		urls[r.URL] = true
	}
	for _, want := range []string{"https://example.com/a", "https://example.com/b", "https://example.com/top"} {
		if !urls[want] {
			t.Errorf("missing expected URL %q in results", want)
		}
	}
}

// TestWebSearch_SplitTitleSnippet unit-tests the helper directly.
func TestWebSearch_SplitTitleSnippet(t *testing.T) {
	cases := []struct {
		text    string
		wantT   string
		wantS   string
	}{
		{"Title - Snippet here.", "Title", "Snippet here."},
		{"No separator at all", "", "No separator at all"},
		{"A - B - C", "A", "B - C"},
		{"  Spaced - Result  ", "Spaced", "Result"},
	}
	for _, tc := range cases {
		gotT, gotS := splitTitleSnippet(tc.text)
		if gotT != tc.wantT {
			t.Errorf("splitTitleSnippet(%q) title=%q, want %q", tc.text, gotT, tc.wantT)
		}
		if gotS != tc.wantS {
			t.Errorf("splitTitleSnippet(%q) snippet=%q, want %q", tc.text, gotS, tc.wantS)
		}
	}
}

// ---- helpers ----

// redirectTransport rewrites every request's host to targetBase so the tool's
// hardcoded ddgAPIBase calls land on the test server instead.
type redirectTransport struct {
	base      *http.Transport
	targetURL string
}

func (rt *redirectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Clone the request and replace the host.
	r2 := r.Clone(r.Context())
	r2.URL.Scheme = "http"
	r2.URL.Host = strings.TrimPrefix(rt.targetURL, "http://")
	return rt.base.RoundTrip(r2)
}

// newRedirectClient returns an *http.Client that forwards all requests to
// targetURL, regardless of what host the tool originally addressed.
func newRedirectClient(targetURL string) *http.Client {
	return &http.Client{
		Transport: &redirectTransport{
			base:      http.DefaultTransport.(*http.Transport).Clone(),
			targetURL: targetURL,
		},
	}
}

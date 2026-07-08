package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WebSearchTool searches the web using the DuckDuckGo Instant Answers API
// (no API key required) and returns a list of results with titles, snippets,
// and URLs. The tool is intentionally read-only and safe to call concurrently.
//
// Input shape (JSON):
//
//	{"query":"Go 1.22 features","max_results":5}
//
// Output shape (JSON):
//
//	[
//	  {"title":"Go 1.22 Release Notes","snippet":"...","url":"https://tip.golang.org/doc/go1.22"},
//	  ...
//	]
//
// The optional SearchAPIKey field is reserved for future provider swaps
// (e.g. Brave Search, Bing). When empty the tool uses DuckDuckGo.
type WebSearchTool struct {
	// SearchAPIKey is optional. When non-empty it is available for callers
	// that want to swap to an API-key-gated provider in a subtype. The
	// DuckDuckGo path (default) ignores it.
	SearchAPIKey string

	// httpClient allows test injection of a custom *http.Client. When nil
	// the tool constructs one with a 10-second timeout.
	httpClient *http.Client
}

// Name implements Tool.
func (WebSearchTool) Name() string { return "web_search" }

// Description implements Tool.
func (WebSearchTool) Description() string {
	return "Search the web for current information. Returns top results with titles, snippets, and URLs."
}

// Schema implements Tool.
func (WebSearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "The search query string."
    },
    "max_results": {
      "type": "integer",
      "description": "Maximum number of results to return (default 5, max 20).",
      "default": 5,
      "minimum": 1,
      "maximum": 20
    }
  },
  "required": ["query"],
  "additionalProperties": false
}`)
}

// webSearchInput mirrors the JSON shape Invoke accepts.
type webSearchInput struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
}

// webSearchResult is one entry in the output array.
type webSearchResult struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	URL     string `json:"url"`
}

// duckduckgoResponse is the minimal subset of the DuckDuckGo Instant Answers
// API response that the tool consumes. The full schema contains many more
// fields; we only need RelatedTopics for web-search-style results.
type duckduckgoResponse struct {
	RelatedTopics []duckduckgoTopic `json:"RelatedTopics"`
}

// duckduckgoTopic is one entry in RelatedTopics. Items that are sub-groups
// (rather than individual results) have a non-empty Name field and an inner
// Topics slice; we skip those and only process leaf items.
type duckduckgoTopic struct {
	Text     string `json:"Text"`
	FirstURL string `json:"FirstURL"`
	// Name is non-empty for topic groups; we skip those entries.
	Name   string            `json:"Name"`
	Topics []duckduckgoTopic `json:"Topics"`
}

const (
	ddgAPIBase        = "https://api.duckduckgo.com/"
	ddgTimeout        = 10 * time.Second
	defaultMaxResults = 5
)

// client returns the HTTP client to use, constructing one if needed.
func (t *WebSearchTool) client() *http.Client {
	if t.httpClient != nil {
		return t.httpClient
	}
	return &http.Client{Timeout: ddgTimeout}
}

// Invoke implements Tool.
func (t *WebSearchTool) Invoke(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("web_search: empty arguments")
	}
	var in webSearchInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("web_search: decode arguments: %w", err)
	}
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return nil, fmt.Errorf("web_search: query is empty")
	}
	maxResults := in.MaxResults
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}
	if maxResults > 20 {
		maxResults = 20
	}

	results, err := t.search(ctx, q, maxResults)
	if err != nil {
		return nil, fmt.Errorf("web_search: %w", err)
	}
	return json.Marshal(results)
}

// search performs the DuckDuckGo query and parses the response.
func (t *WebSearchTool) search(ctx context.Context, query string, maxResults int) ([]webSearchResult, error) {
	apiURL := ddgAPIBase + "?q=" + url.QueryEscape(query) + "&format=json&no_html=1&no_redirect=1"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "bodh-websearch/1.0")

	resp, err := t.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from DuckDuckGo API", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var ddgResp duckduckgoResponse
	if err := json.Unmarshal(body, &ddgResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return extractResults(ddgResp.RelatedTopics, maxResults), nil
}

// extractResults flattens the topic tree into a capped slice of results.
// Sub-group topics (Name != "") are expanded recursively.
func extractResults(topics []duckduckgoTopic, max int) []webSearchResult {
	var out []webSearchResult
	for _, topic := range topics {
		if len(out) >= max {
			break
		}
		// Sub-group: recurse into inner Topics.
		if topic.Name != "" {
			inner := extractResults(topic.Topics, max-len(out))
			out = append(out, inner...)
			continue
		}
		// Leaf result: requires at least a URL.
		if topic.FirstURL == "" {
			continue
		}
		title, snippet := splitTitleSnippet(topic.Text)
		out = append(out, webSearchResult{
			Title:   title,
			Snippet: snippet,
			URL:     topic.FirstURL,
		})
	}
	return out
}

// splitTitleSnippet splits DuckDuckGo's Text field into a title and snippet.
// DuckDuckGo returns a single Text string that typically looks like:
//
//	"Page Title - Short description of the page."
//
// We treat the portion before the first " - " as the title and the rest as
// the snippet. When no separator is present the full text becomes the snippet
// and the URL path fragment is used as the title.
func splitTitleSnippet(text string) (title, snippet string) {
	text = strings.TrimSpace(text)
	if idx := strings.Index(text, " - "); idx >= 0 {
		return strings.TrimSpace(text[:idx]), strings.TrimSpace(text[idx+3:])
	}
	return "", text
}

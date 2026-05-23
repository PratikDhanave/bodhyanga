package llm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// sampleResp builds a Response with non-empty fields so deep-copy bugs and
// JSON round-trip bugs both surface in equality checks.
func sampleResp(content string) *Response {
	return &Response{
		Content:      content,
		InputTokens:  11,
		OutputTokens: 7,
		CachedTokens: 3,
		Model:        "mock-model",
		Provider:     "mock",
		StopReason:   "end_turn",
		ToolCalls: []ToolCall{{
			Name:      "lookup",
			Arguments: json.RawMessage(`{"q":"x"}`),
		}},
	}
}

func TestMemoryCache_PutGetRoundTrip(t *testing.T) {
	c := NewMemoryCache(10)
	ctx := context.Background()
	want := sampleResp("hello")
	c.Put(ctx, "k1", want)

	got, ok := c.Get(ctx, "k1")
	if !ok {
		t.Fatalf("expected hit")
	}
	if got.Content != "hello" || got.InputTokens != 11 {
		t.Fatalf("round-trip lost data: %+v", got)
	}

	// Mutate the returned value — the cached copy must be unaffected.
	got.Content = "MUTATED"
	got.ToolCalls[0].Name = "MUTATED"
	if len(got.ToolCalls[0].Arguments) > 0 {
		got.ToolCalls[0].Arguments[0] = 0xff
	}

	got2, ok := c.Get(ctx, "k1")
	if !ok {
		t.Fatalf("expected hit on second Get")
	}
	if got2.Content != "hello" {
		t.Fatalf("cache poisoned: %q", got2.Content)
	}
	if got2.ToolCalls[0].Name != "lookup" {
		t.Fatalf("tool name poisoned: %q", got2.ToolCalls[0].Name)
	}
	if string(got2.ToolCalls[0].Arguments) != `{"q":"x"}` {
		t.Fatalf("args poisoned: %s", got2.ToolCalls[0].Arguments)
	}
}

func TestMemoryCache_LRUEvictsOldest(t *testing.T) {
	c := NewMemoryCache(3)
	ctx := context.Background()
	c.Put(ctx, "a", sampleResp("a"))
	c.Put(ctx, "b", sampleResp("b"))
	c.Put(ctx, "c", sampleResp("c"))
	// Touch "a" so it becomes most-recent; "b" should now be the oldest.
	if _, ok := c.Get(ctx, "a"); !ok {
		t.Fatalf("expected a to be present")
	}
	c.Put(ctx, "d", sampleResp("d"))

	if _, ok := c.Get(ctx, "b"); ok {
		t.Fatalf("expected b to be evicted (least recently used)")
	}
	for _, k := range []string{"a", "c", "d"} {
		if _, ok := c.Get(ctx, k); !ok {
			t.Fatalf("expected %s to remain", k)
		}
	}
}

func TestMemoryCache_UnboundedWhenMaxZero(t *testing.T) {
	c := NewMemoryCache(0)
	ctx := context.Background()
	const n = 1000
	for i := 0; i < n; i++ {
		key := "k" + itoa(i)
		c.Put(ctx, key, sampleResp(key))
	}
	for i := 0; i < n; i++ {
		key := "k" + itoa(i)
		if _, ok := c.Get(ctx, key); !ok {
			t.Fatalf("expected %s to survive in unbounded cache", key)
		}
	}
}

func TestCacheKey_StableAcrossCalls(t *testing.T) {
	req := Request{
		System:      "be terse",
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens:   16,
		Temperature: 0.2,
		JSONSchema:  &JSONSchema{Name: "emit", Schema: json.RawMessage(`{"type":"object"}`)},
		CacheSystem: true,
	}
	k1 := CacheKey("mock", "m1", req)
	k2 := CacheKey("mock", "m1", req)
	if k1 != k2 {
		t.Fatalf("CacheKey not stable: %s vs %s", k1, k2)
	}
}

func TestCacheKey_VariesWithRequest(t *testing.T) {
	base := Request{
		System:    "s",
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens: 16,
	}
	baseKey := CacheKey("mock", "m1", base)

	// Different temperature.
	r := base
	r.Temperature = 0.7
	if CacheKey("mock", "m1", r) == baseKey {
		t.Fatalf("temperature should vary key")
	}

	// Different message text.
	r = base
	r.Messages = []Message{{Role: RoleUser, Content: "different"}}
	if CacheKey("mock", "m1", r) == baseKey {
		t.Fatalf("message content should vary key")
	}

	// Different JSONSchema.
	r = base
	r.JSONSchema = &JSONSchema{Name: "emit", Schema: json.RawMessage(`{"type":"object"}`)}
	if CacheKey("mock", "m1", r) == baseKey {
		t.Fatalf("JSON schema should vary key")
	}

	// Different image bytes (vision case).
	r = base
	r.Messages = []Message{{
		Role:    RoleUser,
		Content: "hi",
		Images:  []Image{{MediaType: "image/png", Data: []byte{0x01, 0x02, 0x03}}},
	}}
	withImg := CacheKey("mock", "m1", r)
	r.Messages[0].Images[0].Data = []byte{0xff, 0xee, 0xdd}
	withImg2 := CacheKey("mock", "m1", r)
	if withImg == withImg2 {
		t.Fatalf("different image bytes should vary key")
	}

	// Different model.
	if CacheKey("mock", "m2", base) == baseKey {
		t.Fatalf("model should vary key")
	}

	// Different provider.
	if CacheKey("other", "m1", base) == baseKey {
		t.Fatalf("provider should vary key")
	}
}

func TestCachedClient_HitsAvoidUnderlyingCall(t *testing.T) {
	mock := &Mock{Resp: sampleResp("ok")}
	cached := NewCached(mock, NewMemoryCache(8))
	ctx := context.Background()

	req := Request{
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens: 16,
	}
	for i := 0; i < 3; i++ {
		got, err := cached.Complete(ctx, req)
		if err != nil {
			t.Fatalf("Complete %d: %v", i, err)
		}
		if got.Content != "ok" {
			t.Fatalf("content: %s", got.Content)
		}
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 underlying call, got %d", len(mock.Calls))
	}
}

func TestCachedClient_ErrorsNotCached(t *testing.T) {
	mock := &Mock{Err: errors.New("boom")}
	cached := NewCached(mock, NewMemoryCache(8))
	ctx := context.Background()
	req := Request{
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens: 16,
	}
	if _, err := cached.Complete(ctx, req); err == nil {
		t.Fatalf("expected error")
	}
	if _, err := cached.Complete(ctx, req); err == nil {
		t.Fatalf("expected error on second call too")
	}
	if len(mock.Calls) != 2 {
		t.Fatalf("expected 2 underlying calls (errors should not be cached), got %d", len(mock.Calls))
	}
}

func TestFileCache_PutGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, err := NewFileCache(dir, 10)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	ctx := context.Background()
	want := sampleResp("hi")
	c.Put(ctx, "k1", want)

	got, ok := c.Get(ctx, "k1")
	if !ok {
		t.Fatalf("expected hit")
	}
	if got.Content != "hi" || got.InputTokens != 11 || got.OutputTokens != 7 || got.CachedTokens != 3 {
		t.Fatalf("round-trip lost data: %+v", got)
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "lookup" {
		t.Fatalf("tool calls lost: %+v", got.ToolCalls)
	}
}

func TestFileCache_LRUByMTime(t *testing.T) {
	dir := t.TempDir()
	c, err := NewFileCache(dir, 3)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	ctx := context.Background()
	keys := []string{"a", "b", "c", "d"}
	for _, k := range keys {
		c.Put(ctx, k, sampleResp(k))
		// Ensure mtimes differ across filesystems with second-resolution mtime.
		time.Sleep(10 * time.Millisecond)
	}

	// "a" should have been pruned as the oldest.
	if _, ok := c.Get(ctx, "a"); ok {
		t.Fatalf("expected oldest entry a to be pruned")
	}
	for _, k := range []string{"b", "c", "d"} {
		if _, ok := c.Get(ctx, k); !ok {
			t.Fatalf("expected %s to remain", k)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	jsonCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			jsonCount++
		}
	}
	if jsonCount != 3 {
		t.Fatalf("expected 3 json files after prune, got %d", jsonCount)
	}
}

func TestFileCache_SurvivesProcessRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	{
		c, err := NewFileCache(dir, 10)
		if err != nil {
			t.Fatalf("NewFileCache: %v", err)
		}
		c.Put(ctx, "persist", sampleResp("persisted"))
	}
	// Fresh instance against the same directory.
	c2, err := NewFileCache(dir, 10)
	if err != nil {
		t.Fatalf("NewFileCache(reopen): %v", err)
	}
	got, ok := c2.Get(ctx, "persist")
	if !ok {
		t.Fatalf("expected entry to survive restart")
	}
	if got.Content != "persisted" {
		t.Fatalf("content: %s", got.Content)
	}
}

// itoa avoids pulling in strconv just for tests — keeps the test file
// dependency-free.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

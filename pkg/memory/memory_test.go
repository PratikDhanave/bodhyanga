package memory

import (
	"fmt"
	"sync"
	"testing"
)

func TestKV_PutGetRoundTrip(t *testing.T) {
	kv := NewInMemoryKV()

	kv.Set("k1", "v1")
	got, ok := kv.Get("k1")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if got != "v1" {
		t.Fatalf("got %q, want %q", got, "v1")
	}
}

func TestKV_GetUnknownKeyReturnsErrOrZero(t *testing.T) {
	kv := NewInMemoryKV()

	got, ok := kv.Get("missing")
	if ok {
		t.Fatalf("expected ok=false for missing key, got value %q", got)
	}
	if got != "" {
		t.Fatalf("expected zero value, got %q", got)
	}
}

func TestKV_DeleteRemovesKey(t *testing.T) {
	kv := NewInMemoryKV()

	kv.Set("k1", "v1")
	if _, ok := kv.Get("k1"); !ok {
		t.Fatal("setup: expected key to exist before delete")
	}

	kv.Delete("k1")

	if got, ok := kv.Get("k1"); ok {
		t.Fatalf("expected key absent after delete, got %q", got)
	}

	// Deleting a missing key should be a no-op.
	kv.Delete("never-existed")
}

func TestKV_KeysReturnsAllKeysSorted(t *testing.T) {
	// InMemoryKV does not expose a Keys() method; skip until it does.
	t.Skip("InMemoryKV has no Keys() method; see pkg/memory/memory.go")
}

func TestKV_ConcurrentPutsAreSafe(t *testing.T) {
	kv := NewInMemoryKV()

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			kv.Set(fmt.Sprintf("k-%d", i), fmt.Sprintf("v-%d", i))
		}(i)
	}
	wg.Wait()

	// Confirm every key landed and carries the right value.
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("k-%d", i)
		got, ok := kv.Get(key)
		if !ok {
			t.Fatalf("missing key %q after concurrent Puts", key)
		}
		if want := fmt.Sprintf("v-%d", i); got != want {
			t.Fatalf("key %q: got %q want %q", key, got, want)
		}
	}
}

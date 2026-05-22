package persistence

import (
	"path/filepath"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/eval"
)

func TestFileStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(eval.InteractionRecord{ID: "r1", Success: true,
		Metrics: map[string]float64{"accuracy": 1}}); err != nil {
		t.Fatal(err)
	}
	s.Set("hello", "world")

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.List(); len(got) != 1 || got[0].ID != "r1" {
		t.Fatalf("records not persisted: %+v", got)
	}
	if v, ok := s2.Get("hello"); !ok || v != "world" {
		t.Fatalf("kv not persisted: ok=%v v=%q", ok, v)
	}
}

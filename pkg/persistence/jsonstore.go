// Package persistence provides a JSON-file backed implementation of
// eval.Store and memory.KeyValueStore. The aim is to survive process restarts
// for the demo without pulling in a real database.
package persistence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/PratikDhanave/bodh/pkg/eval"
)

// FileStore persists eval.InteractionRecord and a flat key-value map under a
// single JSON file. It loads on open and flushes on every write.
type FileStore struct {
	mu    sync.Mutex
	path  string
	state fileState
}

type fileState struct {
	Records []eval.InteractionRecord `json:"records,omitempty"`
	KV      map[string]string        `json:"kv,omitempty"`
}

// Open loads (or creates) a FileStore at the given path.
func Open(path string) (*FileStore, error) {
	s := &FileStore{path: path, state: fileState{KV: map[string]string{}}}
	if path == "" {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.state); err != nil {
		return nil, err
	}
	if s.state.KV == nil {
		s.state.KV = map[string]string{}
	}
	return s, nil
}

// Save appends an eval record and flushes.
func (s *FileStore) Save(r eval.InteractionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Records = append(s.state.Records, r)
	return s.flushLocked()
}

// List returns a copy of all eval records.
func (s *FileStore) List() []eval.InteractionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]eval.InteractionRecord, len(s.state.Records))
	copy(out, s.state.Records)
	return out
}

// Get implements memory.KeyValueStore.
func (s *FileStore) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.state.KV[key]
	return v, ok
}

// Set implements memory.KeyValueStore (write-through).
func (s *FileStore) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.KV[key] = value
	_ = s.flushLocked()
}

// Delete implements memory.KeyValueStore (write-through).
func (s *FileStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.KV, key)
	_ = s.flushLocked()
}

func (s *FileStore) flushLocked() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o644)
}

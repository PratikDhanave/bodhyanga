package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/agents"
)

// Manifest is the bodhyanga-maf v2 case index (no max_cost_usd — accuracy-only).
type Manifest struct {
	Version     int     `json:"version"`
	Description string  `json:"description"`
	Cases       []Entry `json:"cases"`
}

// Entry is one case: a fixture path (manifest-relative) + its gold label.
type Entry struct {
	ID            string   `json:"id"`
	Path          string   `json:"path"`
	GoldDiagnosis string   `json:"gold_diagnosis"`
	Tags          []string `json:"tags,omitempty"`
}

// Fixture is a loaded case ready to run.
type Fixture struct {
	ID   string
	Gold string
	Case agents.PatientCase
}

// LoadManifest reads a manifest and every fixture it names. Fixture paths
// resolve relative to the manifest file (mirroring bodhyanga's resolver). Fails fast
// on an empty suite, a missing path, or an unreadable/unparseable fixture — a
// silent gap would understate accuracy.
func LoadManifest(path string) ([]Fixture, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("bench: read manifest %q: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("bench: parse manifest %q: %w", path, err)
	}
	if len(m.Cases) == 0 {
		return nil, fmt.Errorf("bench: manifest %q has no cases", path)
	}
	dir := filepath.Dir(path)
	fixtures := make([]Fixture, 0, len(m.Cases))
	for i, e := range m.Cases {
		if e.Path == "" {
			return nil, fmt.Errorf("bench: manifest case %d (%q) has no path", i, e.ID)
		}
		fpath := e.Path
		if !filepath.IsAbs(fpath) {
			fpath = filepath.Join(dir, fpath)
		}
		fraw, err := os.ReadFile(fpath)
		if err != nil {
			return nil, fmt.Errorf("bench: read fixture %q: %w", fpath, err)
		}
		var pc agents.PatientCase
		if err := json.Unmarshal(fraw, &pc); err != nil {
			return nil, fmt.Errorf("bench: parse fixture %q: %w", fpath, err)
		}
		fixtures = append(fixtures, Fixture{ID: e.ID, Gold: e.GoldDiagnosis, Case: pc})
	}
	return fixtures, nil
}

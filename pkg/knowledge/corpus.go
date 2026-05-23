package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
)

// corpusFile is the on-disk shape consumed by LoadCorpus.
type corpusFile struct {
	// Notice / Comment fields are accepted but ignored. They let the JSON
	// carry a top-line disclaimer (e.g. "demo-synth, not a real guideline").
	Notice    string     `json:"_notice,omitempty"`
	Comment   string     `json:"_comment,omitempty"`
	Documents []Document `json:"documents"`
}

// LoadCorpus reads the JSON file at path and returns the contained documents.
// The file must match the schema described in doc.go:
//
//	{
//	  "_notice": "demo-synth, not a real guideline",
//	  "documents": [
//	    {"id": "...", "source": "...", "title": "...",
//	     "content": "...", "metadata": {...}}
//	  ]
//	}
//
// LoadCorpus does not embed or index the documents; pass the result to a
// Store's Index method.
func LoadCorpus(path string) ([]Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("knowledge: read corpus %q: %w", path, err)
	}
	var f corpusFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("knowledge: parse corpus %q: %w", path, err)
	}
	for i, d := range f.Documents {
		if d.ID == "" {
			return nil, fmt.Errorf("knowledge: corpus %q document at index %d has empty id", path, i)
		}
	}
	return f.Documents, nil
}

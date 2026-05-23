// corpus.go — JSON loader for the on-disk knowledge corpus.
//
// Overview:
//   LoadCorpus reads a JSON file shaped as {"_notice": "...",
//   "documents": [Document, ...]} and returns the document slice. The
//   loader does not embed or index — callers pass the result to a Store's
//   Index method. The optional _notice / _comment fields let the file carry
//   a top-line disclaimer (e.g. "demo-synth, not a real guideline"); both
//   are accepted but ignored by the loader. Every document must carry a
//   non-empty ID; the loader rejects the corpus otherwise.
//
// Key declarations:
//   - LoadCorpus(path) ([]Document, error)
//   - corpusFile: the on-disk wrapper struct
//
// See doc.go for package-level documentation.

package knowledge

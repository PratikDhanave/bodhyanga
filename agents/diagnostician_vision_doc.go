// diagnostician_vision.go — vision-augmented LLM diagnostician.
//
// Overview:
//   LLMDiagnosticianVision wraps an LLMDiagnostician and, when the incoming
//   message's Metadata contains an "image_path" entry, reads the referenced
//   file (PNG / JPEG) and attaches it as an inline image on the user message.
//   When no image path is set, it transparently delegates to the base agent.
//   On any image read / decode failure, it logs and falls through to the base
//   diagnostician unchanged — so RAG and rule-based fallback still work.
//   The agent's ID remains "diagnostician" so supervisor routing is unchanged.
//
// Key declarations:
//   - LLMDiagnosticianVision: struct embedding *LLMDiagnostician as Base
//   - LLMDiagnosticianVision.HandleMessage: agent entry point
//   - readImage / sniffMedia: PNG/JPEG MIME detection from magic bytes + ext
//
// See doc.go for package-level documentation.

package agents

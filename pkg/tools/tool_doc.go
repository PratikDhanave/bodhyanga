// tool.go — Tool interface and Registry.
//
// Overview:
//   Defines the Tool interface every clinical helper in this package
//   implements (Name/Description/Schema/Invoke) and the Registry that
//   bundles a set of Tools for an agent to advertise to its LLM. The
//   Registry's LLMTools() converts each Tool into the llm.Tool descriptor
//   the pkg/llm Request.Tools field consumes, so wiring the registry into a
//   diagnostician is a single field assignment.
//
// Key declarations:
//   - Tool: the interface implementations must satisfy
//   - Registry: name-keyed set of Tools
//   - NewRegistry(tools ...Tool) *Registry
//   - (*Registry).Get / List / LLMTools
//
// See doc.go for package-level documentation.

package tools

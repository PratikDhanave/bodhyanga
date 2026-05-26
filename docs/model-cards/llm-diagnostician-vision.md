# Model Card — `LLMDiagnosticianVision`

> **Card version:** 1.0.0 · **Last reviewed:** 2026-05-25 · **Author:** Bodh maintainers

## Model details

- **Variant name:** `LLMDiagnosticianVision`
- **Source:** [`agents/diagnostician_vision.go`](../../agents/diagnostician_vision.go)
- **Type:** Single-LLM-call agent with image attachment + deterministic fallback
- **Default LLM provider:** Anthropic (vision-capable: Sonnet, Opus) / OpenAI (`gpt-4o`, `gpt-4o-mini`) / Ollama (`llama3.2-vision`, `bakllava`, `llava:13b`)
- **Provider-configurable:** yes
- **License:** Apache-2.0 (Bodh) / provider terms (LLM)

## Intended use

- **Primary intended use:** Diagnose with an attached image. When `msg.Metadata["image_path"]` is set, the agent reads the file, sniffs PNG/JPEG, and attaches it as `Message.Images[0]` to the LLM request. Inherits all `LLMDiagnostician` behavior (RAG, fallback, citations) but adds image context.
- **Primary intended users:** Researchers experimenting with chest X-ray, ECG strip, skin lesion, fundus imagery, or similar inputs.
- **Out-of-scope uses:**
  - DICOM-format images (not yet supported — DICOMweb integration planned)
  - PHI-burned images (must be redacted before passing the path)
  - Standalone radiology interpretation (use a dedicated FDA-cleared radiology AI tool)

## Factors

- **Relevant conditions:** Any where image evidence helps (CAP, PE, fractures, skin findings). Empirically depends heavily on the underlying vision model.
- **Image formats:** PNG, JPEG. Sniffed from the file header; unsupported MIME types fall through to text-only with a logged warning.
- **Image size:** Provider-dependent. Anthropic caps at ~3.75 MP per image; OpenAI similar. Resize before passing if exceeding.

## Metrics

This variant is harder to bench because Bodh's fixture set doesn't yet include images. Recommended: hold-out a small fixture set with attached chest X-rays (synthetic / public datasets only — NEVER patient PHI) and measure per-fixture.

## Evaluation data

Not yet shipped. Recommended sources for non-PHI evaluation:

- Open chest X-ray datasets (NIH ChestX-ray14, MIMIC-CXR with DUA, etc.) — license terms permitting
- Synthetic radiology phantoms

Do NOT use patient-identifiable images in CI.

## Training data

Inherits the underlying vision LLM's training data. Refer to:

- Anthropic Claude vision data handling
- OpenAI GPT-4o vision data handling
- Ollama llama3.2-vision / bakllava / llava model cards

## Ethical considerations

- **Risk: PHI burned into image.** Mitigations: human SOP to redact image PHI before passing the path; defence in depth via `pkg/phi` redactor on the file path itself.
- **Risk: training-data demographic bias on imagery.** Vision LLMs have documented bias on Fitzpatrick skin types in dermatology imagery, lighter-skin training distributions, etc. Mitigations: red-team catalogue should add image-specific adversarial cases; equity audit pipeline planned.
- **Risk: hallucinated image findings.** Mitigations: same fallback contract, same hallucination check, but the check is harder for image-derived rationales. Recommended HITL gate on every image-attached diagnosis.
- **Risk: image-data exfiltration.** Mitigations: TLS to provider; provider BAA; no image content in audit events; image_path is logged only as a hash.

## Caveats and recommendations

- **Image attachment is best-effort.** If reading the file fails or the format is unsupported, the agent transparently falls through to text-only diagnosis. The case still finalizes.
- **HITL is strongly recommended for every image-attached diagnosis.** Radiology interpretation by LLM is the highest-risk inference path in Bodh.
- **Don't pass image_path to the LLM call body.** The path is read locally and the image bytes attached; the path itself never goes over the wire.
- **Local Ollama** is the right deployment for HIPAA-sensitive image diagnosis until vendor BAAs explicitly cover vision endpoints.

## References

- [agents/diagnostician_vision.go](../../agents/diagnostician_vision.go)
- [docs/agents.md § LLMDiagnosticianVision](../agents.md#llmdiagnosticianvision)

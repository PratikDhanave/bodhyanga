// diagnostician_vision_test.go — unit tests for LLMDiagnosticianVision.
//
// Overview:
//   Exercises the vision diagnostician across three paths: image attached
//   (PNG written to t.TempDir()), no image_path metadata (delegates to base),
//   and missing file (logs and still emits a proposal via the base path).
//
// Key declarations:
//   - writeTinyPNG: writes a valid 2x2 PNG so http.DetectContentType resolves
//     to image/png and the MIME sniff succeeds
//   - TestVisionAttachesImageWhenPathSet
//   - TestVisionFallsThroughWhenNoImagePath
//   - TestVisionHandlesMissingFileGracefully
//
// See doc.go for package-level documentation.

package agents

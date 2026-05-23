// panel_manager_test.go — unit tests for the panel manager agent.
//
// Overview:
//   Seeds a registry with diabetic and hypertensive patients with open gaps
//   and verifies that direct mode emits engagement_request per gap while
//   HITL mode wraps each gap in a review_request to human_review.
//
// Key declarations:
//   - seedPanelRegistry: builds a registry fixture with two gap patients.
//   - TestPanelManagerDirectMode: asserts panel_report + engagement_request routing.
//   - TestPanelManagerHITLMode: asserts review_request envelope for HITL.
//   - TestPriorityFromSeverity: exercises the severity->priority mapping.
//
// See doc.go for package-level documentation.

package agents

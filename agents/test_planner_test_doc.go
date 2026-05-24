// test_planner_test.go — unit tests for the diagnostic test planner.
//
// Overview:
//   Exercises the per-condition dispatch table introduced when SD-Bench
//   surfaced the "flat respiratory workup for every case" gap. Asserts that
//   chf / acs / sepsis / dka / cap each receive a condition-appropriate test
//   set, that unknown and empty conditions fall back to the legacy default
//   (backward compatibility), that ordered tests carry catalog costs, and
//   that simulated result strings vary with the condition.
//
// Key declarations:
//   - TestTestPlannerDispatchesWorkupByCondition: table-driven check that
//     state.Condition selects the right workup and that the outbound
//     message routes to cost_guardian.
//   - TestSimulatedResultsAreConditionAware: pins down that the simulator
//     emits condition-specific values for the markers the diagnostician
//     reads (BNP, troponin, lactate, ketones).
//   - equalUnordered: order-independent slice equality helper.
//
// See doc.go for package-level documentation.

package agents

# OPA unit tests for pkg/governance/rego/medical.rego.
# Run with:   make opa-test
#             opa test --v1-compatible pkg/governance/rego

package bodh.governance.medical_test

import data.bodh.governance.medical
import rego.v1

# =============================================================================
# case_id_result tests
# =============================================================================

test_case_id_non_clinical if {
	medical.case_id_result == {"allowed": true, "reason": "non-clinical message"} with input as {
		"type": "system",
		"metadata": null,
	}
}

test_case_id_clinical_null_metadata if {
	medical.case_id_result == {"allowed": false, "reason": "missing metadata with case_id"} with input as {
		"type": "presentation",
		"metadata": null,
	}
}

test_case_id_clinical_missing_key if {
	medical.case_id_result == {"allowed": false, "reason": "case_id required for clinical messages"} with input as {
		"type": "diagnosis",
		"metadata": {"tenant_id": "t1"},
	}
}

test_case_id_clinical_present if {
	medical.case_id_result == {"allowed": true, "reason": "case_id present"} with input as {
		"type": "presentation",
		"metadata": {"case_id": "case-001", "tenant_id": "t1"},
	}
}

test_case_id_all_clinical_types if {
	every t in {"presentation", "case_state", "ask_question", "order_test", "diagnosis", "verify_reasoning"} {
		r := medical.case_id_result with input as {
			"type": t,
			"metadata": {"case_id": "c1"},
		}
		r.allowed == true
	}
}

# =============================================================================
# diagnostic_cost_result tests
# =============================================================================

test_diagnostic_cost_not_order_test if {
	medical.diagnostic_cost_result.allowed == true with input as {
		"type": "presentation",
		"metadata": {"case_id": "c1"},
	}
}

test_diagnostic_cost_null_metadata if {
	medical.diagnostic_cost_result.allowed == true with input as {
		"type": "order_test",
		"metadata": null,
	}
}

test_diagnostic_cost_no_max_cost if {
	medical.diagnostic_cost_result == {"allowed": true, "reason": "no max_cost_usd constraint"} with input as {
		"type": "order_test",
		"metadata": {"case_id": "c1", "test_cost_usd": 50},
	}
}

test_diagnostic_cost_no_test_cost if {
	medical.diagnostic_cost_result == {"allowed": true, "reason": "no test_cost_usd in metadata"} with input as {
		"type": "order_test",
		"metadata": {"case_id": "c1", "max_cost_usd": 200},
	}
}

test_diagnostic_cost_within_budget if {
	medical.diagnostic_cost_result == {"allowed": true, "reason": "within diagnostic budget"} with input as {
		"type": "order_test",
		"metadata": {
			"case_id": "c1",
			"max_cost_usd": 200,
			"test_cost_usd": 50,
			"spent_usd": 100,
		},
	}
}

test_diagnostic_cost_exactly_at_budget if {
	medical.diagnostic_cost_result.allowed == true with input as {
		"type": "order_test",
		"metadata": {
			"case_id": "c1",
			"max_cost_usd": 200,
			"test_cost_usd": 100,
			"spent_usd": 100,
		},
	}
}

test_diagnostic_cost_exceeds_budget if {
	medical.diagnostic_cost_result == {"allowed": false, "reason": "test would exceed diagnostic budget"} with input as {
		"type": "order_test",
		"metadata": {
			"case_id": "c1",
			"max_cost_usd": 200,
			"test_cost_usd": 150,
			"spent_usd": 100,
		},
	}
}

test_diagnostic_cost_first_order_no_spent if {
	# spent_usd absent → treated as 0
	medical.diagnostic_cost_result.allowed == true with input as {
		"type": "order_test",
		"metadata": {
			"case_id": "c1",
			"max_cost_usd": 200,
			"test_cost_usd": 50,
		},
	}
}

# =============================================================================
# tenant_result tests
# =============================================================================

test_tenant_null_metadata if {
	medical.tenant_result == {"allowed": false, "reason": "tenant_id required: no metadata"} with input as {
		"type": "presentation",
		"metadata": null,
	}
}

test_tenant_missing_key if {
	medical.tenant_result == {"allowed": false, "reason": "tenant_id required"} with input as {
		"type": "presentation",
		"metadata": {"case_id": "c1"},
	}
}

test_tenant_empty_string if {
	medical.tenant_result == {"allowed": false, "reason": "tenant_id required"} with input as {
		"type": "presentation",
		"metadata": {"tenant_id": ""},
	}
}

test_tenant_present if {
	medical.tenant_result == {"allowed": true, "reason": "tenant_id present"} with input as {
		"type": "presentation",
		"metadata": {"tenant_id": "tenant-abc"},
	}
}

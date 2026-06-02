## Summary

<!-- 1-3 sentences: what changed and why -->

## Changes

<!-- Bulleted list of what was added/modified/removed -->

-

## Type of change

- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] New agent (follows the pattern in CONTRIBUTING.md § Adding an agent)
- [ ] New policy (follows the pattern in CONTRIBUTING.md § Adding a policy)
- [ ] Documentation update
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)

## Checklist

- [ ] `go build ./...` passes
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
- [ ] New code has tests
- [ ] Agent changes follow the [Agent contract](docs/agents.md) (4 methods, bus routing, HITL gating)
- [ ] Policy changes follow the [Governance pattern](docs/governance.md) (deny-first, PHI-free reason)
- [ ] No API keys, PHI, or secrets in committed code
- [ ] CHANGELOG.md updated (if user-facing change)

## PHI Safety

- [ ] No real patient data in test fixtures or examples
- [ ] Audit events use redacted reasons (no diagnosis/HPI/lab values)
- [ ] OTel spans use `sha256(caseID)[:8]` for `bodh.case_id`
- [ ] New tools/agents do not log PHI to stdout

## Test plan

<!-- How to verify this change works -->

```bash
# Example:
go test ./pkg/medical/... -run TestNewFeature -v
make lesson-03
```

## Screenshots

<!-- If frontend changes, include before/after screenshots -->

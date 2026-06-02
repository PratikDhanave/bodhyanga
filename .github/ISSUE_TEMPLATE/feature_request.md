---
name: Feature request
about: Suggest a new feature or enhancement
title: '[FEATURE] '
labels: enhancement
assignees: ''
---

## Problem

What problem does this solve? What's the use case?

## Proposed solution

Describe the feature you'd like. Be specific about:

- **Which package** it belongs in (e.g. `pkg/medical/`, `agents/`, `pkg/governance/`)
- **The interface** it would expose (function signatures, types)
- **How it integrates** with the existing MARA pipeline

## Alternatives considered

What other approaches did you consider? Why is this approach better?

## Design considerations

- [ ] Does this touch `pkg/protocol`, `pkg/agent`, `pkg/orchestration`, or `pkg/audit`?
  - If yes, please describe your approach first — these are load-bearing interfaces.
- [ ] Does this add a new external dependency?
  - If yes, can it be behind a build tag (like `otel` or `kronk`)?
- [ ] Does this involve PHI?
  - If yes, how is it redacted in audit/observability?
- [ ] Does this require HITL gating?
  - If yes, which decisions flow through `human_review`?

## Additional context

Reference any relevant:
- ROADMAP.md items
- docs/agent-interop.md proposals
- External specifications (HL7, FHIR, CDS Hooks, etc.)

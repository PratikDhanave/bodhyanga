# AI Impact Assessment (AIIA) — Template

> Fillable template for every clinically consequential change to Bodhyanga — new condition, new agent variant, new model swap, new policy. Cross-references AIGP Domain IV.B ("Perform key activities to assess the AI system") and GDPR Article 35 (DPIA for AI-based clinical decision support).
>
> Open as a draft PR description, fill in, copy the completed form into the PR body. The PR template (planned) will lift this structure as required sections.

---

## How to use

1. Open the PR that introduces the change.
2. Copy this template's section headers into the PR body.
3. Fill every section honestly. **Unknown is an acceptable answer; omitted is not.**
4. Reviewer checks the AIIA before merging.
5. After merge, the AIIA lives in the PR — searchable, linkable, audit-trail-bound.

---

## AIIA template

```markdown
## AI Impact Assessment

### 1. Change summary

[1-2 sentences. What is being introduced or changed? Anchor in a concrete clinical scenario.]

### 2. Intended use

- **Primary use case:** [...]
- **Primary users:** [clinician / nurse / care coordinator / operator / researcher]
- **Decision affected:** [diagnosis / care plan / outreach / refill / prior auth / discharge / ...]
- **Stakeholders impacted:** [patient / clinician / care team / payer / operator]

### 3. Out-of-scope uses

Enumerate at least three things this change should NOT be used for:

- [...]
- [...]
- [...]

### 4. Expected benefits (concrete + measurable)

- [...]
- [...]

For each, state how it will be measured (which bench metric, which SLI).

### 5. Potential harms (concrete + measurable)

Cover at least these categories:

- **Misdiagnosis / wrong recommendation:** [likelihood / severity / detection mechanism]
- **PHI exposure:** [risk vector / mitigation]
- **Bias / disparate impact:** [affected demographics / how detected]
- **Cost gaming / under-ordering:** [risk / mitigation]
- **Latency / availability degradation:** [risk / SLO]
- **Auditability gap:** [what becomes harder to reconstruct]

### 6. Mitigations

Map each potential harm to a specific Bodhyanga primitive that mitigates it:

| Harm | Mitigation |
|---|---|
| Misdiagnosis | HITL gate / rule-based fallback / reasoning_verifier |
| PHI exposure | `pkg/phi` redactor / opaque case IDs / audit field selection |
| Bias | red-team catalogue / equity audit (when wired) / bench stratification |
| Cost gaming | `cost_guardian` + `MaxDiagnosticCostPolicy` |
| Latency | LLM fallback contract / SLO alerting |
| Auditability | `pkg/audit` records every hop (PR #12) |

### 7. Residual risk

After mitigations, what risk remains? Be specific.

- [...]

### 8. Monitoring plan

| SLI | Target SLO | Alert threshold |
|---|---|---|
| [...] | [...] | [...] |
| [...] | [...] | [...] |

Cross-reference [docs/operations.md § SLI / SLO recommendations](operations.md#sli--slo-recommendations) for established SLOs.

### 9. Bench impact

| | Cases | Accuracy | Mean Cost | Pareto |
|---|---|---|---|---|
| before | [...] | [...] | [...] | [...] |
| after | [...] | [...] | [...] | [...] |

If accuracy or cost shifts significantly, justify. Update `data/cases/baseline.json` via `-update-baseline` only after the change is reviewed.

### 10. Regulatory mapping

Cross-reference the regulations this change touches:

- **HIPAA Privacy / Security / Breach Notification:** [which §, what control]
- **GDPR Article 22 (automated decision-making):** [HITL preserves non-solely-automated nature: yes/no]
- **21st Century Cures Act §3060 CDS carve-out:** [criterion 4 — independent review enabled: yes/no]
- **EU AI Act (if applicable):** [Article 9-15 controls touched]
- **Other:** [42 CFR Part 2, state laws, etc.]

### 11. Sunset criteria

When should this change be retired or replaced?

- [Specific metric thresholds, calendar deadlines, or dependent-system changes]

### 12. Reviewer sign-off

- [ ] Code reviewer: [name]
- [ ] Clinical SME (when applicable): [name]
- [ ] Compliance / privacy reviewer (when applicable): [name]
- [ ] AIIA reviewer (this form, separate from code review): [name]

### 13. Disclaimer

This change ships under Bodhyanga's research disclaimer. Not a medical device. Not approved for clinical use. The AIIA above describes the change for internal review; it is not a regulatory submission.
```

---

## When an AIIA is required

| Change type | AIIA required? |
|---|---|
| New condition (care plan, panel rules, diagnostic rule) | **Yes** |
| New agent variant (LLM-backed or rule-based) | **Yes** |
| New governance policy with deny semantics | **Yes** |
| LLM model swap (e.g. sonnet-4-6 → opus-5-0) | **Yes** |
| LLM provider swap | **Yes** |
| New HTTP endpoint that exposes clinical content | **Yes** |
| Threshold change in any existing rule | **Yes** (small AIIA — just the affected sections) |
| RAG corpus update | **Yes** |
| Pure refactor with no behavior change | No |
| Doc / typo / test-only PR | No |
| Bench fixture addition | **Yes** if it changes the baseline; **No** if tagged `coverage_gap` |

---

## Worked example — asthma support PR (#6)

The asthma care plan + panel rules PR (#6) added a new condition. The retrospective AIIA:

**1. Change summary:** Adds asthma as a first-class condition — care plan template, two panel rules, ACT score tracking on `PatientRecord`. Demonstrates the composition pattern without platform changes.

**2. Intended use:**
- Primary: panel management of asthma patients with controller-medication gap detection.
- Users: care coordinators and nurses running panel reviews.
- Decision affected: outreach decisions, care plan adjustments.
- Stakeholders: asthma patients (target population), clinicians (decision-makers).

**3. Out-of-scope uses:**
- Standalone asthma diagnosis (no diagnostic rule for asthma in `diagnostician.go`).
- Pediatric asthma management (thresholds are adult-based).
- Acute asthma exacerbation triage (separate clinical pathway).

**4. Expected benefits:**
- Detection of asthma patients without an inhaled corticosteroid (the most studied care gap). Measured by `no_controller_persistent_asthma` rule fires per panel run.
- Detection of patients overdue for ACT score (180-day window). Measured by `act_overdue` rule fires.

**5. Potential harms:**
- Mis-triage (asthma misclassified as something else): low risk; the rule path doesn't diagnose asthma.
- Over-outreach: false-positive outreach to patients who don't actually have a controller gap. Detection: clinician reroute rate.
- ACT threshold edge cases (180 days vs 181 days): low severity; mitigated by reviewer discretion.

**6. Mitigations:**

| Harm | Mitigation |
|---|---|
| Over-outreach | `panel_manager` output gated by HITL (outreach_review) when `HITL=true` |
| Edge-case thresholds | Inline comments labeling thresholds illustrative |
| PHI in panel report | Audit event field selection — no patient name / DOB in audit |

**7. Residual risk:** ACT threshold may need clinical SME review before any production deployment.

**8. Monitoring plan:** Rule-fire rate per panel run; reviewer reject rate on outreach_review.

**9. Bench impact:** N/A (no diagnostic rule added for asthma; bench numbers unchanged).

**10. Regulatory mapping:**
- HIPAA: minimum-necessary still respected (panel report carries no PHI beyond opaque IDs).
- GDPR Article 22: outreach is gated by HITL → not solely automated.
- 21st Century Cures Act §3060: criterion 4 satisfied via outreach_review.

**11. Sunset criteria:** When clinical guidelines change (likely 2-3 year horizon for GINA / asthma guidelines), revisit. When a specialty asthma diagnostician variant ships, this AIIA may need a re-review.

**12. Reviewer sign-off:** This was a small PR; code reviewer + AIIA reviewer could be the same person. For larger changes, separate sign-offs.

**13. Disclaimer:** Research reference, not clinical guideline.

---

## References

- IAPP AIGP Body of Knowledge Domain IV.B ("Perform key activities to assess the AI system")
- GDPR Article 35 (DPIA — Data Protection Impact Assessment)
- EU AI Act Articles 9-15 (high-risk system requirements)
- [docs/aigp-governance-mapping.md](aigp-governance-mapping.md) — full AIGP body-of-knowledge mapping
- [docs/operations.md](operations.md) — SLI / SLO reference

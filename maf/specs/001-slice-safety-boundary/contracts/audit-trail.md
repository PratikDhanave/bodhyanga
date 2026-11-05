# Contract: Audit Trail (per case)

The trail is bodhyanga `audit.Event` rows (schema owned by bodhyanga — this repo adds no
fields). For the canonical happy-path case (1 loop-back, approved review) the trail
MUST contain, in a causally coherent order:

| # | Kind | Decision | AgentID | Type |
|---|---|---|---|---|
| 1 | policy_decision | allow | intake | patient_case |
| 2 | message | — | intake | patient_case |
| 3 | policy_decision | allow | test_planner | diagnostic_query |
| 4 | message | — | test_planner | diagnostic_query |
| 5 | policy_decision | allow | diagnostician | workup |
| 6 | message | — | diagnostician | workup |
| 7–10 | (repeat 3–6 for the loop-back round) | | | |
| 11 | message | — | physician_review | review_requested |
| 12 | message | — | — | checkpoint |
| 13 | review_decision | approve | physician_review | — (ReviewerID set) |
| 14 | policy_decision | allow | care_plan | diagnosis_result |
| 15 | message | — | care_plan | diagnosis_result |
| 16 | message | — | — | case_finalized |

Deny-all variant: rows 1 (deny) only, then run halt — one `policy_decision/deny` per
attempted hop, zero `message` execution rows.

**BSP ordering (verified 2026-07-12)**: MAF executes in super-steps, so the trail
interleaves differently than the naive table: admission rows (written synchronously
inside the hop) appear in exact causal order, while the bridge's execution rows for
super-step N land after step N+1's admission rows, and `case_finalized` can precede
the final hop's execution row. The table above therefore binds as: (a) the admission
rows form an ordered subsequence exactly as listed, (b) each hop's execution row
appears after its own admission row (invariant 1), with per-hop counts matching, and
(c) exactly one `case_finalized` row exists. This is the monolith-vs-BSP ordering
difference anticipated in plan.md § risks — parity holds at per-hop granularity, not
global interleaving.

**Machine-checkable invariants** (enforced by `pkg/auditbridge` tests):

1. Completeness: every executed hop has an `allow` row strictly before its `message` row.
2. Deny-completeness: every halted path ends with exactly one `deny` row and no
   subsequent execution rows for that path.
3. PHI-freedom: for every event, every string field matches
   `NOT (raw case ID | MRN pattern | any symptom/diagnosis free text from the payload)`;
   `CaseID` equals the opaque token; `Content`-derived data never appears.
4. Review anchoring: every `review_decision` row has non-empty ReviewerID or reason
   `stale_response`.
5. Duplicates tolerated, gaps not: the ordered subsequence above must be present even
   if bridge re-emission duplicates `message` rows.
6. Restart continuity: for a kill/resume case, invariants 1–5 hold over the UNION of
   both process lifetimes' events.

# Competitive Landscape — YC Healthcare-AI, mapped to Bodh

> **Status:** Market research snapshot (July 2026). Assembled from Y Combinator public
> company pages + web search, not the exhaustive live directory. Company facts (funding,
> traction, batch) are largely self-reported via YC/LinkedIn launches; treat early-stage
> figures as indicative, not audited. This document informs the ADR-0006 "research
> platform vs. clinical product" identity question — see `docs/adr/`.

## Headline finding

YC is now funding **Bodh's substrate thesis** — immutable audit + drift monitoring +
governance *over* clinical AI — as standalone venture-backed companies. That category was
not visibly fundable 18 months ago. The competitive frontier for Bodh is **not the
diagnostician** (commoditized); it is the emerging **clinical-AI governance-infrastructure**
cohort. Two capabilities central to Bodh's architecture are absent across the entire
cohort: the **Wilson-score graduation ladder** (statistically-gated autonomy promotion) and
the **fail-closed HITL execution gate**.

Correction vs. an earlier pass: the "ER-focused evidence CDS startup" is **Vera Health
itself** (its own tagline), not a separate company.

---

## Everything, ranked by overlap with Bodh

Reorganizing by *overlap-to-Bodh* rather than sub-sector: sub-sector says what a company
sells; overlap-rank says who contests Bodh's defensible ground (the safety substrate:
append-only audit, drift monitoring, HITL gate, governance policy stack, graduation ladder).

### Tier 1 — HIGH overlap: building Bodh's substrate (watch closely)

| Company | Batch | What / Bodh collision | Funding |
|---|---|---|---|
| **Parachute** | S25 | Vet + deploy + monitor clinical AI; **"immutable audit trail"**; drift/bias monitoring. Governs *other people's* models; cites ONC HTI-1 / Colorado AI Act / CA SB 3030 as the wedge | ~$500K |
| **Wedge** (wedge.health) | S25 | *"Palantir for Healthcare AI Agents"* — audit-trail mgmt + risk registry + CDS governance as a "trust layer." Explicitly *governance, not automation* | $500K pre-seed |
| **Lattice Health** | S26/P26 | Read-only monitoring of deployed imaging AI: drift, subgroup performance, agreement-with-radiologist. "Watch, don't act" = Bodh's shadow rung, narrowed to radiology | undisclosed |
| **Glass Health** | W23 | Physician-workflow CDS that drafts DDx + assessment for human edit. Closest peer on **stance** (draft, don't decide) to Bodh's HITL gate | $5M seed |
| **FutureClinic** | F24 | Full intake→reasoning→narrowed-Dx→treatment = the MARA flow. But "99% AI, 1% doctor" — the *philosophical inverse* of Bodh's HITL refusal | undisclosed |

### Tier 2 — MEDIUM overlap / adjacent (validate the market, don't contest the core)

| Company | Batch | Note |
|---|---|---|
| **TrustAI** | S26 | Horizontal agent governance (SOC2 / ISO-42001 / AIUC-1); no clinical grounding, no PHI/HIPAA specialization |
| **Commure / Athelas** | W16 | ~$750M raised, $7B valuation "healthcare OS"; scale + autonomous-agent ambition could drift toward decisioning |
| **Prana Health** | W26 | 24/7 "AI doctor" monitoring records + wearables for "clinical drift"; automates ~90%, human backstop |
| **Sully.ai** | S21 | Role-based "AI employees" (Nurse/Consultant/Coder); multi-agent, no published safety substrate |
| **Beacon Health** | W26 | In-EHR agents touch risk-adjustment coding — autonomous clinical-adjacent actions that *need* audit/HITL |
| **Pelica** | X25 | AI-native OS for value-based care; owns the unified data record + agent runtime Bodh must plug into |
| **Arintra** | YC (Series A) | Autonomous medical coding, $21M Series A; coding errors carry compliance/audit exposure |

### Tier 3 — LOW overlap (non-competitive; market color)

- **Evidence / QA:** Vera Health (S24), MediSearch (S23) — RAG-over-literature, advisory only; touch only Bodh's `pkg/knowledge` layer.
- **RCM / prior-auth / voice** (the *largest* YC healthcare cluster): Aegis (X25), LunaBill (F25), Locata (S25), Prosper (S23), Cair Health (S23), Clarion (W24), Sohar Health (S23), Adentris (X25). Administrative dollars, safely below the clinical-decision line.
- **Ambient scribe:** Decoda (S23), Ember (F24), Hona (W24), Quadrant Health (W21) + non-YC leaders Abridge ($250M+), Ambience ($243M), Suki, Nabla, Freed, DeepScribe.
- **EHR / front-office:** Orchid (W23), Penciled (W24), Novoflow (X25).
- **Biotech (out of scope):** Convexia (S25), Exonic (F25), Ditto Bio (W26), Ångström AI (S24), Anthrogen (S24).

---

## Cluster deep-dive: clinical AI governance (the strategically live cohort)

Four companies — **Parachute, Wedge, Lattice, Adentris** — launched inside a single ~12-month
window (X25→S26), each selling a subset of Bodh's exact substrate, all citing the **same
regulatory forcing function**: ONC HTI-1, Colorado AI Act, California SB 3030. The convergent
winning position is *"the trust layer / OS that governs everyone else's AI."*

**What none of them ship:** their own diagnostician, and an evidence-gated autonomy ladder.
They govern *vendor* models from the outside (audit + drift + eval) and deliberately stop at
"watch." Bodh has two things the cohort lacks:

1. **Wilson-score graduation ladder** — statistically-gated, auditable promotion of an agent
   from `development → shadow → copilot → autopilot`, with the autopilot fence requiring an
   explicit authorized override (ADR-0012).
2. **Fail-closed HITL execution gate + governance stack** — answers "how do we let AI *act*,
   not just watch," the problem Lattice's read-only stance and Parachute's audit-only story
   are structurally avoiding.

### Also in the water (governance-adjacent)

- **Qualified Health** — "one governed operating layer" to deploy AI safely at enterprise scale.
- **BitBoard** (X25) — AI agents for healthcare back-offices.
- **Phases** (S25) — automated clinical-trial oversight; "agents that monitor for risk + maintain audit readiness," but domain is CRO/trial ops, not model governance.

---

## Cluster deep-dive: clinical decision support & diagnostic reasoning

Splits into two archetypes, neither replicating Bodh's full posture:

- **Evidence / QA engines** (Vera Health, MediSearch): RAG over literature with citations;
  advisory by nature, low pipeline overlap. Vera cites "medical knowledge doubles every 73
  days"; MediSearch reports 94.2% on USMLE sample.
- **AI-doctor pipelines** (FutureClinic, Prana, and partly Glass): come closest to Bodh's
  intake→reasoning→plan flow, but lean **automation-forward** — FutureClinic "99% AI, 1%
  doctor," Prana "automate 90%" — treating the clinician as a lightweight/async approver.

**Glass Health (W23)** is Bodh's closest peer on *stance*: physician-workflow CDS that
generates DDx/assessment *drafts* a human must edit. But even Glass sells this as UX
convention, not as an architected refusal boundary with append-only audit, LLM-agnostic
rule-based fallback twins, and a MAI-DxO multi-agent panel.

---

## Cluster deep-dive: ambient scribe & documentation (crowded, commoditizing)

Dozens of YC + non-YC entrants; ~$600M category revenue in 2025 (2.4× YoY); clear
consolidation (Commure absorbing **Augmedix** for $139M in Oct 2024). The frontier of value
has already moved *downstream* from the note into **autonomous coding and revenue-cycle /
denial-prevention** (Ember, Ambience, Commure) — that's where quantifiable ROI lives.
"Compliance" here almost always means **billing/payer compliance**, not audit-trail, drift, or
agent-governance controls. **Largely non-overlap with Bodh.** The one entity to watch is
**Commure** purely for its $7B platform reach and autonomous-agent ambition.

The first RCT of AI scribes (NEJM AI, 2025, UCLA Health, 238 physicians) found occasional
inaccuracies in both tools tested — reinforcing that the category still relies on human note
sign-off (human-in-the-loop for *accuracy*, not a governed decisioning system).

---

## Cluster deep-dive: RCM / prior-auth / denials / billing voice agents (biggest by volume)

The single largest and most fundable YC healthcare-AI cluster, because it is the ideal early
AI wedge: pain quantified in the hundreds of billions ($260B+/yr lost to denials), a buyer
with hard-dollar ROI and no compliance committee blocking a back-office tool, and work that is
high-volume, structured, and **safely below the clinical-decision line**. A wrong appeal
letter costs money, not a patient.

Representative companies: **Aegis** (denial appeals), **LunaBill** ($30M+ recovered, 50K+
calls), **Beacon Health** (in-EHR VBC agents), **Locata** (referrals/prior-auth), **Prosper**
($5M seed, payer voice), **Cair Health** (100+ practices), **Clarion** ($5.4M, patient comms),
**Arintra** ($21M Series A, autonomous coding), **Sohar Health** (eligibility/verification).

**Relevance to Bodh is validating, not competitive:** the handful pushing agents *into* the
EHR to touch coding, risk adjustment, or auto-submitted payer actions (**Beacon, Arintra,
Adentris**, partly Aegis) are the only ones whose autonomous actions carry audit/compliance
exposure where a governance + append-only-audit + HITL layer could be repackaged as
horizontal infrastructure — but they'd want *coding/compliance* audit, not *clinical-decision*
governance. Net: the market rewards administrative medical agents far more readily than
clinical ones, reinforcing the ADR-0006 tension.

---

## Cluster deep-dive: AI-native EHR + biotech

**AI-native EHR (platform-adjacency risk, not head-to-head):** Orchid (mental-health EHR),
Penciled (PT front-office), Novoflow (voice ops), Pelica (VBC operating system), Beacon, Cair.
None do diagnostic orchestration with an auditable, governance-gated decision boundary; they
cluster around administrative/financial automation. The strategic signal: **Pelica** and the
VBC-agent players race to own the unified data record and in-EHR agent runtime — the exact
layer a decision-support system must plug into. For a *reference architecture*, they read as
potential **hosts / distribution channels** more than rivals; but if one becomes the dominant
"AI-native OS," it controls the integration surface and could commoditize a thin decision layer.

**Biotech / drug discovery (out of scope):** Convexia ("AI-maximalist pharma"), Exonic
(biological foundation models), Ditto Bio (autoimmune therapies from parasite biology),
Ångström AI (physics-accurate molecular simulation), Anthrogen (protein foundation models).
Only surface similarity is "multi-agent" vocabulary; none touch care delivery, EHR
integration, PHI handling, or point-of-care decision support.

---

## Cross-cutting strategic reads for Bodh

1. **Substrate validated as fundable, but packaging is orthogonal to the money.**
   Parachute/Wedge sell a closed SaaS trust-layer to hospital AI-governance buyers; Bodh is an
   open, noncommercial reference architecture *with* its own agents. Same substrate, opposite
   go-to-market. Sharpens ADR-0006 rather than resolving it.

2. **Automate-vs-HITL is the real battleground, and it's splitting.** FutureClinic/Prana/Sully
   push automation; Glass and Bodh insist on the human refusal boundary. As regulatory forcing
   functions bite, "deliberately refuses to auto-execute + proves it in an audit trail" becomes
   a *feature*, not a limitation.

3. **The two things nobody else has built are the moat:** the graduation ladder and the
   fail-closed HITL gate. If Bodh moves from research artifact toward a product or an adopted
   reference, those — not the diagnostician — are the wedge.

4. **The RCM cluster confirms the market truth:** capital rewards administrative medical
   agents over clinical ones, because failure costs money, not a patient — the exact economic
   gravity ADR-0006 wrestles with.

---

## Method & limitations

- Compiled via web search over YC public company pages + individual `/companies/<slug>`
  detail pages (server-rendered) and funding/traction searches. The YC directory listing
  itself is client-rendered and not directly scrapable without a browser.
- This is a **curated deep-slice**, not the full ~1,000+ healthcare directory. A complete
  enumeration requires rendering the live directory.
- Early-stage funding/traction numbers are self-reported; verify before citing externally.

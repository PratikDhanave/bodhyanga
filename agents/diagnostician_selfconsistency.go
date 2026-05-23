package agents

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// SelfConsistencyDiagnostician runs the same diagnostic prompt N times in
// parallel with a higher sampling temperature, then takes a majority vote
// across the resulting proposals. The voting aggregate replaces any single
// LLM call's variance with a more stable consensus answer — Wang et al.,
// "Self-Consistency Improves Chain of Thought Reasoning" (2023).
//
// It wraps an LLMDiagnostician (Base) and reuses its prompt construction,
// RAG retrieval, JSON schema, fallback, and citation-reconciliation logic.
// The agent's ID remains "diagnostician" so supervisor routing is unchanged.
type SelfConsistencyDiagnostician struct {
	// Base is the reused LLMDiagnostician. Its Client, Knowledge, Fallback,
	// Timeout, MinConfidence, and buildRequest/completeAndDecode are all
	// shared. Required.
	Base *LLMDiagnostician

	// Samples is the number of parallel LLM calls to make. Clamped to
	// [1, 10]; 0 or negative defaults to DefaultSelfConsistencySamples (5).
	Samples int

	// Temperature controls the sampling temperature handed to each call.
	// Higher values produce more diverse votes, which is the point of
	// self-consistency. Defaults to 0.6 — markedly higher than the base
	// diagnostician's 0.2 so the samples can actually disagree.
	Temperature float64

	// Timeout caps the total wall-clock budget across all samples. Defaults
	// to 90 seconds.
	Timeout time.Duration

	// MaxConcurrency caps how many samples run concurrently. Defaults to 4
	// so we never hammer the upstream provider with more than that many
	// in-flight requests per case.
	MaxConcurrency int
}

// DefaultSelfConsistencySamples is the per-case sample count used when
// Samples is unset or non-positive. Five samples is the smallest count
// where simple majority voting is meaningful while keeping the bill bounded.
const DefaultSelfConsistencySamples = 5

// maxSelfConsistencySamples caps the configured sample count so a typo'd
// flag cannot fan out hundreds of LLM calls.
const maxSelfConsistencySamples = 10

// defaultSelfConsistencyTemperature is the sampling temperature for diversity.
const defaultSelfConsistencyTemperature = 0.6

// defaultSelfConsistencyMaxConcurrency caps the goroutine fan-out so a single
// case never opens more than this many concurrent LLM requests.
const defaultSelfConsistencyMaxConcurrency = 4

// defaultSelfConsistencyTimeout is the total wall-clock budget across all
// samples.
const defaultSelfConsistencyTimeout = 90 * time.Second

func (a *SelfConsistencyDiagnostician) ID() string   { return "diagnostician" }
func (a *SelfConsistencyDiagnostician) Name() string { return "Diagnostician (Self-Consistency)" }
func (a *SelfConsistencyDiagnostician) Capabilities() []string {
	return []string{"provide_diagnosis", "self_consistency", "llm_backed"}
}

// selfConsistencySample is one filled goroutine slot. Declared at package
// scope so helpers can take it as a parameter.
type selfConsistencySample struct {
	proposal medical.DiagnosisProposal
	err      error
}

func (a *SelfConsistencyDiagnostician) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.Base == nil {
		return nil, errors.New("self-consistency diagnostician: nil Base")
	}
	if a.Base.Client == nil {
		env.Logf("[diagnostician/self-consistency] llm client not configured — falling back to rule-based")
		return a.Base.runFallback(ctx, msg, env)
	}

	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}

	samples := a.Samples
	if samples <= 0 {
		samples = DefaultSelfConsistencySamples
	}
	if samples > maxSelfConsistencySamples {
		samples = maxSelfConsistencySamples
	}

	temperature := a.Temperature
	if temperature <= 0 {
		temperature = defaultSelfConsistencyTemperature
	}

	timeout := a.Timeout
	if timeout <= 0 {
		timeout = defaultSelfConsistencyTimeout
	}

	maxConc := a.MaxConcurrency
	if maxConc <= 0 {
		maxConc = defaultSelfConsistencyMaxConcurrency
	}
	if maxConc > samples {
		maxConc = samples
	}

	minConfidence := a.Base.MinConfidence
	if minConfidence == 0 {
		minConfidence = 0.3
	}

	// Total budget across every sample. Each goroutine uses the shared
	// context so they all cancel together once the wall-clock budget burns
	// out.
	totalCtx, totalCancel := context.WithTimeout(ctx, timeout)
	defer totalCancel()

	// Build the shared base request once so every sample starts from the
	// same prompt, schema, and retrieved citations. Mutating Temperature is
	// the entire point of self-consistency — without it the samples are
	// near-identical at the base's 0.2 setting.
	baseReq, retrieved, _ := a.Base.buildRequest(totalCtx, state, env)
	baseReq.Temperature = temperature

	results := make([]selfConsistencySample, samples)
	sem := make(chan struct{}, maxConc)
	var wg sync.WaitGroup

	for i := 0; i < samples; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-totalCtx.Done():
				results[idx] = selfConsistencySample{err: totalCtx.Err()}
				return
			}
			defer func() { <-sem }()

			req := cloneRequestForSample(baseReq)
			p, err := a.Base.completeAndDecode(totalCtx, req, retrieved, minConfidence, env)
			results[idx] = selfConsistencySample{proposal: p, err: err}
		}(i)
	}
	wg.Wait()

	successes := make([]medical.DiagnosisProposal, 0, samples)
	for i, r := range results {
		if r.err != nil {
			env.Logf("[diagnostician/self-consistency] sample %d failed: %v", i+1, r.err)
			continue
		}
		successes = append(successes, r.proposal)
	}

	// "Fewer than half" — fall back to rule-based. Use strict inequality
	// against samples/2 so the threshold matches a "majority succeeded"
	// reading: for samples=5 we need >=3, for samples=4 we need >=2.
	if len(successes)*2 < samples {
		env.Logf("[diagnostician/self-consistency] only %d/%d samples succeeded — falling back to rule-based", len(successes), samples)
		return a.Base.runFallback(ctx, msg, env)
	}

	winner, votes, meanConf := voteSelfConsistency(successes)
	if winner.Diagnosis == "" {
		// Defensive: every voter produced an empty diagnosis. Should be
		// impossible because completeAndDecode rejects empty Diagnosis, but
		// fall back rather than emit junk.
		env.Logf("[diagnostician/self-consistency] no usable diagnosis across %d samples — falling back to rule-based", len(successes))
		return a.Base.runFallback(ctx, msg, env)
	}

	final := aggregateSelfConsistencyProposal(winner, successes, retrieved, meanConf)

	env.Logf("[diagnostician/self-consistency] samples=%d winner=%q votes=%d/%d mean_conf=%.2f",
		samples, final.Diagnosis, votes, len(successes), final.Confidence)
	env.Logf("[diagnostician/self-consistency] provider=%s model=%s rationale: %s",
		a.Base.Client.Provider(), a.Base.Client.Model(), final.Rationale)

	state.WorkingDiagnosis = final.Diagnosis
	content, _ := json.Marshal(final)
	stateContent := medical.EncodeState(state)
	verify := agent.NewMessage(
		a.ID(),
		"reasoning_verifier",
		agent.RoleAgent,
		"diagnosis_proposal",
		string(content),
		mergeStateMeta(msg.Metadata, stateContent),
	)
	return []agent.Message{verify}, nil
}

// cloneRequestForSample deep-copies the shared base request so per-goroutine
// mutations (e.g. JSONSchema or Messages tweaks down the line) cannot leak
// between samples. The Messages slice is copied by element; the underlying
// Images slice is not deep-copied because completeAndDecode does not mutate
// it.
func cloneRequestForSample(base llm.Request) llm.Request {
	r := base
	r.Messages = append([]llm.Message(nil), base.Messages...)
	return r
}

// voteSelfConsistency tallies the proposals by normalised diagnosis label,
// then returns the highest-confidence original proposal whose normalised
// label is the winner, along with the winning vote count and the mean
// confidence of those winning votes.
//
// Tie-break: when multiple labels share the top vote count, the label with
// the higher mean confidence across its votes wins. If mean confidences are
// also identical, the lexicographically smaller normalised label wins so the
// result is deterministic across goroutine scheduling.
func voteSelfConsistency(proposals []medical.DiagnosisProposal) (medical.DiagnosisProposal, int, float64) {
	type bucket struct {
		key       string // normalised label, kept for deterministic tie-break
		count     int
		sumConf   float64
		proposals []medical.DiagnosisProposal
	}
	buckets := make(map[string]*bucket)
	for _, p := range proposals {
		k := normaliseDiagnosis(p.Diagnosis)
		b, ok := buckets[k]
		if !ok {
			b = &bucket{key: k}
			buckets[k] = b
		}
		b.count++
		b.sumConf += p.Confidence
		b.proposals = append(b.proposals, p)
	}
	if len(buckets) == 0 {
		return medical.DiagnosisProposal{}, 0, 0
	}

	// Flatten into a slice so we can sort deterministically.
	ordered := make([]*bucket, 0, len(buckets))
	for _, b := range buckets {
		ordered = append(ordered, b)
	}
	sort.Slice(ordered, func(i, j int) bool {
		bi, bj := ordered[i], ordered[j]
		if bi.count != bj.count {
			return bi.count > bj.count
		}
		mi := bi.sumConf / float64(bi.count)
		mj := bj.sumConf / float64(bj.count)
		if mi != mj {
			return mi > mj
		}
		return bi.key < bj.key
	})

	winner := ordered[0]
	// Pick the highest-confidence original proposal inside the winning
	// bucket — this preserves the un-normalised diagnosis string, the
	// strongest rationale, and the citations that the most confident vote
	// produced.
	best := winner.proposals[0]
	for _, p := range winner.proposals[1:] {
		if p.Confidence > best.Confidence {
			best = p
		}
	}
	mean := winner.sumConf / float64(winner.count)
	return best, winner.count, mean
}

// aggregateSelfConsistencyProposal builds the final DiagnosisProposal from
// the vote winner. Diagnosis, Rationale, and Citations come from the
// winning vote (preserving provenance and the most confident phrasing).
// Differential is the union of every sample's differential, deduped and
// ordered by frequency so the most-cited alternatives come first.
// Confidence is the supplied mean of the winning bucket.
func aggregateSelfConsistencyProposal(winner medical.DiagnosisProposal, all []medical.DiagnosisProposal, retrieved []medical.DiagnosisCitation, meanConf float64) medical.DiagnosisProposal {
	final := winner
	final.Confidence = meanConf
	final.Differential = unionDifferentials(all)
	// Preserve citations: prefer the winner's, otherwise the retrieved set
	// (mirrors base completeAndDecode behaviour).
	if len(final.Citations) == 0 && len(retrieved) > 0 {
		final.Citations = retrieved
	}
	return final
}

// unionDifferentials returns the de-duplicated union of every proposal's
// Differential entries. Entries that appear in multiple samples come first
// (frequency-ordered); ties break by first-seen order so the result is
// stable.
func unionDifferentials(all []medical.DiagnosisProposal) []string {
	type entry struct {
		original string
		count    int
		firstIdx int
	}
	idx := 0
	seen := make(map[string]*entry)
	for _, p := range all {
		for _, d := range p.Differential {
			key := normaliseDiagnosis(d)
			if key == "" {
				continue
			}
			if e, ok := seen[key]; ok {
				e.count++
				continue
			}
			seen[key] = &entry{original: d, count: 1, firstIdx: idx}
			idx++
		}
	}
	if len(seen) == 0 {
		return nil
	}
	flat := make([]*entry, 0, len(seen))
	for _, e := range seen {
		flat = append(flat, e)
	}
	sort.Slice(flat, func(i, j int) bool {
		if flat[i].count != flat[j].count {
			return flat[i].count > flat[j].count
		}
		return flat[i].firstIdx < flat[j].firstIdx
	})
	out := make([]string, 0, len(flat))
	for _, e := range flat {
		out = append(out, e.original)
	}
	return out
}

// normaliseDiagnosis lowercases, trims, and collapses internal whitespace so
// "Community-acquired pneumonia", "community-acquired  pneumonia", and
// "  COMMUNITY-ACQUIRED PNEUMONIA " all hash to the same bucket.
func normaliseDiagnosis(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

package engine

import (
	"sync"
	"time"

	"github.com/jinto/kittypaw/core"
)

// PipelineState carries multi-turn state that the deterministic-branch
// pipeline needs to make routing decisions without consulting the LLM.
//
// Today this is just the most recent Skill.search results — used by the
// classifier to disambiguate a bare "네" between "yes, install" and
// other affirmatives, and by InstallConsentBranch to recall the exact
// skill id without LLM hallucination. Future state (PendingClarification,
// recent tool calls, etc.) lands here.
//
// One PipelineState per Session — same isolation boundary as the rest
// of the tenant. All access is mutex-guarded; the Session.Run loop is
// single-goroutine per event but server-side reload could race on init.
type PipelineState struct {
	mu                     sync.Mutex
	lastSkillSearchResults []core.RegistryEntry
	lastSearchAt           time.Time

	// lastSkillOutput is the raw user-facing output from the most recent
	// deterministic skill execution (InstallConsentBranch +
	// RunInstalledSkillBranch). Used by runAgentLoop to augment the
	// system prompt when a short follow-up arrives — the LLM's "ignore
	// history" prior is observably stronger than its "use history"
	// prior, so we re-surface the data inside the system message
	// instead of relying on the conversation transcript alone.
	lastSkillOutput   string
	lastSkillOutputAt time.Time
}

// skillSearchResultsTTL is how long an unused search result hangs
// around before it stops counting as "the most recent suggestion".
// 5 minutes covers a normal think-then-reply pause; longer windows
// risk pairing a stale offer with an unrelated later "네".
const skillSearchResultsTTL = 5 * time.Minute

// skillOutputTTL is how long a skill's raw output stays available for
// cross-turn augmentation. Same 5 min budget as skillSearchResultsTTL
// — a longer window risks pairing a stale rate table with an unrelated
// later "계산해줘".
const skillOutputTTL = 5 * time.Minute

// NewPipelineState returns an empty pipeline state.
func NewPipelineState() *PipelineState {
	return &PipelineState{}
}

// RecordSkillSearch caches the entries returned by the most recent
// Skill.search call. Called from executeSkillSearch — every search,
// not just the ones that lead to an install offer (the classifier
// uses the *presence* of recent results plus a consent-shaped reply
// to gate routing; see classifyIntent).
func (ps *PipelineState) RecordSkillSearch(entries []core.RegistryEntry) {
	if ps == nil {
		return
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.lastSkillSearchResults = entries
	ps.lastSearchAt = time.Now()
}

// RecentSkillSearch returns the cached entries if they were recorded
// within skillSearchResultsTTL, or nil otherwise. The returned slice
// is the live cache — callers should treat it as read-only.
func (ps *PipelineState) RecentSkillSearch() []core.RegistryEntry {
	if ps == nil {
		return nil
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if time.Since(ps.lastSearchAt) > skillSearchResultsTTL {
		return nil
	}
	return ps.lastSkillSearchResults
}

// ClearSkillSearch drops the cached search results. Called after a
// successful install so an unrelated later "네" doesn't re-trigger
// install consent against the stale offer.
func (ps *PipelineState) ClearSkillSearch() {
	if ps == nil {
		return
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.lastSkillSearchResults = nil
}

// RecordSkillOutput stores the raw user-facing output from a
// deterministic skill execution. Called by InstallConsentBranch and
// RunInstalledSkillBranch right before they return, so the next
// short follow-up turn can augment its system prompt with the data
// the user is most likely referencing.
func (ps *PipelineState) RecordSkillOutput(output string) {
	if ps == nil || output == "" {
		return
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.lastSkillOutput = output
	ps.lastSkillOutputAt = time.Now()
}

// RecentSkillOutput returns the cached skill output if recorded
// within skillOutputTTL, or "" otherwise. Cheap to call on every
// turn — caller decides whether to augment.
func (ps *PipelineState) RecentSkillOutput() string {
	if ps == nil {
		return ""
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if time.Since(ps.lastSkillOutputAt) > skillOutputTTL {
		return ""
	}
	return ps.lastSkillOutput
}

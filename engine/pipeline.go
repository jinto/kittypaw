package engine

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jinto/kittypaw/core"
)

// IntentKind classifies a user message into a deterministic branch or
// the legacy LLM agent loop. Each non-fallback kind is owned by a single
// Branch implementation. Adding a new behavioral case becomes "add a
// constant + classifier rule + Branch", not "grow the system prompt".
type IntentKind string

const (
	IntentChitchat            IntentKind = "chitchat"
	IntentBrowse              IntentKind = "browse"
	IntentInstallConsentReply IntentKind = "install_consent_reply"
	IntentLegacyFallback      IntentKind = "legacy_fallback"
)

// Intent is the classifier's output. Params carry branch-specific state
// (e.g. the chitchat trigger phrase, the suggested skill id from the
// previous turn). Confidence is reserved for future LLM-fallback
// classifiers — rule-first matches are 1.0.
type Intent struct {
	Kind       IntentKind
	Params     map[string]any
	Confidence float64
}

// Branch handles one intent kind end-to-end. Implementations must be
// safe to call from any tenant Session without sharing state across
// tenants — branch-local state lives on PipelineState (per-Session).
type Branch interface {
	Execute(ctx context.Context, sess *Session, event core.Event, intent Intent) (string, error)
}

// classifyIntent runs the rule-first classifier. Phase 1-3 cover
// chitchat / browse / install-consent-reply. Everything else falls
// back to the legacy LLM agent loop. Phase 4 will add LLM-fallback
// classification for ambiguous queries (clarify trigger).
//
// install-consent-reply is the only state-aware rule today: it fires
// only when (a) a recent Skill.search exists in PipelineState (i.e.
// the legacy LLM path just made an offer) AND (b) the reply looks like
// consent. This keeps a bare "네" off the consent branch when there's
// no offer to consent to.
func classifyIntent(text string, state *PipelineState) Intent {
	t := strings.TrimSpace(text)
	if t == "" {
		return Intent{Kind: IntentLegacyFallback}
	}
	if isChitchat(t) {
		return Intent{Kind: IntentChitchat, Confidence: 1.0}
	}
	if isBrowse(t) {
		return Intent{Kind: IntentBrowse, Confidence: 1.0}
	}
	if state != nil && len(state.RecentSkillSearch()) > 0 && isInstallConsent(t) {
		return Intent{Kind: IntentInstallConsentReply, Confidence: 1.0}
	}
	return Intent{Kind: IntentLegacyFallback}
}

// isInstallConsent matches the user's reply to a suffix offer. Two
// shapes:
//
//  1. Any phrase containing "설치" (설치해줘요/설치해주세요/설치할게요/...)
//     — strong signal regardless of length.
//  2. A short bare affirmative (네/응/그래/yes/ok/...) — only fires on
//     short replies because longer messages with the same prefix
//     ("네, 그런데 다른 거 알려줘") aren't consent.
func isInstallConsent(text string) bool {
	if strings.Contains(text, "설치") {
		return true
	}
	if runeCount(text) > 8 {
		return false
	}
	lowered := strings.ToLower(text)
	// Strip a trailing punctuation set used in casual Korean replies.
	trimmed := strings.TrimRight(lowered, ".,!?~ ")
	bareAffirmatives := []string{
		"네", "넵", "응", "어", "그래", "그래요",
		"ㅇ", "ㅇㅇ", "ㅇㅋ", "오케이", "예",
		"yes", "y", "ok", "okay", "sure", "yep", "yeah",
	}
	for _, a := range bareAffirmatives {
		if trimmed == a {
			return true
		}
	}
	return false
}

// isBrowse detects "show me what's available" queries — the user wants
// a registry overview, not a specific install. Phrasing is varied
// enough that a substring list beats a regex; the length cap blocks
// long prose that *contains* "스킬" but isn't actually browsing.
func isBrowse(text string) bool {
	const browseMaxRunes = 30
	if runeCount(text) > browseMaxRunes {
		return false
	}
	lowered := strings.ToLower(text)
	patterns := []string{
		"어떤 스킬", "어떤 기능", "무슨 스킬",
		"스킬 목록", "스킬 뭐", "스킬은 뭐", "스킬들",
		"스킬 추천", "추천 스킬", "어떤 거", "뭐 있",
		"what skills", "available skills", "list skills",
		"browse", "list of", "추천해",
	}
	for _, p := range patterns {
		if strings.Contains(lowered, p) {
			return true
		}
	}
	return false
}

// isChitchat detects short reactive utterances that don't carry a new
// request — "오 잘하네!", "고마워", "thanks", etc. The length cap keeps
// real questions ("이게 잘하는 건가요?") out of the chitchat branch.
func isChitchat(text string) bool {
	const chitchatMaxRunes = 25
	if runeCount(text) > chitchatMaxRunes {
		return false
	}
	lowered := strings.ToLower(text)
	patterns := []string{
		"잘하네", "잘하는", "잘해", "잘하시",
		"고마워", "고맙", "감사",
		"좋네", "좋아", "굿", "최고",
		"thanks", "thank you", "thx",
		"nice", "good job", "great",
		"멋져", "멋지", "굳",
	}
	for _, p := range patterns {
		if strings.Contains(lowered, p) {
			return true
		}
	}
	return false
}

func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// dispatchPipeline runs the rule-first classifier and, if the intent
// has a deterministic branch, executes it and returns (response, true).
// On (legacy_fallback OR branch error), it returns (_, false) so the
// caller falls through to the legacy LLM agent loop.
//
// Returning a bool instead of a sentinel error keeps the legacy path
// untouched — callers can wire this in with a single if-statement.
func dispatchPipeline(ctx context.Context, sess *Session, event core.Event, eventText string) (string, bool) {
	intent := classifyIntent(eventText, sess.Pipeline)
	branch := getBranch(intent.Kind)
	if branch == nil {
		return "", false
	}
	resp, err := branch.Execute(ctx, sess, event, intent)
	if err != nil {
		return "", false
	}
	return resp, true
}

// getBranch returns the Branch implementation for an intent kind, or
// nil for legacy_fallback / unmapped kinds.
func getBranch(kind IntentKind) Branch {
	switch kind {
	case IntentChitchat:
		return &ChitchatBranch{}
	case IntentBrowse:
		return &BrowseBranch{}
	case IntentInstallConsentReply:
		return &InstallConsentBranch{}
	}
	return nil
}

// ChitchatBranch returns a deterministic ack — no LLM call, no tool
// call. Reproduces the user-vision pattern (4) "친화 비서 persona": a
// short reactive utterance gets a short reactive reply, never re-runs
// the prior tool or re-emits the prior result.
type ChitchatBranch struct{}

func (b *ChitchatBranch) Execute(ctx context.Context, sess *Session, event core.Event, intent Intent) (string, error) {
	return "도움이 됐다니 좋아요! 또 필요하면 말씀해 주세요.", nil
}

// BrowseBranch lists registry skills grouped by domain. No LLM call —
// the previous LLM-driven implementation produced the same shape via
// emergent grouping; this branch reproduces it deterministically.
//
// Reproduces user-vision pattern (2) "도구 부족 가시화": when the user
// asks "어떤 스킬?", they get the full registry surface, not a
// guess-and-suggest from one search keyword.
type BrowseBranch struct{}

func (b *BrowseBranch) Execute(ctx context.Context, sess *Session, event core.Event, intent Intent) (string, error) {
	rc, err := newRegistryClient(sess.Config)
	if err != nil {
		return "지금 스킬 레지스트리에 연결하지 못했어요. 잠시 후 다시 시도해 주세요.", nil
	}
	entries, err := rc.SearchEntries("")
	if err != nil {
		return "지금 스킬 목록을 가져오지 못했어요. 잠시 후 다시 시도해 주세요.", nil
	}
	if len(entries) == 0 {
		return "현재 등록된 스킬이 없어요.", nil
	}
	return formatBrowseResponse(entries), nil
}

// formatBrowseResponse groups entries into a small number of named
// categories using keyword inference on name + description. Hard-coded
// category mapping is a known antipattern (Phase 6 will revisit) but
// keeps Phase 2 within the "no LLM, no new state" boundary. New skills
// land under "기타" until the mapping is updated.
func formatBrowseResponse(entries []core.RegistryEntry) string {
	type bucket struct {
		name  string
		items []core.RegistryEntry
	}
	buckets := []*bucket{
		{name: "금융"}, {name: "날씨"}, {name: "뉴스"},
		{name: "환경"}, {name: "할일"}, {name: "기타"},
	}
	idx := map[string]*bucket{}
	for _, b := range buckets {
		idx[b.name] = b
	}
	for _, e := range entries {
		idx[categorize(e.Name, e.Description)].items = append(idx[categorize(e.Name, e.Description)].items, e)
	}
	var sb strings.Builder
	sb.WriteString("## 사용 가능한 스킬들 (")
	sb.WriteString(strconv.Itoa(len(entries)))
	sb.WriteString("개)\n")
	for _, b := range buckets {
		if len(b.items) == 0 {
			continue
		}
		sb.WriteString("\n### ")
		sb.WriteString(b.name)
		sb.WriteString("\n")
		for _, e := range b.items {
			sb.WriteString("• **")
			sb.WriteString(e.Name)
			sb.WriteString("** — ")
			sb.WriteString(e.Description)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n관심 있는 스킬이 있으면 말씀해 주세요. 설치를 도와드릴게요.")
	return sb.String()
}

func categorize(name, desc string) string {
	t := strings.ToLower(name + " " + desc)
	switch {
	case containsAny(t, "환율", "주가", "주식", "exchange", "stock"):
		return "금융"
	case containsAny(t, "날씨", "weather"):
		return "날씨"
	case containsAny(t, "뉴스", "rss", "news"):
		return "뉴스"
	case containsAny(t, "미세먼지", "air", "환경"):
		return "환경"
	case containsAny(t, "리마인더", "remind", "todo", "할일"):
		return "할일"
	}
	return "기타"
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// InstallConsentBranch handles the user's "네" / "설치해줘요" / etc.
// reply to a previous turn's install offer. The skill id comes from
// PipelineState.RecentSkillSearch — recorded by the legacy path's
// `Skill.search` call. No LLM hallucination of the id (truncation
// regression in commit a4dc8a4 / 26d25c2).
//
// Reproduces user-vision pattern (2) "도구 부족 가시화" plus the
// friendly persona — the user agrees, and the system installs and
// runs without asking again.
type InstallConsentBranch struct{}

func (b *InstallConsentBranch) Execute(ctx context.Context, sess *Session, event core.Event, intent Intent) (string, error) {
	results := sess.Pipeline.RecentSkillSearch()
	if len(results) == 0 {
		// Should not happen — classifier gates on this — but be defensive.
		return "", errBranchFallback
	}
	target := results[0]

	// Guard: PackageManager must be wired (not in some bare test fixtures).
	if sess.PackageManager == nil {
		return "지금 스킬을 설치하기 위한 환경이 준비되지 않았어요. 잠시 후 다시 시도해 주세요.", nil
	}

	rc, err := newRegistryClient(sess.Config)
	if err != nil {
		return "지금 스킬 레지스트리에 연결하지 못했어요. 잠시 후 다시 시도해 주세요.", nil
	}
	entry, err := rc.FindEntry(target.ID)
	if err != nil || entry == nil {
		return "스킬 레지스트리에서 '" + target.Name + "' 항목을 다시 찾지 못했어요. 잠시 후 다시 시도해 주세요.", nil
	}

	pkg, err := sess.PackageManager.InstallFromRegistry(rc, *entry)
	if err != nil {
		return "'" + target.Name + "' 설치 중 문제가 발생했어요: " + err.Error(), nil
	}

	// Clear so a later unrelated "네" doesn't re-install the same skill.
	sess.Pipeline.ClearSkillSearch()

	// Run immediately — match the user vision of "agree → see result".
	output, _ := runSkillOrPackage(ctx, pkg.Meta.ID, sess)
	runOutput := extractOutputField(output)
	if runOutput == "" {
		runOutput = "방금 설치된 스킬을 한 번 실행해 보세요. 결과가 비어 있어요."
	}
	return "✅ '" + pkg.Meta.Name + "' 스킬을 설치했어요.\n\n" + runOutput, nil
}

// errBranchFallback signals "this branch declined; let the legacy path
// handle it". dispatchPipeline already turns any branch error into a
// fallback, but this sentinel makes the intent explicit at the call site.
var errBranchFallback = errBranchFallbackType{}

type errBranchFallbackType struct{}

func (errBranchFallbackType) Error() string { return "branch fallback to legacy" }

// extractOutputField pulls the user-facing string out of runSkillOrPackage's
// JSON envelope. The shape is {"success":true,"output":"..."} on the happy
// path and {"error":"...","output":"..."} on the not-found path (the latter
// also carries an actionable user-facing message). Falls back to the raw
// JSON if the envelope can't be decoded — better than silently dropping.
func extractOutputField(jsonStr string) string {
	type runResult struct {
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	var r runResult
	if err := json.Unmarshal([]byte(jsonStr), &r); err != nil {
		return jsonStr
	}
	if r.Output != "" {
		return r.Output
	}
	if r.Error != "" {
		return r.Error
	}
	return ""
}


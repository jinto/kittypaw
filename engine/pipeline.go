package engine

import (
	"context"
	"encoding/json"
	"regexp"
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
	IntentRunInstalledSkill   IntentKind = "run_installed_skill"
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
func classifyIntent(text string, state *PipelineState, sess *Session) Intent {
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
	// Installed-skill dispatch: when the query keyword appears in an
	// already-installed package's name, run that skill directly. This
	// closes the regression where the LLM, despite the
	// "PRIORITY: installed → Skill.run" prompt rule, still went out
	// to Web.search + suggested re-installing an already-present
	// skill (turn 5 of the 2026-04-27 transcript).
	if pkg := matchInstalledSkill(t, sess); pkg != nil {
		return Intent{
			Kind: IntentRunInstalledSkill,
			Params: map[string]any{
				"skill_id":   pkg.Meta.ID,
				"skill_name": pkg.Meta.Name,
			},
			Confidence: 1.0,
		}
	}
	return Intent{Kind: IntentLegacyFallback}
}

// matchInstalledSkill returns an installed package whose name keywords
// appear in the user query. Single-word match (e.g. "환율 조회" -> "환율")
// is the common case; multi-skill ambiguity ("주식" matches both
// "주식 알림" and "주가 조회") falls through to legacy LLM since picking
// without context is the wrong call.
func matchInstalledSkill(text string, sess *Session) *core.SkillPackage {
	// 1-char query is too noisy for substring matching against installed
	// package metadata — let the legacy LLM clarify it instead.
	if runeCount(text) < 2 {
		return nil
	}
	if sess == nil || sess.PackageManager == nil {
		return nil
	}
	packages, err := sess.PackageManager.ListInstalled()
	if err != nil || len(packages) == 0 {
		return nil
	}
	lowered := strings.ToLower(text)
	var matches []core.SkillPackage
	for _, pkg := range packages {
		if pkgKeywordMatches(lowered, pkg) {
			matches = append(matches, pkg)
		}
	}
	if len(matches) != 1 {
		// 0 → no match, ≥2 → ambiguous (let legacy LLM resolve).
		return nil
	}
	return &matches[0]
}

// pkgKeywordMatches checks whether a query keyword appears in the
// package's name or description. Description is the Korean source
// since installed package names are often ASCII slugs (e.g.
// "exchange-rate") while descriptions carry "환율" / "주가" / "날씨"
// as natural keywords.
//
// A small stop-word list filters generic description tokens that
// would otherwise match every domain query (e.g. "조회", "확인").
func pkgKeywordMatches(loweredQuery string, pkg core.SkillPackage) bool {
	candidates := strings.ToLower(pkg.Meta.Name) + " " + strings.ToLower(pkg.Meta.Description)
	for _, raw := range strings.Fields(candidates) {
		word := strings.Trim(raw, ".,()[]{}:;!?-_/\"'")
		if runeCount(word) < 2 {
			continue
		}
		if pkgKeywordStopWord(word) {
			continue
		}
		// Bidirectional match — Korean attaches particles ("환율" + "을"
		// → "환율을") so the description token is often longer than the
		// query keyword. Both directions catch this without
		// overgenerating: query "환율" matches description token
		// "환율을", query "오늘 환율 어때" matches token "환율".
		if strings.Contains(loweredQuery, word) || strings.Contains(word, loweredQuery) {
			return true
		}
	}
	return false
}

// pkgKeywordStopWord skips description tokens that are too generic to
// signal an installed-skill match. The list intentionally stays short;
// extending it is cheap but each addition narrows the dispatch.
func pkgKeywordStopWord(w string) bool {
	switch w {
	case "조회", "확인", "알림", "정보", "데이터", "api", "free", "skill",
		"실시간", "현재", "오늘", "기준", "별개의", "별개",
		"및", "을", "를", "의", "에", "이", "가", "로", "과",
		"주요", "전용", "제공", "사용", "키", "불필요",
		"the", "and", "for", "with", "from", "into",
		"into.", "텔레그램으로", "발송합니다", "알려줍니다",
		"보내줍니다", "확인하고", "조회합니다", "관리합니다":
		return true
	}
	return false
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
	intent := classifyIntent(eventText, sess.Pipeline, sess)
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
	case IntentRunInstalledSkill:
		return &RunInstalledSkillBranch{}
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

// RunInstalledSkillBranch dispatches an installed skill directly when
// the user's query keyword matches the skill name. Replaces the legacy
// LLM path's "PRIORITY: installed → Skill.run" rule, which the model
// occasionally ignored — most visibly in the 2026-04-27 transcript
// where "환율" right after installing "환율 조회" still triggered
// Web.search + a duplicate install offer. The deterministic branch
// removes that drift.
type RunInstalledSkillBranch struct{}

func (b *RunInstalledSkillBranch) Execute(ctx context.Context, sess *Session, event core.Event, intent Intent) (string, error) {
	skillID, _ := intent.Params["skill_id"].(string)
	if skillID == "" {
		return "", errBranchFallback
	}
	rawJSON, _ := runSkillOrPackage(ctx, skillID, sess)
	output := extractOutputField(rawJSON)
	if output == "" {
		// Empty output is the legacy "(no output)" path — fall back rather
		// than echo a confusing "응답이 비어 있어요" template here, since
		// the legacy LLM may have a meaningful reformulation.
		return "", errBranchFallback
	}
	userText := ""
	if p, err := event.ParsePayload(); err == nil {
		userText = p.Text
	}
	return mediateSkillOutput(ctx, sess, skillID, userText, output), nil
}

// mediateSkillRawOutputCap caps how much skill output is fed to the
// reframing LLM. Mirrors moaCandidateCharLimit so a verbose skill
// (e.g. large JSON fetch) cannot blow up input tokens. Above the cap we
// truncate with a marker; truncation is rare (exchange-rate / weather /
// news outputs all sit under 2 kB) and safer than uncapped spend.
const mediateSkillRawOutputCap = 8000

// mediateSkillOutput reframes a skill's raw output through a small LLM
// call so the user query's modifier (단위/언어/scope/verbosity) lands in
// the response. The contract is reformat-only: raw numbers stay
// verbatim, no new web search, no fabrication. On any failure (nil
// provider, LLM error, empty response) returns rawOutput unchanged so
// the user never loses the underlying data.
//
// Cost trade-off: Phase 4 RunInstalledSkillBranch was 0 LLM calls per
// dispatch (verbatim output). This adds one small call per dispatch to
// align the response with query intent. The verbatim path was correct
// on shape but lost user-vision quality whenever the query carried a
// modifier the skill JS didn't parse — fixing that in skill JS would
// be case-by-case (env feedback_no_hardcoding.md). LLM mediation
// generalizes the fix to every installed skill at a measured 1-call
// cost. Cache deferred (query-dependent key gives low hit rate).
func mediateSkillOutput(ctx context.Context, sess *Session, skillID, userText, rawOutput string) string {
	if sess == nil || sess.Provider == nil || rawOutput == "" || userText == "" {
		return rawOutput
	}
	truncated := rawOutput
	if len(truncated) > mediateSkillRawOutputCap {
		truncated = truncated[:mediateSkillRawOutputCap] + "\n…(truncated)"
	}
	messages := buildSubLLMMessages(buildMediatePrompt(skillID, userText, truncated))
	resp, err := sess.Provider.Generate(ctx, messages)
	if err != nil || resp == nil {
		return rawOutput
	}
	out := strings.TrimSpace(resp.Content)
	if out == "" {
		return rawOutput
	}
	if !mediationPreservesFacts(rawOutput, out) {
		// LLM ignored the raw and fabricated a response from priors.
		// Observed in 2026-04-27: T3 "환율" with raw "1 USD = 1477.04
		// KRW…" yielded a hallucinated "정확한 수치는 가져오지 못했습니다"
		// + spurious install offer. Numeric-overlap zero is the strongest
		// signal that the LLM didn't read the raw — fall back rather than
		// ship the fabrication.
		return rawOutput
	}
	return out
}

// mediationNumberRe captures numeric tokens (with optional decimal) so
// we can verify the LLM response shares at least one number with the
// raw output. Currency symbols, units, and locale-specific separators
// are intentionally not parsed — over-strict matching would false-flag
// legit reformatting (e.g. "1,477원" vs "1477"). The check is a
// fabrication floor, not a unit converter validator.
var mediationNumberRe = regexp.MustCompile(`\d+(?:\.\d+)?`)

// mediationPreservesFacts returns true when the mediated response
// shares at least one numeric token with the raw output (or when the
// raw has no numbers, in which case this guard can't speak). Zero
// overlap means the LLM authored the response from priors instead of
// the raw — a fabrication signature we never want to ship.
func mediationPreservesFacts(raw, mediated string) bool {
	rawNums := mediationNumberRe.FindAllString(raw, -1)
	if len(rawNums) == 0 {
		return true
	}
	medNums := mediationNumberRe.FindAllString(mediated, -1)
	if len(medNums) == 0 {
		return false
	}
	medSet := make(map[string]struct{}, len(medNums))
	for _, n := range medNums {
		medSet[n] = struct{}{}
	}
	for _, n := range rawNums {
		if _, ok := medSet[n]; ok {
			return true
		}
	}
	return false
}

// buildMediatePrompt is the reformat-only contract sent to the
// reframing LLM. Phrased as general rules (no per-skill enumeration)
// so the same prompt works for every installed skill the user might
// dispatch through RunInstalledSkillBranch.
func buildMediatePrompt(skillID, userText, rawOutput string) string {
	var b strings.Builder
	b.WriteString("사용자 query: \"")
	b.WriteString(userText)
	b.WriteString("\"\n\n설치된 스킬 \"")
	b.WriteString(skillID)
	b.WriteString("\" 의 raw 출력:\n---\n")
	b.WriteString(rawOutput)
	b.WriteString("\n---\n\n위 raw 출력을 사용자 query 의 의도에 맞게 정리해 답하세요. 규칙:\n")
	b.WriteString("- raw 의 수치/사실은 변경 X. 그대로 쓰거나 환산 가능한 단위만 환산.\n")
	b.WriteString("- 새 정보 추가 X. 추가 검색/추론 fabrication 금지.\n")
	b.WriteString("- query 의 modifier (단위/언어/scope/verbosity) 를 응답 형식에 반영.\n")
	b.WriteString("- 응답은 reformatted raw 만. 메타 안내 (추가 도움 권유, 스킬 설치 제안, 후속 질문) 금지.\n")
	b.WriteString("- raw 가 query 충족에 *부족할 때만* 정직히 인정 + 다음 행동 제안.\n")
	b.WriteString("- 짧고 자연스러운 비서 톤.")
	return b.String()
}

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

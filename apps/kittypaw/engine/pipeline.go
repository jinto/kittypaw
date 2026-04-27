package engine

import (
	"context"
	"strings"

	"github.com/jinto/kittypaw/core"
)

// IntentKind classifies a user message into a deterministic branch or
// the legacy LLM agent loop. Each non-fallback kind is owned by a single
// Branch implementation. Adding a new behavioral case becomes "add a
// constant + classifier rule + Branch", not "grow the system prompt".
type IntentKind string

const (
	IntentChitchat       IntentKind = "chitchat"
	IntentLegacyFallback IntentKind = "legacy_fallback"
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

// classifyIntent runs the rule-first classifier. Phase 1 only routes
// chitchat; everything else falls back to the legacy LLM agent loop.
// Future phases extend the rule list (browse, install_consent_reply,
// clarify) and add an LLM-fallback for ambiguous cases.
func classifyIntent(text string) Intent {
	t := strings.TrimSpace(text)
	if t == "" {
		return Intent{Kind: IntentLegacyFallback}
	}
	if isChitchat(t) {
		return Intent{Kind: IntentChitchat, Confidence: 1.0}
	}
	return Intent{Kind: IntentLegacyFallback}
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
	intent := classifyIntent(eventText)
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

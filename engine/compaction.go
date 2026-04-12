package engine

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jinto/gopaw/core"
)

// CompactionConfig controls 3-stage context window management.
type CompactionConfig struct {
	RecentWindow int // recent turns kept in full
	MiddleWindow int // middle turns kept but truncated
	TruncateLen  int // max chars for truncated content
}

// DefaultCompaction returns the default compaction settings.
func DefaultCompaction() CompactionConfig {
	return CompactionConfig{
		RecentWindow: 20,
		MiddleWindow: 30,
		TruncateLen:  100,
	}
}

// CompactionForAttempt returns progressively tighter compaction for retries.
func CompactionForAttempt(attempt int) CompactionConfig {
	switch attempt {
	case 0:
		return DefaultCompaction()
	case 1:
		return CompactionConfig{RecentWindow: 10, MiddleWindow: 10, TruncateLen: 50}
	default:
		return CompactionConfig{RecentWindow: 5, MiddleWindow: 0, TruncateLen: 50}
	}
}

// EstimateTokens gives a rough token count. ASCII ~ 1 token/4 chars, CJK ~ 1 token/1.5 chars.
func EstimateTokens(text string) int {
	count := 0
	for _, r := range text {
		if r < 128 {
			count++
		} else {
			count += 2
		}
	}
	return count / 3
}

// CompactTurns applies 3-stage compaction to conversation turns.
//
// Stages:
//   - Old (beyond middle+recent): collapsed into a summary system message
//   - Middle: each turn kept but truncated to TruncateLen chars
//   - Recent (last RecentWindow): full content preserved
func CompactTurns(turns []core.ConversationTurn, cfg CompactionConfig) []core.LlmMessage {
	total := len(turns)
	recentStart := max(0, total-cfg.RecentWindow)
	middleStart := max(0, recentStart-cfg.MiddleWindow)

	oldZone := turns[:middleStart]
	middleZone := turns[middleStart:recentStart]
	recentZone := turns[recentStart:]

	var messages []core.LlmMessage

	// Stage 3: old zone → summary
	if len(oldZone) > 0 {
		messages = append(messages, summarizeOldTurns(oldZone))
	}

	// Stage 2: middle zone → truncated
	for i := range middleZone {
		if msg, ok := turnToMessage(&middleZone[i], cfg.TruncateLen); ok {
			messages = append(messages, msg)
		}
	}

	// Stage 1: recent zone → full
	for i := range recentZone {
		if msg, ok := turnToMessage(&recentZone[i], 0); ok {
			messages = append(messages, msg)
		}
	}

	return messages
}

func turnToMessage(turn *core.ConversationTurn, truncateTo int) (core.LlmMessage, bool) {
	switch turn.Role {
	case core.RoleSystem:
		return core.LlmMessage{}, false
	case core.RoleUser:
		content := turn.Content
		if turn.Result != "" {
			result := turn.Result
			if truncateTo > 0 {
				result = truncate(result, truncateTo)
			}
			content += fmt.Sprintf("\n[Previous result: %s]", result)
		}
		if truncateTo > 0 {
			content = truncate(content, truncateTo)
		}
		return core.LlmMessage{Role: core.RoleUser, Content: content}, true
	case core.RoleAssistant:
		content := turn.Content
		if truncateTo > 0 {
			content = truncate(content, truncateTo)
		}
		return core.LlmMessage{Role: core.RoleAssistant, Content: content}, true
	}
	return core.LlmMessage{}, false
}

func summarizeOldTurns(turns []core.ConversationTurn) core.LlmMessage {
	var userCount, assistantCount, codeCount, successCount, failureCount int

	for i := range turns {
		switch turns[i].Role {
		case core.RoleUser:
			userCount++
		case core.RoleAssistant:
			assistantCount++
			if turns[i].Code != "" {
				codeCount++
			}
			r := strings.ToLower(turns[i].Result)
			if strings.Contains(r, "success") || strings.Contains(r, "output:") {
				successCount++
			} else if strings.Contains(r, "error") || strings.Contains(r, "fail") {
				failureCount++
			}
		}
	}

	total := userCount + assistantCount
	return core.LlmMessage{
		Role: core.RoleSystem,
		Content: fmt.Sprintf(
			"[이전 대화 요약] 지금까지 %d번 대화 (%d번 사용자, %d번 어시스턴트), 코드 실행 %d번, 성공 %d번, 실패 %d번.",
			total, userCount, assistantCount, codeCount, successCount, failureCount,
		),
	}
}

func truncate(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + "…"
}

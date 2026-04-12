package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/jinto/gopaw/core"
)

// tryHandleCommand checks if the event text is a slash command.
// Returns (response, true) if handled, ("", false) otherwise.
func tryHandleCommand(_ context.Context, text string, s *Session) (string, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", false
	}

	parts := strings.Fields(text)
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/help":
		return handleHelp(), true
	case "/status":
		return handleStatus(s), true
	case "/skills":
		return handleSkills(), true
	case "/run":
		if len(parts) > 1 {
			return handleRun(parts[1]), true
		}
		return "사용법: /run <skill-name>", true
	case "/teach":
		if len(parts) > 1 {
			return handleTeach(strings.Join(parts[1:], " ")), true
		}
		return "사용법: /teach <설명>", true
	default:
		return "", false
	}
}

func handleHelp() string {
	return `GoPaw 명령어:
/help — 도움말 표시
/status — 실행 통계 확인
/skills — 스킬 목록
/run <name> — 스킬 실행
/teach <설명> — 새 스킬 학습`
}

func handleStatus(s *Session) string {
	stats, err := s.Store.TodayStats()
	if err != nil {
		return fmt.Sprintf("통계 조회 실패: %s", err)
	}
	return fmt.Sprintf(
		"📊 오늘 실행 통계\n총 실행: %d\n성공: %d\n실패: %d\n토큰: %d",
		stats.TotalRuns, stats.Successful, stats.Failed, stats.TotalTokens,
	)
}

func handleSkills() string {
	skills, err := core.LoadAllSkills()
	if err != nil {
		return fmt.Sprintf("스킬 목록 조회 실패: %s", err)
	}
	if len(skills) == 0 {
		return "등록된 스킬이 없습니다."
	}
	var sb strings.Builder
	sb.WriteString("📋 스킬 목록:\n")
	for _, s := range skills {
		status := "✅"
		if !s.Skill.Enabled {
			status = "⛔"
		}
		sb.WriteString(fmt.Sprintf("  %s %s — %s\n", status, s.Skill.Name, s.Skill.Description))
	}
	return sb.String()
}

func handleRun(name string) string {
	// TODO: dispatch skill execution
	return fmt.Sprintf("스킬 '%s' 실행 요청됨", name)
}

func handleTeach(description string) string {
	// TODO: LLM-based skill generation
	return fmt.Sprintf("스킬 학습 요청됨: %s", description)
}

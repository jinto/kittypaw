package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/jinto/kittypaw/core"
)

// tryHandleCommand checks if the event text is a slash command.
// Returns (response, true) if handled, ("", false) otherwise.
func tryHandleCommand(ctx context.Context, text string, s *Session) (string, bool) {
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
		return handleSkills(s), true
	case "/run":
		if len(parts) > 1 {
			return handleRun(parts[1]), true
		}
		return "사용법: /run <skill-name>", true
	case "/teach":
		if len(parts) > 1 {
			return handleTeach(ctx, strings.Join(parts[1:], " "), s), true
		}
		return "사용법: /teach <설명>", true
	default:
		return "", false
	}
}

func handleHelp() string {
	return `KittyPaw 명령어:
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

func handleSkills(s *Session) string {
	skills, err := core.LoadAllSkillsFrom(s.BaseDir)
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

func handleTeach(ctx context.Context, description string, s *Session) string {
	result, err := HandleTeach(ctx, description, "chat", s)
	if err != nil {
		return fmt.Sprintf("스킬 학습 실패: %s", err)
	}
	if !result.SyntaxOK {
		return fmt.Sprintf("생성된 코드에 구문 오류가 있습니다: %s\n\n코드:\n%s", result.SyntaxError, result.Code)
	}

	// Block auto-approve for skills using dangerous permissions
	for _, perm := range result.Permissions {
		if perm == "Shell" || perm == "File" || perm == "Git" {
			return fmt.Sprintf("생성된 스킬이 위험한 권한(%s)을 사용합니다. API /skills/teach/approve를 통해 수동 승인이 필요합니다.\n\n코드:\n%s", perm, result.Code)
		}
	}

	// Auto-approve for chat entry point (no interactive mechanism for safe skills)
	if err := ApproveSkill(s.BaseDir, result); err != nil {
		return fmt.Sprintf("스킬 저장 실패: %s", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "스킬 '%s' 생성 완료!\n", result.SkillName)
	fmt.Fprintf(&sb, "설명: %s\n", result.Description)
	fmt.Fprintf(&sb, "트리거: %s\n", result.Trigger.Type)
	if len(result.Permissions) > 0 {
		fmt.Fprintf(&sb, "권한: %s\n", strings.Join(result.Permissions, ", "))
	}
	fmt.Fprintf(&sb, "\n코드:\n%s", result.Code)
	return sb.String()
}

package core

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxHistoryTurns = 100

// Role represents who is speaking in a conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// EventType identifies the channel source of an incoming event.
type EventType string

const (
	EventWebChat   EventType = "web_chat"
	EventTelegram  EventType = "telegram"
	EventDesktop   EventType = "desktop"
	EventKakaoTalk EventType = "kakao_talk"
	EventSlack     EventType = "slack"
	EventDiscord   EventType = "discord"
)

// LoopPhase tracks the agent loop state machine position.
type LoopPhase string

const (
	PhaseInit     LoopPhase = "init"
	PhasePrompt   LoopPhase = "prompt"
	PhaseGenerate LoopPhase = "generate"
	PhaseExecute  LoopPhase = "execute"
	PhaseRetry    LoopPhase = "retry"
	PhaseFinish   LoopPhase = "finish"
)

// AgentState holds the mutable runtime state for one agent.
type AgentState struct {
	AgentID      string             `json:"agent_id"`
	SystemPrompt string             `json:"system_prompt"`
	Turns        []ConversationTurn `json:"turns"`
}

// ConversationTurn is a single message in a conversation.
type ConversationTurn struct {
	Role      Role   `json:"role"`
	Content   string `json:"content"`
	Code      string `json:"code,omitempty"`
	Result    string `json:"result,omitempty"`
	Timestamp string `json:"timestamp"`
}

// Event is an inbound message from any channel.
type Event struct {
	Type    EventType       `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ChatPayload is the common structure inside Event.Payload.
type ChatPayload struct {
	ChatID      string `json:"chat_id"`
	Text        string `json:"text"`
	FromName    string `json:"from_name,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
}

// LlmMessage is a single message sent to/from an LLM.
type LlmMessage struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// SkillDefinition describes a skill's interface for the LLM.
type SkillDefinition struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Parameters  []SkillParameter `json:"parameters"`
}

// SkillParameter is one parameter in a skill definition.
type SkillParameter struct {
	Name        string `json:"name"`
	ParamType   string `json:"param_type"`
	Description string `json:"description"`
}

// SkillCall represents a skill invocation captured from sandbox execution.
type SkillCall struct {
	SkillName string            `json:"skill_name"`
	Method    string            `json:"method"`
	Args      []json.RawMessage `json:"args"`
}

// ExecutionResult is the output of a sandbox code execution.
type ExecutionResult struct {
	Success    bool        `json:"success"`
	Output     string      `json:"output"`
	SkillCalls []SkillCall `json:"skill_calls"`
	Error      string      `json:"error,omitempty"`
}

// TransitionReason records why the agent loop moved between phases.
type TransitionReason struct {
	Reason       string `json:"reason"`
	MessageCount int    `json:"message_count,omitempty"`
	CodeLen      int    `json:"code_len,omitempty"`
	OutputLen    int    `json:"output_len,omitempty"`
	SkillCalls   int    `json:"skill_calls,omitempty"`
	Error        string `json:"error,omitempty"`
	Attempt      int    `json:"attempt,omitempty"`
	ActionCount  int    `json:"action_count,omitempty"`
}

// ToEventType maps a channel configuration type to its corresponding event type.
func (ct ChannelType) ToEventType() EventType {
	switch ct {
	case ChannelTelegram:
		return EventTelegram
	case ChannelSlack:
		return EventSlack
	case ChannelDiscord:
		return EventDiscord
	case ChannelWeb:
		return EventWebChat
	case ChannelDesktop:
		return EventDesktop
	case ChannelKakaoTalk:
		return EventKakaoTalk
	default:
		return EventType(ct)
	}
}

// ChannelName returns the human-readable channel name for an event type.
func (t EventType) ChannelName() string {
	switch t {
	case EventTelegram:
		return "telegram"
	case EventSlack:
		return "slack"
	case EventDiscord:
		return "discord"
	case EventWebChat:
		return "web"
	case EventDesktop:
		return "desktop"
	case EventKakaoTalk:
		return "kakao_talk"
	default:
		return string(t)
	}
}

// SplitChunks breaks text into pieces no longer than maxLen.
// It tries to split on newlines, falling back to hard splits.
func SplitChunks(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}
	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}
		cut := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > maxLen/2 {
			cut = idx + 1
		}
		chunks = append(chunks, text[:cut])
		text = text[cut:]
	}
	return chunks
}

// ValidateSkillName checks that a skill name contains only safe characters.
var validSkillName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func ValidateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name is empty")
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("skill name contains path traversal characters: %q", name)
	}
	if !validSkillName.MatchString(name) {
		return fmt.Errorf("skill name contains invalid characters: %q (allowed: a-z, A-Z, 0-9, _, -)", name)
	}
	return nil
}

// IsSecretEnvVar returns true if the variable name likely contains a secret.
func IsSecretEnvVar(name string) bool {
	upper := strings.ToUpper(name)
	for _, pattern := range []string{"KEY", "SECRET", "TOKEN", "PASSWORD", "CREDENTIAL", "AUTH"} {
		if strings.Contains(upper, pattern) {
			return true
		}
	}
	return false
}

// IsPrivateIP returns true if the host resolves to a private/loopback/link-local address.
func IsPrivateIP(host string) bool {
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasPrefix(lower, "127.") || lower == "::1" {
		return true
	}
	// Check common private IP prefixes (heuristic, not full CIDR check).
	for _, prefix := range []string{"10.", "172.16.", "172.17.", "172.18.", "172.19.",
		"172.20.", "172.21.", "172.22.", "172.23.", "172.24.", "172.25.",
		"172.26.", "172.27.", "172.28.", "172.29.", "172.30.", "172.31.",
		"192.168.", "169.254.", "0."} {
		if strings.HasPrefix(host, prefix) {
			return true
		}
	}
	return false
}

// NowTimestamp returns the current Unix epoch seconds as a string.
func NowTimestamp() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

// ParsePayload decodes the Event payload into a ChatPayload.
func (e *Event) ParsePayload() (ChatPayload, error) {
	var p ChatPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return p, fmt.Errorf("parse event payload: %w", err)
	}
	return p, nil
}

package chatrelay

import "encoding/json"

const ProtocolVersion = "1"

const (
	OperationOpenAIModels          = "openai.models"
	OperationOpenAIChatCompletions = "openai.chat_completions"
)

const (
	ScopeChatRelay     = "chat:relay"
	ScopeModelsRead    = "models:read"
	ScopeDaemonConnect = "daemon:connect"
)

const (
	FrameHello           = "hello"
	FrameRequest         = "request"
	FrameResponseHeaders = "response_headers"
	FrameResponseChunk   = "response_chunk"
	FrameResponseEnd     = "response_end"
	FrameError           = "error"
)

type HelloFrame struct {
	Type            string   `json:"type"`
	DeviceID        string   `json:"device_id"`
	LocalAccounts   []string `json:"local_accounts"`
	DaemonVersion   string   `json:"daemon_version"`
	ProtocolVersion string   `json:"protocol_version"`
	Capabilities    []string `json:"capabilities"`
}

type RequestFrame struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Operation string          `json:"operation"`
	AccountID string          `json:"account_id"`
	Body      json.RawMessage `json:"body,omitempty"`
}

type ResponseHeadersFrame struct {
	Type    string            `json:"type"`
	ID      string            `json:"id"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
}

type ResponseChunkFrame struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Data string `json:"data"`
}

type ResponseEndFrame struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type ErrorFrame struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

func DefaultCapabilities() []string {
	return []string{OperationOpenAIModels, OperationOpenAIChatCompletions}
}

func NewHelloFrame(deviceID string, localAccounts []string, daemonVersion string, capabilities []string) HelloFrame {
	return HelloFrame{
		Type:            FrameHello,
		DeviceID:        deviceID,
		LocalAccounts:   append([]string(nil), localAccounts...),
		DaemonVersion:   daemonVersion,
		ProtocolVersion: ProtocolVersion,
		Capabilities:    EffectiveCapabilities(capabilities),
	}
}

func EffectiveCapabilities(capabilities []string) []string {
	if capabilities == nil {
		return DefaultCapabilities()
	}
	return append([]string(nil), capabilities...)
}

func SupportedOperation(operation string) bool {
	switch operation {
	case OperationOpenAIModels, OperationOpenAIChatCompletions:
		return true
	default:
		return false
	}
}

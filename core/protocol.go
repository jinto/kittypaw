package core

// WsClientMsg represents messages from the WebSocket client.
// Discriminated by the "type" field.
type WsClientMsg struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"` // for "chat"
	OK   *bool  `json:"ok,omitempty"`   // for "permit"
}

// WsServerMsg represents messages from the server to WebSocket clients.
// Discriminated by the "type" field.
type WsServerMsg struct {
	Type        string `json:"type"`
	ID          string `json:"id,omitempty"`          // for "session"
	Text        string `json:"text,omitempty"`        // for "token"
	FullText    string `json:"full_text,omitempty"`   // for "done"
	TokensUsed  *int64 `json:"tokens_used,omitempty"` // for "done"
	Message     string `json:"message,omitempty"`     // for "error"
	Description string `json:"description,omitempty"` // for "permission"
	Resource    string `json:"resource,omitempty"`    // for "permission"
}

// WsServerMsg type constants
const (
	WsMsgSession    = "session"
	WsMsgToken      = "token"
	WsMsgDone       = "done"
	WsMsgError      = "error"
	WsMsgPermission = "permission"
)

// WsClientMsg type constants
const (
	WsMsgChat   = "chat"
	WsMsgPermit = "permit"
)

// NewSessionMsg creates a session initialization message.
func NewSessionMsg(id string) WsServerMsg {
	return WsServerMsg{Type: WsMsgSession, ID: id}
}

// NewTokenMsg creates a streaming token message.
func NewTokenMsg(text string) WsServerMsg {
	return WsServerMsg{Type: WsMsgToken, Text: text}
}

// NewDoneMsg creates a completion message.
func NewDoneMsg(fullText string, tokensUsed *int64) WsServerMsg {
	return WsServerMsg{Type: WsMsgDone, FullText: fullText, TokensUsed: tokensUsed}
}

// NewErrorMsg creates an error message.
func NewErrorMsg(message string) WsServerMsg {
	return WsServerMsg{Type: WsMsgError, Message: message}
}

// NewPermissionMsg creates a permission request message.
func NewPermissionMsg(description, resource string) WsServerMsg {
	return WsServerMsg{Type: WsMsgPermission, Description: description, Resource: resource}
}

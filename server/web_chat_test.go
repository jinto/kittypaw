package server

import (
	"os"
	"strings"
	"testing"
)

func TestWebChatUsesCookieForAuthenticatedBrowserSession(t *testing.T) {
	src, err := os.ReadFile("web/chat.js")
	if err != nil {
		t.Fatalf("read web chat: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "App.apiKey && !App.authRequired") {
		t.Fatalf("chat websocket must not append bootstrap token for authenticated browser sessions")
	}
}

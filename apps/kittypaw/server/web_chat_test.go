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

func TestWebChatUsesChatScopedSocketOnChatSurface(t *testing.T) {
	src, err := os.ReadFile("web/chat.js")
	if err != nil {
		t.Fatalf("read web chat: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "App.chatOnly ? '/chat/ws' : '/ws'") {
		t.Fatalf("chat surface must fall back to /chat/ws, got:\n%s", body)
	}
}

func TestWebSettingsDoesNotLaunchSetupWizard(t *testing.T) {
	src, err := os.ReadFile("web/settings.js")
	if err != nil {
		t.Fatalf("read web settings: %v", err)
	}
	body := string(src)
	if strings.Contains(body, "launchWizard") || strings.Contains(body, "Setup Wizard") || strings.Contains(body, "setup wizard") {
		t.Fatalf("settings must not route users into browser onboarding, got:\n%s", body)
	}
	if !strings.Contains(body, "/api/settings/llm") || !strings.Contains(body, "/api/settings/telegram") {
		t.Fatalf("settings must use post-setup settings APIs, got:\n%s", body)
	}
}

func TestWebSettingsManagesAccountWorkspaces(t *testing.T) {
	src, err := os.ReadFile("web/settings.js")
	if err != nil {
		t.Fatalf("read web settings: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "/api/settings/workspaces") {
		t.Fatalf("settings must use account-scoped workspace settings APIs, got:\n%s", body)
	}
	if !strings.Contains(body, "Workspace") || !strings.Contains(body, "Alias") || !strings.Contains(body, "Path") {
		t.Fatalf("settings must expose workspace alias and path controls, got:\n%s", body)
	}
}

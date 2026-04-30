package server

import (
	"os"
	"strings"
	"testing"
)

func TestWebAppApiRawRoutesUnauthorizedBackToLogin(t *testing.T) {
	src, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatalf("read web app: %v", err)
	}
	body := string(src)
	start := strings.Index(body, "async function apiRaw")
	if start < 0 {
		t.Fatal("apiRaw function not found")
	}
	end := strings.Index(body[start:], "\n}\n\n/** Fetch with Bearer auth header. */")
	if end < 0 {
		t.Fatal("apiRaw function end not found")
	}
	apiRaw := body[start : start+end]
	if !strings.Contains(apiRaw, "res.status === 401") || !strings.Contains(apiRaw, "App.showLogin") {
		t.Fatalf("apiRaw must send expired sessions back to login, got:\n%s", apiRaw)
	}
}

func TestWebAppApiRoutesUnauthorizedBackToLogin(t *testing.T) {
	src, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatalf("read web app: %v", err)
	}
	body := string(src)
	start := strings.Index(body, "async function api(")
	if start < 0 {
		t.Fatal("api function not found")
	}
	end := strings.Index(body[start:], "\n}\n\nasync function apiPost")
	if end < 0 {
		t.Fatal("api function end not found")
	}
	api := body[start : start+end]
	if !strings.Contains(api, "res.status === 401") || !strings.Contains(api, "App.showLogin") {
		t.Fatalf("api must send expired sessions back to login, got:\n%s", api)
	}
	if !strings.Contains(api, "res.status === 403") {
		t.Fatalf("api must surface forbidden sessions, got:\n%s", api)
	}
}

func TestWebAppBootstrapDoesNotSwallowUnauthorized(t *testing.T) {
	src, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatalf("read web app: %v", err)
	}
	body := string(src)
	start := strings.Index(body, "async bootstrap()")
	if start < 0 {
		t.Fatal("bootstrap method not found")
	}
	end := strings.Index(body[start:], "\n  },")
	if end < 0 {
		t.Fatal("bootstrap method end not found")
	}
	bootstrap := body[start : start+end]
	if strings.Contains(bootstrap, "catch") && !strings.Contains(bootstrap, "throw") {
		t.Fatalf("bootstrap must not swallow auth failures, got:\n%s", bootstrap)
	}
}

func TestWebAppAuthenticatedAccountsUseSetupStatus(t *testing.T) {
	src, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatalf("read web app: %v", err)
	}
	body := string(src)
	if strings.Contains(body, "auth.is_default === false") {
		t.Fatal("authenticated non-default accounts must not bypass account-scoped setup/status")
	}
	if !strings.Contains(body, "await this.startMainFlow()") {
		t.Fatal("authenticated accounts must enter setup/status flow")
	}
}

func TestWebAppNonDefaultShellHidesDefaultBoundAPITabs(t *testing.T) {
	src, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatalf("read web app: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "const fullShell = !this.authRequired || this.isDefault") {
		t.Fatal("showShell must distinguish default-account UI from non-default chat UI")
	}
	if !strings.Contains(body, "const adminNav = fullShell") {
		t.Fatal("showShell must gate API-backed nav items")
	}
	if !strings.Contains(body, "const wizardButton = fullShell") {
		t.Fatal("showShell must gate default-bound wizard entry")
	}
}

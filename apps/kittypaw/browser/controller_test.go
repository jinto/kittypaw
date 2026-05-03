package browser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
)

type fakeBackend struct {
	calls []string
	tab   tabInfo
}

func (f *fakeBackend) status(context.Context) (StatusResult, error) {
	return StatusResult{Enabled: true, Running: true, Managed: true, ActiveTargetID: f.tab.TargetID}, nil
}

func (f *fakeBackend) open(_ context.Context, rawURL string) (tabInfo, error) {
	f.calls = append(f.calls, "open:"+rawURL)
	f.tab = tabInfo{TargetID: "target-1", URL: rawURL, Title: "Title", Active: true}
	return f.tab, nil
}

func (f *fakeBackend) tabs(context.Context) ([]tabInfo, error) {
	return []tabInfo{f.tab}, nil
}

func (f *fakeBackend) use(_ context.Context, targetID string) (tabInfo, error) {
	f.calls = append(f.calls, "use:"+targetID)
	f.tab.TargetID = targetID
	f.tab.Active = true
	return f.tab, nil
}

func (f *fakeBackend) navigate(_ context.Context, rawURL string) (map[string]any, error) {
	f.calls = append(f.calls, "navigate:"+rawURL)
	f.tab.URL = rawURL
	return map[string]any{"url": rawURL}, nil
}

func (f *fakeBackend) close(_ context.Context, targetID string) error {
	f.calls = append(f.calls, "close:"+targetID)
	return nil
}

func TestControllerExecuteStatusDisabled(t *testing.T) {
	c := NewController(ControllerOptions{
		Config:  core.BrowserConfig{Enabled: false},
		BaseDir: t.TempDir(),
	})
	got, err := c.Execute(context.Background(), core.SkillCall{SkillName: "Browser", Method: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"enabled":false`) {
		t.Fatalf("status = %s", got)
	}
}

func TestControllerExecuteOpenValidatesURL(t *testing.T) {
	fake := &fakeBackend{}
	c := newControllerWithBackend(core.BrowserConfig{Enabled: true}, t.TempDir(), fake)
	got, err := c.Execute(context.Background(), core.SkillCall{
		SkillName: "Browser",
		Method:    "open",
		Args:      []json.RawMessage{json.RawMessage(`"javascript:alert(1)"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "unsupported URL scheme") {
		t.Fatalf("got %s", got)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("backend should not be called: %v", fake.calls)
	}
}

func TestControllerExecuteOpen(t *testing.T) {
	fake := &fakeBackend{}
	c := newControllerWithBackend(core.BrowserConfig{Enabled: true}, t.TempDir(), fake)
	got, err := c.Execute(context.Background(), core.SkillCall{
		SkillName: "Browser",
		Method:    "open",
		Args:      []json.RawMessage{json.RawMessage(`"https://example.com"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"target_id":"target-1"`) {
		t.Fatalf("got %s", got)
	}
	if fake.calls[0] != "open:https://example.com" {
		t.Fatalf("calls = %v", fake.calls)
	}
}

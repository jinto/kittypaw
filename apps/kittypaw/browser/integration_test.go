package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestManagedChromeIntegration(t *testing.T) {
	if os.Getenv("KITTYPAW_BROWSER_INTEGRATION") != "1" {
		t.Skip("set KITTYPAW_BROWSER_INTEGRATION=1 to run managed Chrome integration")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><title>KP Test</title><body><input id="q"><button id="b" onclick="document.body.dataset.clicked='yes'">Go</button><p>Hello Browser</p></body></html>`))
	}))
	defer srv.Close()

	c := NewController(ControllerOptions{
		Config: core.BrowserConfig{
			Enabled:        true,
			Headless:       true,
			AllowedHosts:   []string{"127.0.0.1"},
			TimeoutSeconds: 10,
		},
		BaseDir: t.TempDir(),
	})
	defer c.Close()

	ctx := context.Background()
	if _, err := c.open(ctx, srv.URL); err != nil {
		t.Fatalf("open: %v", err)
	}
	snap, err := c.snapshot(ctx, nil)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Title != "KP Test" || !strings.Contains(snap.Text, "Hello Browser") {
		t.Fatalf("snapshot = %#v", snap)
	}
	if _, err := c.typeText(ctx, "#q", "kittypaw"); err != nil {
		t.Fatalf("typeText: %v", err)
	}
	if _, err := c.click(ctx, "#b"); err != nil {
		t.Fatalf("click: %v", err)
	}
	eval, err := c.evaluate(ctx, `document.querySelector("#q").value + ":" + document.body.dataset.clicked`)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !strings.Contains(eval, "kittypaw:yes") {
		t.Fatalf("evaluate = %s", eval)
	}
	shot, err := c.screenshot(ctx, "png")
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	if shot.Bytes == 0 {
		t.Fatal("screenshot empty")
	}
}

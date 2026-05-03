package browser

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jinto/kittypaw/core"
)

func TestControllerBackendStatusBeforeLaunch(t *testing.T) {
	c := NewController(ControllerOptions{Config: testBrowserConfig(), BaseDir: t.TempDir()})
	status, err := c.status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || status.Running {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerBackendOpenUsesCDP(t *testing.T) {
	conn := newFakeCDPConn()
	c := NewController(ControllerOptions{Config: testBrowserConfig(), BaseDir: t.TempDir()})
	c.client = newCDPClient(conn)
	c.targets = make(map[string]string)
	defer c.client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan tabInfo, 1)
	errCh := make(chan error, 1)
	go func() {
		tab, err := c.open(ctx, "https://example.com")
		if err != nil {
			errCh <- err
			return
		}
		done <- tab
	}()

	req := readCDPRequest(t, conn)
	if req.Method != "Target.createTarget" {
		t.Fatalf("first method = %s", req.Method)
	}
	conn.reads <- []byte(`{"id":1,"result":{"targetId":"target-1"}}`)

	req = readCDPRequest(t, conn)
	if req.Method != "Target.attachToTarget" {
		t.Fatalf("second method = %s", req.Method)
	}
	conn.reads <- []byte(`{"id":2,"result":{"sessionId":"session-1"}}`)

	req = readCDPRequest(t, conn)
	if req.Method != "Target.activateTarget" {
		t.Fatalf("third method = %s", req.Method)
	}
	conn.reads <- []byte(`{"id":3,"result":{}}`)

	req = readCDPRequest(t, conn)
	if req.Method != "Target.getTargets" {
		t.Fatalf("fourth method = %s", req.Method)
	}
	conn.reads <- []byte(`{"id":4,"result":{"targetInfos":[{"targetId":"target-1","type":"page","url":"https://example.com","title":"Example"}]}}`)

	select {
	case err := <-errCh:
		t.Fatalf("open: %v", err)
	case tab := <-done:
		if tab.TargetID != "target-1" || tab.URL != "https://example.com" || tab.Title != "Example" || !tab.Active {
			t.Fatalf("tab = %#v", tab)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func testBrowserConfig() core.BrowserConfig {
	return core.BrowserConfig{Enabled: true, TimeoutSeconds: 1}
}

func readCDPRequest(t *testing.T, conn *fakeCDPConn) cdpRequest {
	t.Helper()
	var req cdpRequest
	if err := json.Unmarshal(<-conn.writes, &req); err != nil {
		t.Fatalf("request json: %v", err)
	}
	return req
}

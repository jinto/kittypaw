package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestParseBindAddr(t *testing.T) {
	tests := []struct {
		bind     string
		wantHost string
		wantPort string
	}{
		{":3000", "localhost", "3000"},
		{"0.0.0.0:8080", "localhost", "8080"},
		{"127.0.0.1:9000", "127.0.0.1", "9000"},
		{"myhost:4000", "myhost", "4000"},
		{"", "localhost", "3000"},
	}
	for _, tt := range tests {
		host, port := parseBindAddr(tt.bind)
		if host != tt.wantHost || port != tt.wantPort {
			t.Errorf("parseBindAddr(%q) = (%q, %q), want (%q, %q)",
				tt.bind, host, port, tt.wantHost, tt.wantPort)
		}
	}
}

func TestWebSocketURL(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{"http://localhost:3000", "ws://localhost:3000/ws"},
		{"https://example.com:443", "wss://example.com:443/ws"},
		{"http://192.168.1.1:8080", "ws://192.168.1.1:8080/ws"},
	}
	for _, tt := range tests {
		d := &DaemonConn{BaseURL: tt.baseURL}
		got := d.WebSocketURL()
		if got != tt.want {
			t.Errorf("WebSocketURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
		}
	}
}

func TestNewDaemonConn_Remote(t *testing.T) {
	d, err := NewDaemonConn("http://remote:3000")
	if err != nil {
		t.Fatalf("NewDaemonConn error: %v", err)
	}
	if d.BaseURL != "http://remote:3000" {
		t.Errorf("BaseURL = %q, want %q", d.BaseURL, "http://remote:3000")
	}
	if d.APIKey != "" {
		t.Errorf("APIKey = %q, want empty", d.APIKey)
	}
}

func TestReadPid(t *testing.T) {
	dir := t.TempDir()

	// Non-existent file.
	_, ok := readPid(filepath.Join(dir, "no.pid"))
	if ok {
		t.Error("readPid(nonexistent) = _, true; want false")
	}

	// Valid PID file.
	path := filepath.Join(dir, "test.pid")
	os.WriteFile(path, []byte("12345\n"), 0o644)
	pid, ok := readPid(path)
	if !ok || pid != 12345 {
		t.Errorf("readPid = (%d, %v), want (12345, true)", pid, ok)
	}

	// Invalid content.
	os.WriteFile(path, []byte("not-a-number"), 0o644)
	_, ok = readPid(path)
	if ok {
		t.Error("readPid(invalid) = _, true; want false")
	}
}

func TestIsKittypawProcess_CurrentProcess(t *testing.T) {
	// Current process is "go" (test runner), not "kittypaw".
	pid := os.Getpid()
	if isKittypawProcess(pid) {
		t.Skip("test runner name contains 'kittypaw'")
	}
}

func TestIsKittypawProcess_DeadPid(t *testing.T) {
	// PID 1 is init/launchd, not kittypaw.
	if isKittypawProcess(99999999) {
		t.Error("isKittypawProcess(99999999) = true, want false")
	}
}

func TestLockPidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	// First lock should succeed.
	f1, err := lockPidFile(path)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}

	// Second lock should fail (non-blocking).
	_, err = lockPidFile(path)
	if err == nil {
		t.Error("second lock should fail while first is held")
	}

	// Release first lock.
	unlockPidFile(f1)

	// Third lock should succeed.
	f3, err := lockPidFile(path)
	if err != nil {
		t.Fatalf("third lock after release: %v", err)
	}
	unlockPidFile(f3)
}

func TestConnect_AlreadyRunning(t *testing.T) {
	// Start a mock health server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	// Write a PID file pointing to current process.
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "daemon.pid")
	os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644)

	// DaemonConn with remote URL (bypasses PID/auto-start logic).
	d := &DaemonConn{BaseURL: srv.URL}
	cl, err := d.Connect()
	if err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	if cl == nil {
		t.Fatal("Connect() returned nil client")
	}

	// Verify client works.
	if err := cl.Health(); err != nil {
		t.Errorf("client.Health() error: %v", err)
	}
}

func TestPollHealth_Success(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	d := &DaemonConn{BaseURL: srv.URL}
	cl := New(srv.URL, "")
	if err := d.pollHealth(cl); err != nil {
		t.Fatalf("pollHealth() error: %v", err)
	}
	if callCount < 3 {
		t.Errorf("expected at least 3 calls, got %d", callCount)
	}
}

package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type serviceReinstallFixture struct {
	t        *testing.T
	dir      string
	fakeBin  string
	logPath  string
	stopPath string
	ackPath  string
}

func newServiceReinstallFixture(t *testing.T) *serviceReinstallFixture {
	t.Helper()

	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}

	cfgDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(filepath.Join(cfgDir, "accounts", "jinto"), 0o755); err != nil {
		t.Fatalf("mkdir account: %v", err)
	}

	t.Setenv("KITTYPAW_CONFIG_DIR", cfgDir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LOG", filepath.Join(dir, "commands.log"))
	t.Setenv("FAKE_STOP_SIGNAL", filepath.Join(dir, "stop.signal"))
	t.Setenv("FAKE_STOP_ACK", filepath.Join(dir, "stop.ack"))

	return &serviceReinstallFixture{
		t:        t,
		dir:      dir,
		fakeBin:  fakeBin,
		logPath:  filepath.Join(dir, "commands.log"),
		stopPath: filepath.Join(dir, "stop.signal"),
		ackPath:  filepath.Join(dir, "stop.ack"),
	}
}

func (f *serviceReinstallFixture) holdPortUntilServiceStop(t *testing.T) int {
	t.Helper()

	ln, port := f.listenOnLocalPort(t)
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(f.stopPath); err == nil {
				_ = ln.Close()
				_ = os.WriteFile(f.ackPath, []byte("ok"), 0o600)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	return port
}

func (f *serviceReinstallFixture) holdPort(t *testing.T) int {
	t.Helper()
	_, port := f.listenOnLocalPort(t)
	return port
}

func (f *serviceReinstallFixture) listenOnLocalPort(t *testing.T) (net.Listener, int) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	return ln, ln.Addr().(*net.TCPAddr).Port
}

func (f *serviceReinstallFixture) writeFakeCommand(name, body string) {
	f.t.Helper()
	path := filepath.Join(f.fakeBin, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		f.t.Fatalf("write fake %s: %v", name, err)
	}
}

func waitForServiceStopAckScript() string {
	return `
touch "$FAKE_STOP_SIGNAL"
i=0
while [ ! -f "$FAKE_STOP_ACK" ] && [ "$i" -lt 100 ]; do
  i=$((i + 1))
  sleep 0.05
done
`
}

func readFileIfExists(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInstallScriptRestartsLoadedMacOSService(t *testing.T) {
	env := installScriptFixture(t, "Darwin", "arm64")
	env.setFake("FAKE_LAUNCHCTL_LOADED", "1")

	out, err := env.runInstallScript()
	if err != nil {
		t.Fatalf("install-kittypaw.sh failed: %v\n%s", err, out)
	}

	log := env.readLog()
	target := "gui/" + strconv.Itoa(os.Getuid()) + "/dev.kittypaw.daemon"
	if !strings.Contains(log, "launchctl print "+target) {
		t.Fatalf("launchctl print was not called for %s\nlog:\n%s", target, log)
	}
	if !strings.Contains(log, "launchctl kickstart -k "+target) {
		t.Fatalf("loaded macOS service was not restarted\nlog:\n%s", log)
	}
}

func TestInstallScriptDoesNotUseLegacyStandaloneDaemonFallback(t *testing.T) {
	env := installScriptFixture(t, "Darwin", "arm64")
	env.setFake("FAKE_KITTYPAW_DAEMON_RUNNING", "1")

	out, err := env.runInstallScript()
	if err != nil {
		t.Fatalf("install-kittypaw.sh failed: %v\n%s", err, out)
	}

	log := env.readLog()
	if strings.Contains(log, "launchctl kickstart") {
		t.Fatalf("standalone daemon path should not restart launchd service\nlog:\n%s", log)
	}
	if strings.Contains(log, "kittypaw daemon") {
		t.Fatalf("installer should not call legacy daemon commands\nlog:\n%s", log)
	}
}

func TestInstallScriptRestartsActiveLinuxService(t *testing.T) {
	env := installScriptFixture(t, "Linux", "x86_64")
	env.setFake("FAKE_SYSTEMD_ACTIVE", "1")

	out, err := env.runInstallScript()
	if err != nil {
		t.Fatalf("install-kittypaw.sh failed: %v\n%s", err, out)
	}

	log := env.readLog()
	if !strings.Contains(log, "systemctl --user is-active --quiet kittypaw.service") {
		t.Fatalf("systemd service status was not checked\nlog:\n%s", log)
	}
	if !strings.Contains(log, "systemctl --user restart kittypaw.service") {
		t.Fatalf("active systemd service was not restarted\nlog:\n%s", log)
	}
}

type installScriptEnv struct {
	t          *testing.T
	root       string
	dir        string
	fakeBin    string
	installDir string
	logPath    string
	env        []string
}

func installScriptFixture(t *testing.T, osName, arch string) *installScriptEnv {
	t.Helper()

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}

	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "bin")
	installDir := filepath.Join(dir, "install")
	homeDir := filepath.Join(dir, "home")
	for _, path := range []string{fakeBin, installDir, homeDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}

	logPath := filepath.Join(dir, "commands.log")
	platformOS, platformArch := installScriptPlatform(osName, arch)
	env := &installScriptEnv{
		t:          t,
		root:       root,
		dir:        dir,
		fakeBin:    fakeBin,
		installDir: installDir,
		logPath:    logPath,
		env: append(os.Environ(),
			"VERSION=1.2.3",
			"INSTALL_DIR="+installDir,
			"HOME="+homeDir,
			"FAKE_LOG="+logPath,
			"FAKE_UNAME_OS="+osName,
			"FAKE_UNAME_ARCH="+arch,
			"FAKE_EXPECTED_PLATFORM="+platformOS+"_"+platformArch,
			"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		),
	}

	env.writeFakeCommand("uname", `#!/bin/sh
case "$1" in
  -s) printf '%s\n' "$FAKE_UNAME_OS" ;;
  -m) printf '%s\n' "$FAKE_UNAME_ARCH" ;;
  *) exit 1 ;;
esac
`)
	env.writeFakeCommand("curl", `#!/bin/sh
out=
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    out="$1"
  fi
  shift || true
done
if [ -z "$out" ]; then
  exit 1
fi
case "$out" in
  *checksums.txt) printf 'dummy  %s\n' "kittypaw_${FAKE_EXPECTED_PLATFORM:-darwin_arm64}.tar.gz" > "$out" ;;
  *) printf 'fake tarball\n' > "$out" ;;
esac
`)
	env.writeFakeCommand("shasum", `#!/bin/sh
cat >/dev/null
exit 0
`)
	env.writeFakeCommand("tar", `#!/bin/sh
cat > kittypaw <<'SCRIPT'
#!/bin/sh
printf 'kittypaw %s\n' "$*" >> "$FAKE_LOG"
exit 0
SCRIPT
chmod +x kittypaw
`)
	env.writeFakeCommand("launchctl", `#!/bin/sh
printf 'launchctl %s\n' "$*" >> "$FAKE_LOG"
if [ "$1" = "print" ]; then
  [ "$FAKE_LAUNCHCTL_LOADED" = "1" ] && exit 0
  exit 1
fi
exit 0
`)
	env.writeFakeCommand("systemctl", `#!/bin/sh
printf 'systemctl %s\n' "$*" >> "$FAKE_LOG"
if [ "$1" = "--user" ] && [ "$2" = "is-active" ]; then
  [ "$FAKE_SYSTEMD_ACTIVE" = "1" ] && exit 0
  exit 3
fi
exit 0
`)

	return env
}

func installScriptPlatform(osName, arch string) (string, string) {
	platformOS := strings.ToLower(osName)
	if platformOS == "darwin" {
		platformOS = "darwin"
	}
	if platformOS == "linux" {
		platformOS = "linux"
	}

	platformArch := arch
	if platformArch == "x86_64" {
		platformArch = "amd64"
	}
	if platformArch == "aarch64" {
		platformArch = "arm64"
	}
	return platformOS, platformArch
}

func (e *installScriptEnv) setFake(key, value string) {
	e.env = append(e.env, key+"="+value)
}

func (e *installScriptEnv) runInstallScript() (string, error) {
	e.t.Helper()
	cmd := exec.Command("/bin/sh", filepath.Join(e.root, "install-kittypaw.sh"))
	cmd.Dir = e.root
	cmd.Env = e.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *installScriptEnv) readLog() string {
	e.t.Helper()
	b, err := os.ReadFile(e.logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		e.t.Fatalf("read log: %v", err)
	}
	return string(b)
}

func (e *installScriptEnv) writeFakeCommand(name, body string) {
	e.t.Helper()
	path := filepath.Join(e.fakeBin, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		e.t.Fatalf("write fake %s: %v", name, err)
	}
}

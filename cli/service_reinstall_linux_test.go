//go:build linux

package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestServiceInstallCommandStopsExistingLinuxServiceBeforePortPreflight(t *testing.T) {
	fx := newServiceReinstallFixture(t)
	port := fx.holdPortUntilServiceStop(t)
	fx.writeFakeCommand("systemctl", `#!/bin/sh
printf 'systemctl %s\n' "$*" >> "$FAKE_LOG"
if [ "$1" = "--user" ] && [ "$2" = "stop" ]; then
`+waitForServiceStopAckScript()+`
  exit 0
fi
if [ "$1" = "--user" ] && [ "$2" = "show" ] && [ "$3" = "-p" ]; then
  printf 'yes\n'
  exit 0
fi
exit 0
`)
	fx.writeFakeCommand("loginctl", `#!/bin/sh
exit 1
`)

	cmd := newServiceInstallCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--bind-port", strconv.Itoa(port)})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("server install command returned error: %v\nstdout:\n%s\nstderr:\n%s", err, out.String(), errOut.String())
	}

	log := readFileIfExists(t, fx.logPath)
	if !strings.Contains(log, "systemctl --user stop kittypaw.service") {
		t.Fatalf("existing service was not stopped before reinstall\nlog:\n%s", log)
	}
}

func TestServiceInstallCommandStillRejectsLinuxPortHeldByAnotherProcess(t *testing.T) {
	fx := newServiceReinstallFixture(t)
	port := fx.holdPort(t)
	fx.writeFakeCommand("systemctl", `#!/bin/sh
printf 'systemctl %s\n' "$*" >> "$FAKE_LOG"
exit 0
`)

	cmd := newServiceInstallCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--bind-port", strconv.Itoa(port)})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("server install command unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", out.String(), errOut.String())
	}
	if !strings.Contains(err.Error(), "port 127.0.0.1:") {
		t.Fatalf("error = %v, want port conflict", err)
	}
}

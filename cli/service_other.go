//go:build !linux && !darwin

package main

import (
	"fmt"
	"io"
	"runtime"
)

func serviceInstall(stdout, stderr io.Writer, f *serviceFlags) error {
	return fmt.Errorf("kittypaw service is not supported on %s yet", runtime.GOOS)
}

func serviceUninstall(stdout, stderr io.Writer) error {
	return fmt.Errorf("kittypaw service is not supported on %s yet", runtime.GOOS)
}

func serviceStatus(stdout, stderr io.Writer) error {
	return fmt.Errorf("kittypaw service is not supported on %s yet", runtime.GOOS)
}

func serviceLogs(stdout, stderr io.Writer, follow bool) error {
	return fmt.Errorf("kittypaw service is not supported on %s yet", runtime.GOOS)
}

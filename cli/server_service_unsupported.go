//go:build !linux && !darwin

package main

import (
	"io"

	"github.com/spf13/cobra"
)

func serverServiceSupported() bool {
	return false
}

func addServerServiceCommands(_ *cobra.Command) {}

func installServerServiceFromSetup(_, _ io.Writer) error {
	return nil
}

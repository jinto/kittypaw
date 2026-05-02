//go:build linux || darwin

package main

import (
	"io"

	"github.com/spf13/cobra"
)

func serverServiceSupported() bool {
	return true
}

func addServerServiceCommands(cmd *cobra.Command) {
	cmd.AddCommand(
		newServiceInstallCmd(),
		newServiceUninstallCmd(),
		newServiceStatusCmd(),
		newServiceLogsCmd(),
	)
}

func installServerServiceFromSetup(stdout, stderr io.Writer) error {
	return serviceInstall(stdout, stderr, &serviceFlags{
		bindHost: "127.0.0.1",
		bindPort: 3000,
	})
}

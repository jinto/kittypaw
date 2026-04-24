// Package packaging bundles per-user init-system templates that the CLI
// writes to ~/.config/systemd/user (Linux) or ~/Library/LaunchAgents (macOS)
// when the user runs `kittypaw service install`.
//
// Templates are kept as text source under packaging/{linux,macos}/ so package
// maintainers (Homebrew, AUR, .deb) can inspect or ship them verbatim; the
// CLI reads the same bytes via go:embed to avoid a runtime file lookup and
// keep the binary self-contained.
package packaging

import _ "embed"

//go:embed linux/systemd/user/kittypaw.service
var LinuxSystemdUnit string

//go:embed macos/LaunchAgents/dev.kittypaw.daemon.plist
var MacOSLaunchAgent string

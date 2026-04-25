//go:build windows

package engine

import "os"

// Windows has no portable equivalent of O_NOFOLLOW (NTFS reparse points
// require GENERIC_READ + FILE_FLAG_OPEN_REPARSE_POINT via syscall, which
// the project does not depend on). The symlink TOCTOU window degrades to
// whatever filepath.EvalSymlinks already caught at validation time.
// Acceptable trade-off for kittypaw's production targets (Linux/macOS).
func openNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY, 0)
}

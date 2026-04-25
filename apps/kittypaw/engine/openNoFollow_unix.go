//go:build !windows

package engine

import (
	"os"
	"syscall"
)

// openNoFollow opens path read-only with O_NOFOLLOW so a symlink swap
// between EvalSymlinks validation and the open call cannot redirect the
// read to a different file. POSIX-only — see the windows variant for the
// fallback.
func openNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}

//go:build !windows

package core

import "syscall"

// isMultiHardlink reports whether the os.FileInfo.Sys() value indicates the
// inode is referenced by more than one directory entry — the signal that a
// hardlink may have been planted to escape a tenant's BaseDir. POSIX hosts
// expose this via syscall.Stat_t.Nlink.
func isMultiHardlink(sys any) bool {
	stat, ok := sys.(*syscall.Stat_t)
	if !ok {
		return false
	}
	return stat.Nlink > 1
}

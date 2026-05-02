//go:build windows

package core

// isMultiHardlink reports whether sys indicates the inode is referenced by
// more than one directory entry. On Windows the os.FileInfo.Sys() returns
// *syscall.Win32FileAttributeData which does NOT expose a hardlink count;
// querying it would require a separate FindFirstFileNameW call, which the
// project does not depend on. Hardlink-escape protection is therefore
// skipped on Windows — an acceptable trade-off because kittypaw's
// production targets are Linux and macOS, and the symlink defense above
// (filepath.EvalSymlinks) still catches the more common escape vector.
func isMultiHardlink(sys any) bool {
	_ = sys
	return false
}

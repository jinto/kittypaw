package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cross-account read errors. These are sentinels so the sandbox (and
// future audit log) can distinguish policy rejections from filesystem
// errors.
var (
	ErrCrossAccountPath           = errors.New("cross-account read: invalid path")
	ErrCrossAccountAbsolute       = errors.New("cross-account read: absolute path not permitted")
	ErrCrossAccountTraversal      = errors.New("cross-account read: path traversal rejected")
	ErrCrossAccountUnauthorized   = errors.New("cross-account read: reader account is not a team space member")
	ErrCrossAccountNotAllowlisted = errors.New("cross-account read: path not in share allowlist")
	ErrCrossAccountNotShareable   = errors.New("cross-account read: path is not in team-space shareable data")
	ErrCrossAccountBoundary       = errors.New("cross-account read: symlink escapes account boundary")
	ErrCrossAccountHardlink       = errors.New("cross-account read: hardlink multi-reference rejected")
	ErrCrossAccountNotFound       = errors.New("cross-account read: file not found")
)

// ValidateSharedReadPath returns the canonical filesystem path of a file the
// reader account is permitted to read from owner's directory, or one of the
// Err* sentinels. This is the single chokepoint for every cross-account read —
// no caller is allowed to bypass it with its own check.
//
// Checks run cheapest-first so hostile input never reaches the filesystem:
//
//  1. Null bytes and empty strings.
//  2. Absolute paths — only account-relative input allowed.
//  3. `..` traversal after Clean.
//  4. Team-space membership and shareable data root lookup.
//  5. realpath symlink escape — EvalSymlinks + baseDir prefix check.
//  6. Hardlink guard — nlink>1 rejects any file with a second reference,
//     because a hardlink from outside boundaryBase to an inode inside boundaryBase
//     cannot be detected via realpath alone (both paths resolve to the
//     same inode inside boundaryBase).
func ValidateSharedReadPath(ownerCfg *Config, ownerBaseDir, readerAccountID, reqPath string) (string, error) {
	if reqPath == "" || strings.ContainsRune(reqPath, 0) {
		return "", ErrCrossAccountPath
	}
	if filepath.IsAbs(reqPath) {
		return "", ErrCrossAccountAbsolute
	}
	cleaned := filepath.Clean(reqPath)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrCrossAccountTraversal
	}

	if ownerCfg == nil || !ownerCfg.IsTeamSpaceAccount() || !ownerCfg.TeamSpaceHasMember(readerAccountID) {
		return "", ErrCrossAccountUnauthorized
	}

	abs, boundaryBase, err := resolveTeamSpaceSharedPath(ownerCfg, ownerBaseDir, cleaned)
	if err != nil {
		return "", err
	}

	realBase, err := filepath.EvalSymlinks(boundaryBase)
	if err != nil {
		return "", fmt.Errorf("realpath baseDir: %w", err)
	}
	realFile, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrCrossAccountNotFound
		}
		return "", fmt.Errorf("realpath %s: %w", abs, err)
	}
	realBase = filepath.Clean(realBase)
	realFile = filepath.Clean(realFile)
	if realFile != realBase && !strings.HasPrefix(realFile, realBase+string(filepath.Separator)) {
		return "", ErrCrossAccountBoundary
	}

	info, err := os.Lstat(realFile)
	if err != nil {
		return "", fmt.Errorf("lstat %s: %w", realFile, err)
	}
	if isMultiHardlink(info.Sys()) {
		return "", ErrCrossAccountHardlink
	}

	return realFile, nil
}

func resolveTeamSpaceSharedPath(ownerCfg *Config, ownerBaseDir, cleaned string) (abs string, boundaryBase string, err error) {
	parts := strings.Split(cleaned, string(filepath.Separator))
	if len(parts) == 0 {
		return "", "", ErrCrossAccountNotShareable
	}
	switch parts[0] {
	case "memory":
		return filepath.Join(ownerBaseDir, cleaned), filepath.Join(ownerBaseDir, "memory"), nil
	case "workspace":
		if len(parts) < 3 {
			return "", "", ErrCrossAccountNotShareable
		}
		alias := parts[1]
		rel := filepath.Join(parts[2:]...)
		for _, root := range ownerCfg.WorkspaceRoots() {
			if root.Alias == alias && root.Path != "" {
				return filepath.Join(root.Path, rel), root.Path, nil
			}
		}
		return "", "", ErrCrossAccountNotShareable
	default:
		return "", "", ErrCrossAccountNotShareable
	}
}

package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cross-tenant read errors. These are sentinels so the sandbox (and
// future audit log) can distinguish policy rejections from filesystem
// errors — a "not in allowlist" message to the skill author is actionable,
// while a "boundary escape" message signals a real security event that
// operations should page on.
var (
	ErrCrossTenantPath           = errors.New("cross-tenant read: invalid path")
	ErrCrossTenantAbsolute       = errors.New("cross-tenant read: absolute path not permitted")
	ErrCrossTenantTraversal      = errors.New("cross-tenant read: path traversal rejected")
	ErrCrossTenantUnauthorized   = errors.New("cross-tenant read: reader tenant not listed in share")
	ErrCrossTenantNotAllowlisted = errors.New("cross-tenant read: path not in share allowlist")
	ErrCrossTenantBoundary       = errors.New("cross-tenant read: symlink escapes tenant boundary")
	ErrCrossTenantHardlink       = errors.New("cross-tenant read: hardlink multi-reference rejected")
	ErrCrossTenantNotFound       = errors.New("cross-tenant read: file not found")
)

// ValidateSharedReadPath returns the canonical filesystem path of a file the
// reader tenant is permitted to read from owner's directory, or one of the
// Err* sentinels. This is the single chokepoint for every cross-tenant read —
// no caller is allowed to bypass it with its own check.
//
// Checks run cheapest-first so hostile input never reaches the filesystem:
//
//  1. Null bytes and empty strings.
//  2. Absolute paths — only tenant-relative input allowed.
//  3. `..` traversal after Clean.
//  4. Allowlist lookup — cleaned paths compared so `memory/../memory/x`
//     collapses to `memory/x` before the match.
//  5. realpath symlink escape — EvalSymlinks + baseDir prefix check.
//  6. Hardlink guard — nlink>1 rejects any file with a second reference,
//     because a hardlink from outside baseDir to an inode inside baseDir
//     cannot be detected via realpath alone (both paths resolve to the
//     same inode inside baseDir).
func ValidateSharedReadPath(ownerCfg *Config, ownerBaseDir, readerTenantID, reqPath string) (string, error) {
	if reqPath == "" || strings.ContainsRune(reqPath, 0) {
		return "", ErrCrossTenantPath
	}
	if filepath.IsAbs(reqPath) {
		return "", ErrCrossTenantAbsolute
	}
	cleaned := filepath.Clean(reqPath)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrCrossTenantTraversal
	}

	if ownerCfg == nil || ownerCfg.Share == nil {
		return "", ErrCrossTenantUnauthorized
	}
	share, ok := ownerCfg.Share[readerTenantID]
	if !ok {
		return "", ErrCrossTenantUnauthorized
	}

	allowed := false
	for _, p := range share.Read {
		if filepath.Clean(p) == cleaned {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", ErrCrossTenantNotAllowlisted
	}

	abs := filepath.Join(ownerBaseDir, cleaned)

	realBase, err := filepath.EvalSymlinks(ownerBaseDir)
	if err != nil {
		return "", fmt.Errorf("realpath baseDir: %w", err)
	}
	realFile, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrCrossTenantNotFound
		}
		return "", fmt.Errorf("realpath %s: %w", abs, err)
	}
	realBase = filepath.Clean(realBase)
	realFile = filepath.Clean(realFile)
	if realFile != realBase && !strings.HasPrefix(realFile, realBase+string(filepath.Separator)) {
		return "", ErrCrossTenantBoundary
	}

	info, err := os.Lstat(realFile)
	if err != nil {
		return "", fmt.Errorf("lstat %s: %w", realFile, err)
	}
	if isMultiHardlink(info.Sys()) {
		return "", ErrCrossTenantHardlink
	}

	return realFile, nil
}

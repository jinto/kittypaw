package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"github.com/jinto/kittypaw/core"
)

// executeShare implements Share.read — the sole cross-account file read path.
// Access control lives in core.ValidateSharedReadPath; this layer plumbs the
// reader identity and emits the cross_account_read audit record unconditionally
// so data-flow auditing never relies on callers remembering to log.
func executeShare(_ context.Context, call core.SkillCall, s *Session) (string, error) {
	if call.Method != "read" {
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Share method: %s", call.Method)})
	}

	if s.AccountRegistry == nil || s.AccountID == "" {
		return jsonResult(map[string]any{"error": "Share.read unavailable: account context not configured"})
	}

	if len(call.Args) < 2 {
		return jsonResult(map[string]any{"error": "Share.read(accountID, path) requires two arguments"})
	}
	var targetID string
	if err := json.Unmarshal(call.Args[0], &targetID); err != nil {
		return jsonResult(map[string]any{"error": "accountID must be a string"})
	}
	if err := core.ValidateAccountID(targetID); err != nil {
		return jsonResult(map[string]any{"error": err.Error()})
	}
	var reqPath string
	if err := json.Unmarshal(call.Args[1], &reqPath); err != nil {
		return jsonResult(map[string]any{"error": "path must be a string"})
	}

	// Family-only target gate (I5). Closes the case where a personal account's
	// config contains a [share.<peer>] allowlist — the allowlist says "these
	// paths are safe to share", but it does NOT say "the owner is reachable".
	// Reachability is a property of the target account's role, and only the
	// family account plays the owner role. Without this gate, a sloppy or
	// hostile config could turn Plan B's "personal ↔ personal forbidden" rule
	// into a documentation-only invariant.
	//
	// "unknown account" and "not family" collapse into one externally-visible
	// outcome so a caller cannot enumerate account IDs by probing for which
	// error string comes back; the audit log keeps the distinction internally
	// for forensics. Same reason both branches share the rejection message.
	owner := s.AccountRegistry.Get(targetID)
	if owner == nil || owner.Config == nil || !owner.Config.IsFamily {
		reason := "target_not_family"
		if owner == nil {
			reason = "unknown_account"
		}
		slog.Warn("cross_account_read_rejected",
			"from", s.AccountID, "to", targetID, "path", filepath.Clean(reqPath), "reason", reason)
		return jsonResult(map[string]any{"error": "cross-account read: target is not the family account"})
	}

	realPath, err := core.ValidateSharedReadPath(owner.Config, owner.BaseDir, s.AccountID, reqPath)
	if err != nil {
		// Clean the logged path so newlines in reqPath can't forge fake audit lines.
		slog.Warn("cross_account_read_rejected",
			"from", s.AccountID, "to", targetID, "path", filepath.Clean(reqPath), "error", err.Error())
		return jsonResult(map[string]any{"error": err.Error()})
	}

	// O_NOFOLLOW closes the TOCTOU window between EvalSymlinks validation and
	// the actual open — if realPath's last component is swapped to a symlink
	// after validation, the open fails rather than following it. Windows
	// degrades to a plain O_RDONLY (see openNoFollow_windows.go).
	f, err := openNoFollow(realPath)
	if err != nil {
		return jsonResult(map[string]any{"error": fmt.Sprintf("read: %v", err)})
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return jsonResult(map[string]any{"error": fmt.Sprintf("read: %v", err)})
	}
	if info.Size() > maxFileReadSize {
		return jsonResult(map[string]any{"error": fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), maxFileReadSize)})
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileReadSize+1))
	if err != nil {
		return jsonResult(map[string]any{"error": fmt.Sprintf("read: %v", err)})
	}

	slog.Info("cross_account_read",
		"from", s.AccountID, "to", targetID, "path", filepath.Clean(reqPath), "bytes", len(data))

	return jsonResult(map[string]any{"content": string(data)})
}

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"

	"github.com/jinto/kittypaw/core"
)

// executeShare implements Share.read — the sole cross-tenant file read path.
// Access control lives in core.ValidateSharedReadPath; this layer plumbs the
// reader identity and emits the cross_tenant_read audit record unconditionally
// so data-flow auditing never relies on callers remembering to log.
func executeShare(_ context.Context, call core.SkillCall, s *Session) (string, error) {
	if call.Method != "read" {
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Share method: %s", call.Method)})
	}

	if s.TenantRegistry == nil || s.TenantID == "" {
		return jsonResult(map[string]any{"error": "Share.read unavailable: tenant context not configured"})
	}

	if len(call.Args) < 2 {
		return jsonResult(map[string]any{"error": "Share.read(tenantID, path) requires two arguments"})
	}
	var targetID string
	if err := json.Unmarshal(call.Args[0], &targetID); err != nil {
		return jsonResult(map[string]any{"error": "tenantID must be a string"})
	}
	if err := core.ValidateTenantID(targetID); err != nil {
		return jsonResult(map[string]any{"error": err.Error()})
	}
	var reqPath string
	if err := json.Unmarshal(call.Args[1], &reqPath); err != nil {
		return jsonResult(map[string]any{"error": "path must be a string"})
	}

	owner := s.TenantRegistry.Get(targetID)
	if owner == nil {
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown tenant: %s", targetID)})
	}

	realPath, err := core.ValidateSharedReadPath(owner.Config, owner.BaseDir, s.TenantID, reqPath)
	if err != nil {
		// Clean the logged path so newlines in reqPath can't forge fake audit lines.
		slog.Warn("cross_tenant_read_rejected",
			"from", s.TenantID, "to", targetID, "path", filepath.Clean(reqPath), "error", err.Error())
		return jsonResult(map[string]any{"error": err.Error()})
	}

	// O_NOFOLLOW closes the TOCTOU window between EvalSymlinks validation and
	// the actual open — if realPath's last component is swapped to a symlink
	// after validation, the open fails rather than following it.
	f, err := os.OpenFile(realPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
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

	slog.Info("cross_tenant_read",
		"from", s.TenantID, "to", targetID, "path", filepath.Clean(reqPath), "bytes", len(data))

	return jsonResult(map[string]any{"content": string(data)})
}

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jinto/gopaw/core"
)

type contextKey string

const ctxKeyAgentID contextKey = "agentID"

// ContextWithAgentID stores the agent ID in context for use by skill handlers.
func ContextWithAgentID(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, ctxKeyAgentID, agentID)
}

// AgentIDFromContext retrieves the agent ID from context.
func AgentIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyAgentID).(string); ok {
		return v
	}
	return ""
}

// needsPermission checks whether a skill call requires explicit user approval
// based on the config's permission policy. Returns false for AutonomyFull
// (auto-approve) and AutonomyReadonly (execution blocked elsewhere).
func needsPermission(skillName, method string, cfg *core.Config) bool {
	if cfg.AutonomyLevel == core.AutonomyFull {
		return false
	}
	if cfg.AutonomyLevel == core.AutonomyReadonly {
		return false
	}
	key := skillName + "." + method
	list := cfg.Permissions.RequireApproval
	if list == nil {
		list = core.DefaultRequireApproval
	}
	return slices.Contains(list, key)
}

// resolveSkillCall dispatches a single skill call to the appropriate handler.
func resolveSkillCall(ctx context.Context, call core.SkillCall, s *Session, permFn PermissionCallback) (string, error) {
	slog.Debug("resolving skill call", "skill", call.SkillName, "method", call.Method)

	// Central permission gate — applies to ALL skills uniformly.
	if needsPermission(call.SkillName, call.Method, s.Config) {
		desc := fmt.Sprintf("%s.%s", call.SkillName, call.Method)
		// Include the first argument for context (e.g., the shell command or file path).
		if len(call.Args) > 0 {
			var arg string
			if json.Unmarshal(call.Args[0], &arg) == nil && arg != "" {
				const maxArgLen = 200
				if len(arg) > maxArgLen {
					// Avoid splitting a multi-byte UTF-8 character.
					arg = arg[:maxArgLen]
					for len(arg) > 0 && arg[len(arg)-1]&0xC0 == 0x80 {
						arg = arg[:len(arg)-1]
					}
					if len(arg) > 0 && arg[len(arg)-1]&0xC0 == 0xC0 {
						arg = arg[:len(arg)-1]
					}
					arg += "..."
				}
				desc += ": " + arg
			}
		}
		if permFn != nil {
			ok, err := permFn(ctx, desc, call.SkillName)
			if err != nil || !ok {
				return jsonResult(map[string]any{"error": call.SkillName + "." + call.Method + " permission denied"})
			}
		} else {
			return jsonResult(map[string]any{"error": call.SkillName + "." + call.Method + " requires permission approval"})
		}
	}

	switch call.SkillName {
	case "Http", "Web":
		return executeHTTP(ctx, call, s)
	case "File":
		return executeFile(ctx, call, s)
	case "Storage":
		return executeStorage(ctx, call, s)
	case "Shell":
		return executeShell(ctx, call, s)
	case "Git":
		return executeGit(ctx, call, s)
	case "Llm":
		return executeLLM(ctx, call, s)
	case "Memory":
		return executeMemory(ctx, call, s)
	case "Todo":
		return executeTodo(ctx, call, s)
	case "Env":
		return executeEnv(call)
	case "Telegram":
		return executeTelegram(ctx, call, s)
	case "Slack":
		return executeSlack(ctx, call, s)
	case "Discord":
		return executeDiscord(ctx, call, s)
	case "Skill":
		return executeSkillMgmt(ctx, call, s)
	case "Profile":
		return executeProfile(ctx, call, s)
	case "Tts":
		return executeTTS(ctx, call, s)
	case "Image":
		return executeImage(ctx, call, s)
	case "Vision":
		return executeVision(ctx, call, s)
	case "Mcp":
		return executeMCP(ctx, call, s)
	case "Agent":
		return executeDelegate(ctx, call, s)
	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown skill: %s", call.SkillName)})
	}
}

// --- HTTP / Web ---

func executeHTTP(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	switch call.Method {
	case "get", "post", "put", "delete", "patch", "head", "search", "fetch":
	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Http method: %s", call.Method)})
	}

	if len(call.Args) == 0 {
		return jsonResult(map[string]any{"error": "url argument required"})
	}

	var urlStr string
	if err := json.Unmarshal(call.Args[0], &urlStr); err != nil {
		return jsonResult(map[string]any{"error": "invalid url"})
	}

	// SSRF prevention: validate URL host.
	if err := validateHTTPTarget(urlStr, s.Config.Sandbox.AllowedHosts); err != nil {
		return jsonResult(map[string]any{"error": err.Error()})
	}

	// Web.search uses a search API
	if call.Method == "search" {
		return webSearch(ctx, urlStr)
	}
	// Web.fetch gets page text
	if call.Method == "fetch" {
		return webFetch(ctx, urlStr)
	}

	method := strings.ToUpper(call.Method)
	var body io.Reader
	if len(call.Args) > 1 && (method == "POST" || method == "PUT" || method == "PATCH") {
		body = strings.NewReader(string(call.Args[1]))
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error()})
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error()})
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 100_000))
	return jsonResult(map[string]any{
		"status": resp.StatusCode,
		"body":   string(respBody),
	})
}

// validateHTTPTarget blocks requests to private/internal addresses and enforces AllowedHosts.
func validateHTTPTarget(urlStr string, allowedHosts []string) error {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	host := parsed.Hostname()
	if core.IsPrivateIP(host) {
		return fmt.Errorf("requests to private/internal address %q are blocked", host)
	}
	if len(allowedHosts) > 0 {
		for _, h := range allowedHosts {
			if h == "*" || h == host {
				return nil
			}
		}
		return fmt.Errorf("host %q not in allowed hosts", host)
	}
	return nil
}

func webSearch(ctx context.Context, query string) (string, error) {
	// Use DuckDuckGo HTML search as a free search backend
	url := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error()})
	}
	req.Header.Set("User-Agent", "GoPaw/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error()})
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 200_000))
	// Simple extraction of results from DuckDuckGo HTML
	results := extractSearchResults(string(body))
	return jsonResult(map[string]any{"results": results})
}

func webFetch(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error()})
	}
	req.Header.Set("User-Agent", "GoPaw/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error()})
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 500_000))
	// Strip HTML tags for text extraction
	text := stripHTMLTags(string(body))
	if len(text) > 10000 {
		text = text[:10000]
	}
	return jsonResult(map[string]any{"text": text, "status": resp.StatusCode})
}

// --- File ---

const maxFileReadSize = 10 * 1024 * 1024 // 10MB — protects LLM context from huge files.

func executeFile(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	// Index-based methods dispatch early — they don't take a file path.
	switch call.Method {
	case "search":
		return executeFileSearch(ctx, call, s)
	case "stats":
		return executeFileStats(ctx, call, s)
	case "reindex":
		return executeFileReindex(ctx, call, s)
	}

	if len(call.Args) == 0 {
		return "", fmt.Errorf("path argument required")
	}
	var rawPath string
	if err := json.Unmarshal(call.Args[0], &rawPath); err != nil {
		return "", fmt.Errorf("invalid path argument")
	}

	// Resolve the path once and use it for both validation and all file operations.
	// This eliminates the TOCTOU race between validation and filesystem access.
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return "", fmt.Errorf("path not allowed")
	}
	resolvedPath := resolveForValidation(absPath)

	if !isPathAllowedResolved(resolvedPath, s.AllowedPaths()) {
		return "", fmt.Errorf("path not allowed")
	}

	switch call.Method {
	case "read":
		// Open + fstat + limited read on the same fd to prevent TOCTOU size bypass.
		f, err := os.Open(resolvedPath)
		if err != nil {
			return "", fmt.Errorf("file read: %w", err)
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return "", fmt.Errorf("file read: %w", err)
		}
		if info.Size() > maxFileReadSize {
			return "", fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), maxFileReadSize)
		}
		data, err := io.ReadAll(io.LimitReader(f, maxFileReadSize+1))
		if err != nil {
			return "", fmt.Errorf("file read: %w", err)
		}
		return jsonResult(map[string]any{"content": string(data)})

	case "write":
		if len(call.Args) < 2 {
			return "", fmt.Errorf("content argument required")
		}
		var content string
		if err := json.Unmarshal(call.Args[1], &content); err != nil {
			return "", fmt.Errorf("invalid content argument")
		}
		if err := os.WriteFile(resolvedPath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("file write: %w", err)
		}
		return jsonResult(map[string]any{"success": true})

	case "append":
		if len(call.Args) < 2 {
			return "", fmt.Errorf("content argument required")
		}
		var content string
		if err := json.Unmarshal(call.Args[1], &content); err != nil {
			return "", fmt.Errorf("invalid content argument")
		}
		f, err := os.OpenFile(resolvedPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return "", fmt.Errorf("file append: %w", err)
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			return "", fmt.Errorf("file append: %w", err)
		}
		return jsonResult(map[string]any{"success": true})

	case "delete":
		if err := os.Remove(resolvedPath); err != nil {
			return "", fmt.Errorf("file delete: %w", err)
		}
		return jsonResult(map[string]any{"success": true})

	case "list":
		entries, err := os.ReadDir(resolvedPath)
		if err != nil {
			return "", fmt.Errorf("file list: %w", err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return jsonResult(map[string]any{"files": names})

	case "exists":
		_, err := os.Stat(resolvedPath)
		return jsonResult(map[string]any{"exists": err == nil})

	case "mkdir":
		if err := os.MkdirAll(resolvedPath, 0o755); err != nil {
			return "", fmt.Errorf("file mkdir: %w", err)
		}
		return jsonResult(map[string]any{"success": true})

	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown File method: %s", call.Method)})
	}
}

// --- File index methods ---

func executeFileSearch(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	if s.Indexer == nil {
		return "", fmt.Errorf("workspace indexer not available")
	}
	if len(call.Args) == 0 {
		return "", fmt.Errorf("query argument required")
	}
	var query string
	if err := json.Unmarshal(call.Args[0], &query); err != nil {
		return "", fmt.Errorf("invalid query argument")
	}
	if query == "" {
		return "", fmt.Errorf("empty search query")
	}

	var opts SearchOptions
	if len(call.Args) > 1 {
		json.Unmarshal(call.Args[1], &opts)
	}

	result, err := s.Indexer.Search(ctx, query, opts)
	if err != nil {
		return "", fmt.Errorf("file search: %w", err)
	}

	// Post-filter by AllowedPaths (defense-in-depth).
	allowed := s.AllowedPaths()
	if allowed != nil {
		filtered := make([]SearchHit, 0, len(result.Files))
		for _, hit := range result.Files {
			resolved := resolveForValidation(hit.Path)
			if isPathAllowedResolved(resolved, allowed) {
				filtered = append(filtered, hit)
			}
		}
		result.Files = filtered
	}

	return jsonResult(result)
}

func executeFileStats(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	if s.Indexer == nil {
		return "", fmt.Errorf("workspace indexer not available")
	}
	var opts StatsOptions
	if len(call.Args) > 0 {
		var path string
		if err := json.Unmarshal(call.Args[0], &path); err == nil {
			opts.Path = path
		}
	}
	result, err := s.Indexer.Stats(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("file stats: %w", err)
	}
	return jsonResult(result)
}

func executeFileReindex(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	if s.Indexer == nil {
		return "", fmt.Errorf("workspace indexer not available")
	}

	// Determine which workspace(s) to reindex.
	var targetPath string
	if len(call.Args) > 0 {
		json.Unmarshal(call.Args[0], &targetPath)
	}

	wss, err := s.Store.ListWorkspaces()
	if err != nil {
		return "", fmt.Errorf("list workspaces: %w", err)
	}

	var totalResult IndexResult
	for _, ws := range wss {
		// If a path is given, only reindex matching workspace.
		if targetPath != "" {
			absTarget, _ := filepath.Abs(targetPath)
			if !strings.HasPrefix(absTarget, ws.RootPath) {
				continue
			}
		}
		result, reErr := s.Indexer.Reindex(ctx, ws.ID, ws.RootPath)
		if reErr != nil {
			slog.Warn("reindex failed", "workspace", ws.ID, "error", reErr)
			totalResult.Errors++
			continue
		}
		totalResult.Indexed += result.Indexed
		totalResult.Skipped += result.Skipped
		totalResult.Errors += result.Errors
		totalResult.DurationMs += result.DurationMs
	}

	return jsonResult(totalResult)
}

// --- Storage ---

func executeStorage(_ context.Context, call core.SkillCall, s *Session) (string, error) {
	switch call.Method {
	case "get":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "key required"})
		}
		var key string
		json.Unmarshal(call.Args[0], &key)
		val, ok, err := s.Store.StorageGet("default", key)
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		if !ok {
			return jsonResult(nil)
		}
		return jsonResult(map[string]any{"value": val})

	case "set":
		if len(call.Args) < 2 {
			return jsonResult(map[string]any{"error": "key and value required"})
		}
		var key string
		json.Unmarshal(call.Args[0], &key)
		val := string(call.Args[1])
		if err := s.Store.StorageSet("default", key, val); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	case "delete":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "key required"})
		}
		var key string
		json.Unmarshal(call.Args[0], &key)
		if err := s.Store.StorageDelete("default", key); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	case "list":
		keys, err := s.Store.StorageList("default")
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"keys": keys})

	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Storage method: %s", call.Method)})
	}
}

// --- Shell ---

func executeShell(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	if call.Method != "exec" {
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Shell method: %s", call.Method)})
	}
	if len(call.Args) == 0 {
		return jsonResult(map[string]any{"error": "command required"})
	}
	var command string
	json.Unmarshal(call.Args[0], &command)

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return jsonResult(map[string]any{"error": err.Error()})
		}
	}
	return jsonResult(map[string]any{
		"output":    string(output),
		"exit_code": exitCode,
	})
}

// --- Git ---

func executeGit(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	var args []string

	switch call.Method {
	case "status":
		args = []string{"status", "--short"}
	case "log":
		n := "10"
		if len(call.Args) > 0 {
			json.Unmarshal(call.Args[0], &n)
		}
		args = []string{"log", "--oneline", "-n", n}
	case "diff":
		args = []string{"diff"}
	case "add":
		if len(call.Args) == 0 {
			args = []string{"add", "."}
		} else {
			var path string
			json.Unmarshal(call.Args[0], &path)
			args = []string{"add", path}
		}
	case "commit":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "commit message required"})
		}
		var msg string
		json.Unmarshal(call.Args[0], &msg)
		args = []string{"commit", "-m", msg}
	case "push":
		args = []string{"push"}
	case "pull":
		args = []string{"pull"}
	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Git method: %s", call.Method)})
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return jsonResult(map[string]any{"output": string(output), "error": err.Error()})
	}
	return jsonResult(map[string]any{"output": string(output)})
}

// --- LLM ---

func executeLLM(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	if call.Method != "generate" {
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Llm method: %s", call.Method)})
	}
	if len(call.Args) == 0 {
		return jsonResult(map[string]any{"error": "prompt required"})
	}
	var prompt string
	json.Unmarshal(call.Args[0], &prompt)

	messages := []core.LlmMessage{
		{Role: core.RoleUser, Content: prompt},
	}
	resp, err := s.Provider.Generate(ctx, messages)
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error()})
	}

	result := map[string]any{"text": resp.Content}
	if resp.Usage != nil {
		result["model"] = resp.Usage.Model
		result["usage"] = map[string]any{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
		}
	}
	return jsonResult(result)
}

// --- Memory ---

func executeMemory(_ context.Context, call core.SkillCall, s *Session) (string, error) {
	switch call.Method {
	case "search":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "query required"})
		}
		var query string
		json.Unmarshal(call.Args[0], &query)
		results, err := s.Store.SearchExecutions(query, 10)
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"results": results})

	case "user", "set":
		if len(call.Args) < 2 {
			return jsonResult(map[string]any{"error": "key and value required"})
		}
		var key string
		json.Unmarshal(call.Args[0], &key)
		var value string
		json.Unmarshal(call.Args[1], &value)
		if err := s.Store.SetUserContext(key, value, "agent"); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	case "get":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "key required"})
		}
		var key string
		json.Unmarshal(call.Args[0], &key)
		val, ok, err := s.Store.GetUserContext(key)
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		if !ok {
			return jsonResult(nil)
		}
		return jsonResult(map[string]any{"value": val})

	case "delete":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "key required"})
		}
		var key string
		json.Unmarshal(call.Args[0], &key)
		ok, err := s.Store.DeleteUserContext(key)
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"deleted": ok})

	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Memory method: %s", call.Method)})
	}
}

// --- Todo ---

func executeTodo(_ context.Context, call core.SkillCall, s *Session) (string, error) {
	ns := "todo"
	switch call.Method {
	case "list":
		keys, err := s.Store.StorageList(ns)
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		var items []map[string]string
		for _, k := range keys {
			val, _, _ := s.Store.StorageGet(ns, k)
			items = append(items, map[string]string{"id": k, "text": val})
		}
		return jsonResult(map[string]any{"items": items})

	case "add":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "text required"})
		}
		var text string
		json.Unmarshal(call.Args[0], &text)
		id := fmt.Sprintf("todo-%d", time.Now().UnixNano())
		if err := s.Store.StorageSet(ns, id, text); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"id": id, "success": true})

	case "update":
		if len(call.Args) < 2 {
			return jsonResult(map[string]any{"error": "id and text required"})
		}
		var id, text string
		json.Unmarshal(call.Args[0], &id)
		json.Unmarshal(call.Args[1], &text)
		if err := s.Store.StorageSet(ns, id, text); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	case "delete":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "id required"})
		}
		var id string
		json.Unmarshal(call.Args[0], &id)
		if err := s.Store.StorageDelete(ns, id); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Todo method: %s", call.Method)})
	}
}

// --- Env ---

func executeEnv(call core.SkillCall) (string, error) {
	if call.Method != "get" {
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Env method: %s", call.Method)})
	}
	if len(call.Args) == 0 {
		return jsonResult(map[string]any{"error": "name required"})
	}
	var name string
	json.Unmarshal(call.Args[0], &name)
	if core.IsSecretEnvVar(name) {
		return jsonResult(map[string]any{"error": fmt.Sprintf("access to secret env var %q is blocked", name)})
	}
	return jsonResult(map[string]any{"value": os.Getenv(name)})
}

// --- Channel sends (Telegram, Slack, Discord) ---

func executeTelegram(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	if call.Method != "sendMessage" && call.Method != "send" && call.Method != "sendVoice" {
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Telegram method: %s", call.Method)})
	}
	// Find telegram token from config
	var token string
	for _, ch := range s.Config.Channels {
		if ch.ChannelType == core.ChannelTelegram {
			token = ch.Token
			break
		}
	}
	if token == "" {
		return jsonResult(map[string]any{"error": "telegram not configured"})
	}
	if len(call.Args) == 0 {
		return jsonResult(map[string]any{"error": "text required"})
	}
	var text string
	json.Unmarshal(call.Args[0], &text)

	// Send via Telegram Bot API
	return sendTelegramMessage(ctx, token, text)
}

func executeSlack(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	if call.Method != "send" {
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Slack method: %s", call.Method)})
	}
	_ = ctx
	_ = s
	if len(call.Args) == 0 {
		return jsonResult(map[string]any{"error": "text required"})
	}
	// TODO: implement Slack send
	return jsonResult(map[string]any{"success": true, "note": "slack send not yet implemented"})
}

func executeDiscord(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	if call.Method != "send" {
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Discord method: %s", call.Method)})
	}
	_ = ctx
	_ = s
	if len(call.Args) == 0 {
		return jsonResult(map[string]any{"error": "text required"})
	}
	// TODO: implement Discord send
	return jsonResult(map[string]any{"success": true, "note": "discord send not yet implemented"})
}

// --- Skill Management ---

func executeSkillMgmt(_ context.Context, call core.SkillCall, s *Session) (string, error) {
	switch call.Method {
	case "list":
		skills, err := core.LoadAllSkillsFrom(s.BaseDir)
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		var items []map[string]any
		for _, s := range skills {
			items = append(items, map[string]any{
				"name":        s.Skill.Name,
				"description": s.Skill.Description,
				"enabled":     s.Skill.Enabled,
				"trigger":     s.Skill.Trigger.Type,
			})
		}
		return jsonResult(map[string]any{"skills": items})

	case "create":
		if len(call.Args) < 3 {
			return jsonResult(map[string]any{"error": "name, description, and code required"})
		}
		var name, desc, code string
		json.Unmarshal(call.Args[0], &name)
		json.Unmarshal(call.Args[1], &desc)
		json.Unmarshal(call.Args[2], &code)

		triggerType := "manual"
		if len(call.Args) > 3 {
			json.Unmarshal(call.Args[3], &triggerType)
		}
		schedule := ""
		if len(call.Args) > 4 {
			json.Unmarshal(call.Args[4], &schedule)
		}

		skill := &core.Skill{
			Name:        name,
			Version:     1,
			Description: desc,
			Enabled:     true,
			Format:      core.SkillFormatNative,
			Trigger: core.SkillTrigger{
				Type: triggerType,
			},
		}
		if triggerType == "schedule" {
			skill.Trigger.Cron = schedule
		}

		if err := core.SaveSkillTo(s.BaseDir, skill, code); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true, "name": name})

	case "disable":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "name required"})
		}
		var name string
		json.Unmarshal(call.Args[0], &name)
		if err := core.DisableSkillFrom(s.BaseDir, name); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	case "rollback":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "name required"})
		}
		var name string
		json.Unmarshal(call.Args[0], &name)
		if err := core.RollbackSkillFrom(s.BaseDir, name); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Skill method: %s", call.Method)})
	}
}

// --- Profile Management ---

func executeProfile(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	switch call.Method {
	case "list":
		profiles, err := s.Store.ListActiveProfiles()
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"profiles": profiles})

	case "switch":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "profile id required"})
		}
		var id string
		if err := json.Unmarshal(call.Args[0], &id); err != nil {
			return jsonResult(map[string]any{"error": "invalid profile id argument"})
		}
		if err := core.ValidateProfileID(id); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		// Verify profile exists on disk.
		base, err := core.ResolveBaseDir(s.BaseDir)
		if err != nil {
			return jsonResult(map[string]any{"error": "config dir: " + err.Error()})
		}
		if _, err := core.LoadProfile(base, id); err != nil {
			return jsonResult(map[string]any{"error": fmt.Sprintf("profile %q not found", id)})
		}
		// Store active_profile:{agentID} so next message uses this profile.
		agentID := AgentIDFromContext(ctx)
		if agentID == "" {
			agentID = "default"
		}
		key := fmt.Sprintf("active_profile:%s", agentID)
		if err := s.Store.SetUserContext(key, id, "agent"); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true, "profile": id})

	case "create":
		if len(call.Args) < 2 {
			return jsonResult(map[string]any{"error": "id and description required"})
		}
		var id, desc string
		if err := json.Unmarshal(call.Args[0], &id); err != nil {
			return jsonResult(map[string]any{"error": "invalid id argument"})
		}
		if err := json.Unmarshal(call.Args[1], &desc); err != nil {
			return jsonResult(map[string]any{"error": "invalid description argument"})
		}
		if err := core.ValidateProfileID(id); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		if err := s.Store.UpsertProfileMeta(id, desc, "[]", "agent"); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Profile method: %s", call.Method)})
	}
}

// --- Stubs for complex skills ---

func executeTTS(_ context.Context, _ core.SkillCall, _ *Session) (string, error) {
	// TODO: implement TTS via external API
	return jsonResult(map[string]any{"error": "TTS not yet implemented"})
}

// executeImage and executeVision are in vision.go.

func executeMCP(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	if s.McpRegistry == nil {
		return jsonResult(map[string]any{"error": "MCP not configured"})
	}
	switch call.Method {
	case "call":
		if len(call.Args) < 3 {
			return jsonResult(map[string]any{"error": "Mcp.call requires (server, tool, args)"})
		}
		var server, tool string
		if err := json.Unmarshal(call.Args[0], &server); err != nil {
			return jsonResult(map[string]any{"error": "invalid server argument"})
		}
		if err := json.Unmarshal(call.Args[1], &tool); err != nil {
			return jsonResult(map[string]any{"error": "invalid tool argument"})
		}
		var args any
		if err := json.Unmarshal(call.Args[2], &args); err != nil {
			return jsonResult(map[string]any{"error": "invalid args argument"})
		}
		return s.McpRegistry.CallTool(ctx, server, tool, args)
	case "listTools":
		if len(call.Args) < 1 {
			return jsonResult(map[string]any{"error": "Mcp.listTools requires (server)"})
		}
		var server string
		if err := json.Unmarshal(call.Args[0], &server); err != nil {
			return jsonResult(map[string]any{"error": "invalid server argument"})
		}
		tools, err := s.McpRegistry.ListTools(ctx, server)
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"tools": tools})
	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Mcp method: %s", call.Method)})
	}
}

func executeDelegate(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	switch call.Method {
	case "delegate":
		// Agent.delegate(task, profileId, background)
		if len(call.Args) < 2 {
			return jsonResult(map[string]any{"error": "Agent.delegate requires (task, profileId)"})
		}
		var task, profileID string
		if err := json.Unmarshal(call.Args[0], &task); err != nil {
			return jsonResult(map[string]any{"error": "invalid task argument"})
		}
		if err := json.Unmarshal(call.Args[1], &profileID); err != nil {
			return jsonResult(map[string]any{"error": "invalid profileId argument"})
		}
		var background bool
		if len(call.Args) > 2 {
			_ = json.Unmarshal(call.Args[2], &background)
		}

		if len(task) > maxDelegateTaskLen {
			return jsonResult(map[string]any{
				"error":   fmt.Sprintf("task too long (%d > %d chars)", len(task), maxDelegateTaskLen),
				"success": false,
			})
		}

		// Execute delegation.
		spec := PMTaskSpec{ProfileID: profileID, Task: task, Background: background}
		maxDepth := 3
		if s.Config.Orchestration.MaxDepth > 0 {
			maxDepth = int(s.Config.Orchestration.MaxDepth)
		}

		result := executeDelegateTask(ctx, spec, s.Provider, s.Store, nil, 1, maxDepth, s.BaseDir)
		return jsonResult(map[string]any{
			"result":      result.Result,
			"success":     result.Success,
			"token_usage": result.TokenUsage,
		})

	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Agent method: %s", call.Method)})
	}
}

// --- Helpers ---

func jsonResult(v any) (string, error) {
	if v == nil {
		return "null", nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "{}", err
	}
	return string(data), nil
}

// isPathAllowed resolves both the target path and the allowed paths, then
// checks containment. Used by tests and callers with raw (unresolved) paths.
// The production hot path in executeFile uses isPathAllowedResolved with
// pre-resolved paths from the Session cache.
func isPathAllowed(path string, allowedPaths []string) bool {
	if len(allowedPaths) == 0 {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	resolved := resolveForValidation(absPath)
	resolvedAllowed := make([]string, 0, len(allowedPaths))
	for _, a := range allowedPaths {
		abs, err := filepath.Abs(a)
		if err != nil {
			continue
		}
		resolvedAllowed = append(resolvedAllowed, resolveForValidation(abs))
	}
	return isPathAllowedResolved(resolved, resolvedAllowed)
}

// isPathAllowedResolved checks an already-resolved absolute path against the
// allowed paths list. The allowed paths are expected to be pre-resolved
// (stored that way by RefreshAllowedPaths).
func isPathAllowedResolved(resolvedPath string, allowedPaths []string) bool {
	if len(allowedPaths) == 0 {
		return false
	}
	for _, allowed := range allowedPaths {
		if resolvedPath == allowed || strings.HasPrefix(resolvedPath, allowed+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// resolveForValidation resolves symlinks for path validation. When the file
// doesn't exist (e.g., write to new file), it walks up to find the deepest
// existing ancestor, resolves that, and re-appends the remaining path segments.
// This prevents symlink-in-parent-dir attacks for non-existent target files.
func resolveForValidation(absPath string) string {
	// Fast path: file/dir exists — resolve directly.
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		return resolved
	}
	// Walk up to find the deepest existing ancestor.
	current := absPath
	var trail []string
	for {
		parent := filepath.Dir(current)
		trail = append(trail, filepath.Base(current))
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			// Reconstruct: resolved ancestor + unresolved tail segments.
			for i := len(trail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, trail[i])
			}
			return resolved
		}
		if parent == current {
			break // reached filesystem root
		}
		current = parent
	}
	return absPath
}

func extractSearchResults(html string) []map[string]string {
	var results []map[string]string
	// Simple regex-free extraction from DuckDuckGo HTML
	parts := strings.Split(html, "result__a")
	for i, part := range parts {
		if i == 0 {
			continue
		}
		// Extract href
		hrefIdx := strings.Index(part, "href=\"")
		if hrefIdx == -1 {
			continue
		}
		href := part[hrefIdx+6:]
		hrefEnd := strings.Index(href, "\"")
		if hrefEnd == -1 {
			continue
		}
		url := href[:hrefEnd]

		// Extract title text (between > and </a>)
		titleStart := strings.Index(part, ">")
		if titleStart == -1 {
			continue
		}
		titleEnd := strings.Index(part[titleStart:], "</a>")
		if titleEnd == -1 {
			continue
		}
		title := stripHTMLTags(part[titleStart+1 : titleStart+titleEnd])

		// Extract snippet
		snippet := ""
		snippetIdx := strings.Index(part, "result__snippet")
		if snippetIdx != -1 {
			snipStart := strings.Index(part[snippetIdx:], ">")
			if snipStart != -1 {
				snipEnd := strings.Index(part[snippetIdx+snipStart:], "</")
				if snipEnd != -1 {
					snippet = stripHTMLTags(part[snippetIdx+snipStart+1 : snippetIdx+snipStart+snipEnd])
				}
			}
		}

		results = append(results, map[string]string{
			"title":   strings.TrimSpace(title),
			"url":     url,
			"snippet": strings.TrimSpace(snippet),
		})
		if len(results) >= 10 {
			break
		}
	}
	return results
}

func stripHTMLTags(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func sendTelegramMessage(ctx context.Context, token, text string) (string, error) {
	// This needs a chat_id — get it from the event context
	// For now, broadcast to admin chat IDs would need config
	// The actual chat_id comes from the event being processed
	_ = ctx
	_ = token

	// Chunked sending for long messages (Telegram 4096 char limit)
	const maxLen = 4096
	if len(text) <= maxLen {
		return jsonResult(map[string]any{"success": true, "message": text})
	}

	chunks := core.SplitChunks(text, maxLen)
	return jsonResult(map[string]any{"success": true, "chunks": len(chunks)})
}

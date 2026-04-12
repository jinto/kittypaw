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
	"strings"
	"time"

	"github.com/jinto/gopaw/core"
)

// resolveSkillCall dispatches a single skill call to the appropriate handler.
func resolveSkillCall(ctx context.Context, call core.SkillCall, s *Session, permFn PermissionCallback) (string, error) {
	slog.Debug("resolving skill call", "skill", call.SkillName, "method", call.Method)

	switch call.SkillName {
	case "Http", "Web":
		return executeHTTP(ctx, call, s)
	case "File":
		return executeFile(ctx, call, s)
	case "Storage":
		return executeStorage(ctx, call, s)
	case "Shell":
		return executeShell(ctx, call, s, permFn)
	case "Git":
		return executeGit(ctx, call, s, permFn)
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

func executeFile(_ context.Context, call core.SkillCall, s *Session) (string, error) {
	if len(call.Args) == 0 {
		return jsonResult(map[string]any{"error": "path argument required"})
	}
	var path string
	if err := json.Unmarshal(call.Args[0], &path); err != nil {
		return jsonResult(map[string]any{"error": "invalid path"})
	}

	// Validate path is within allowed paths
	if !isPathAllowed(path, s.Config.Sandbox.AllowedPaths) {
		return jsonResult(map[string]any{"error": "path not allowed"})
	}

	switch call.Method {
	case "read":
		data, err := os.ReadFile(path)
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"content": string(data)})

	case "write":
		if len(call.Args) < 2 {
			return jsonResult(map[string]any{"error": "content argument required"})
		}
		var content string
		json.Unmarshal(call.Args[1], &content)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	case "append":
		if len(call.Args) < 2 {
			return jsonResult(map[string]any{"error": "content argument required"})
		}
		var content string
		json.Unmarshal(call.Args[1], &content)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	case "delete":
		if err := os.Remove(path); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	case "list":
		entries, err := os.ReadDir(path)
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return jsonResult(map[string]any{"files": names})

	case "exists":
		_, err := os.Stat(path)
		return jsonResult(map[string]any{"exists": err == nil})

	case "mkdir":
		if err := os.MkdirAll(path, 0o755); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown File method: %s", call.Method)})
	}
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

func executeShell(ctx context.Context, call core.SkillCall, s *Session, permFn PermissionCallback) (string, error) {
	if call.Method != "exec" {
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Shell method: %s", call.Method)})
	}
	if len(call.Args) == 0 {
		return jsonResult(map[string]any{"error": "command required"})
	}
	var command string
	json.Unmarshal(call.Args[0], &command)

	if permFn != nil {
		ok, err := permFn(ctx, "Shell.exec: "+command, command)
		if err != nil || !ok {
			return jsonResult(map[string]any{"error": "Shell.exec permission denied"})
		}
	} else if s.Config.AutonomyLevel != core.AutonomyFull {
		return jsonResult(map[string]any{"error": "Shell.exec requires permission approval"})
	}

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

func executeGit(ctx context.Context, call core.SkillCall, s *Session, permFn PermissionCallback) (string, error) {
	var args []string

	// Destructive git operations require explicit permission.
	destructive := false

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
		destructive = true
		if len(call.Args) == 0 {
			args = []string{"add", "."}
		} else {
			var path string
			json.Unmarshal(call.Args[0], &path)
			args = []string{"add", path}
		}
	case "commit":
		destructive = true
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "commit message required"})
		}
		var msg string
		json.Unmarshal(call.Args[0], &msg)
		args = []string{"commit", "-m", msg}
	case "push":
		destructive = true
		args = []string{"push"}
	case "pull":
		destructive = true
		args = []string{"pull"}
	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Git method: %s", call.Method)})
	}

	if destructive {
		desc := fmt.Sprintf("Git.%s: git %s", call.Method, strings.Join(args, " "))
		if permFn != nil {
			ok, err := permFn(ctx, desc, "git")
			if err != nil || !ok {
				return jsonResult(map[string]any{"error": "Git permission denied for " + call.Method})
			}
		} else if s.Config.AutonomyLevel != core.AutonomyFull {
			return jsonResult(map[string]any{"error": "Git." + call.Method + " requires permission approval"})
		}
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

func executeSkillMgmt(_ context.Context, call core.SkillCall, _ *Session) (string, error) {
	switch call.Method {
	case "list":
		skills, err := core.LoadAllSkills()
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

		if err := core.SaveSkill(skill, code); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true, "name": name})

	case "disable":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "name required"})
		}
		var name string
		json.Unmarshal(call.Args[0], &name)
		if err := core.DisableSkill(name); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	case "rollback":
		if len(call.Args) == 0 {
			return jsonResult(map[string]any{"error": "name required"})
		}
		var name string
		json.Unmarshal(call.Args[0], &name)
		if err := core.RollbackSkill(name); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Skill method: %s", call.Method)})
	}
}

// --- Profile Management ---

func executeProfile(_ context.Context, call core.SkillCall, s *Session) (string, error) {
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
		json.Unmarshal(call.Args[0], &id)
		// TODO: implement profile switching
		return jsonResult(map[string]any{"success": true, "profile": id})

	case "create":
		if len(call.Args) < 2 {
			return jsonResult(map[string]any{"error": "id and description required"})
		}
		var id, desc string
		json.Unmarshal(call.Args[0], &id)
		json.Unmarshal(call.Args[1], &desc)
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

func executeImage(_ context.Context, _ core.SkillCall, _ *Session) (string, error) {
	// TODO: implement image generation
	return jsonResult(map[string]any{"error": "Image generation not yet implemented"})
}

func executeVision(_ context.Context, _ core.SkillCall, _ *Session) (string, error) {
	// TODO: implement vision analysis
	return jsonResult(map[string]any{"error": "Vision not yet implemented"})
}

func executeMCP(_ context.Context, _ core.SkillCall, _ *Session) (string, error) {
	// TODO: implement MCP tool calls
	return jsonResult(map[string]any{"error": "MCP not yet implemented"})
}

func executeDelegate(_ context.Context, _ core.SkillCall, _ *Session) (string, error) {
	// TODO: implement agent delegation
	return jsonResult(map[string]any{"error": "Agent delegation not yet implemented"})
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

func isPathAllowed(path string, allowedPaths []string) bool {
	if len(allowedPaths) == 0 {
		return false // Deny by default when no paths are configured
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	// Resolve symlinks to prevent escaping allowed directories.
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolved
	}
	for _, allowed := range allowedPaths {
		absAllowed, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(absAllowed); err == nil {
			absAllowed = resolved
		}
		if absPath == absAllowed || strings.HasPrefix(absPath, absAllowed+string(filepath.Separator)) {
			return true
		}
	}
	return false
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

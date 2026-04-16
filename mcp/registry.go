// Package mcpreg provides an MCP (Model Context Protocol) client registry
// that manages connections to external MCP tool servers.
package mcpreg

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jinto/kittypaw/core"
)

// ToolInfo is a simplified view of an MCP tool for prompt injection.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// serverEntry holds a live connection to one MCP server.
type serverEntry struct {
	session *mcp.ClientSession
	cmd     *exec.Cmd   // non-nil for CommandTransport (subprocess)
	tools   []*mcp.Tool // cached after connect
}

// Registry manages connections to multiple MCP servers.
type Registry struct {
	configs map[string]core.MCPServerConfig
	entries map[string]*serverEntry
	mu      sync.RWMutex
}

// NewRegistry creates a Registry from the given server configurations.
// No connections are made until Connect or ConnectAll is called.
func NewRegistry(servers []core.MCPServerConfig) *Registry {
	configs := make(map[string]core.MCPServerConfig, len(servers))
	for _, s := range servers {
		configs[s.Name] = s
	}
	return &Registry{
		configs: configs,
		entries: make(map[string]*serverEntry),
	}
}

// ValidateConfig checks that all server configs have required fields.
func ValidateConfig(servers []core.MCPServerConfig) error {
	seen := make(map[string]bool, len(servers))
	for _, s := range servers {
		if s.Name == "" {
			return fmt.Errorf("MCP server config: name is required")
		}
		if s.Command == "" {
			return fmt.Errorf("MCP server %q: command is required", s.Name)
		}
		if seen[s.Name] {
			return fmt.Errorf("MCP server %q: duplicate name", s.Name)
		}
		seen[s.Name] = true
	}
	return nil
}

// IsConnected returns whether the named server has an active session.
func (r *Registry) IsConnected(server string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.entries[server]
	return ok
}

var clientImpl = &mcp.Implementation{Name: "kittypaw", Version: "1.0.0"}

// connectWithSession establishes an MCP client session over the given transport,
// caches the tool list, and stores the entry under the given name.
func (r *Registry) connectWithSession(ctx context.Context, name string, transport mcp.Transport, cmd *exec.Cmd) error {
	client := mcp.NewClient(clientImpl, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("MCP server %q: connect: %w", name, err)
	}

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		_ = session.Close()
		return fmt.Errorf("MCP server %q: list tools: %w", name, err)
	}

	r.mu.Lock()
	if old, ok := r.entries[name]; ok {
		_ = old.session.Close() // close previous session to avoid leak
	}
	r.entries[name] = &serverEntry{
		session: session,
		cmd:     cmd,
		tools:   result.Tools,
	}
	r.mu.Unlock()
	return nil
}

// ConnectWithTransport connects to an MCP server using the provided transport.
// This is primarily for testing with InMemoryTransport.
func (r *Registry) ConnectWithTransport(ctx context.Context, name string, transport mcp.Transport) error {
	return r.connectWithSession(ctx, name, transport, nil)
}

// Connect establishes a connection to the named MCP server via CommandTransport.
// The ctx controls the handshake timeout; the subprocess itself runs independently
// until Shutdown is called (CommandTransport manages its lifecycle).
func (r *Registry) Connect(ctx context.Context, name string) error {
	cfg, ok := r.configs[name]
	if !ok {
		return fmt.Errorf("MCP server %q: not configured", name)
	}

	// Use background context for the subprocess lifetime — ctx only bounds
	// the connect handshake, not the process lifecycle.
	cmd := exec.Command(cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), envSlice(cfg.Env)...)
	}

	transport := &mcp.CommandTransport{Command: cmd}
	return r.connectWithSession(ctx, name, transport, cmd)
}

// ConnectAll connects to all configured servers in parallel with a 10s timeout per server.
// Returns errors for servers that failed; successful servers are usable.
func (r *Registry) ConnectAll(ctx context.Context) []error {
	type result struct {
		name string
		err  error
	}

	ch := make(chan result, len(r.configs))
	var wg sync.WaitGroup

	for name := range r.configs {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			err := r.Connect(timeoutCtx, name)
			ch <- result{name, err}
		}(name)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	var errs []error
	for res := range ch {
		if res.err != nil {
			slog.Warn("MCP server connect failed", "server", res.name, "error", res.err)
			errs = append(errs, res.err)
		}
	}
	return errs
}

// ListTools returns the cached tool list for the named server.
func (r *Registry) ListTools(_ context.Context, server string) ([]ToolInfo, error) {
	r.mu.RLock()
	entry, ok := r.entries[server]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("MCP server %q: not connected", server)
	}
	return toToolInfos(entry.tools), nil
}

// AllTools returns all cached tools grouped by server name.
func (r *Registry) AllTools() map[string][]ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string][]ToolInfo, len(r.entries))
	for name, entry := range r.entries {
		result[name] = toToolInfos(entry.tools)
	}
	return result
}

// CallTool calls a tool on the named server and returns the result as JSON.
// The read lock is held for the entire call to prevent use-after-close when
// Shutdown runs concurrently.
func (r *Registry) CallTool(ctx context.Context, server, tool string, args any) (string, error) {
	r.mu.RLock()
	entry, ok := r.entries[server]
	if !ok {
		r.mu.RUnlock()
		return errorJSON(fmt.Sprintf("MCP server %q: not connected", server)), nil
	}

	result, err := entry.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      tool,
		Arguments: args,
	})
	r.mu.RUnlock()
	if err != nil {
		return errorJSON(err.Error()), nil
	}
	return contentToJSON(result), nil
}

// Shutdown gracefully closes all server connections.
// For CommandTransport servers, the SDK handles Close → 5s → SIGTERM escalation.
func (r *Registry) Shutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, entry := range r.entries {
		if err := entry.session.Close(); err != nil {
			slog.Warn("MCP session close error", "server", name, "error", err)
		}
		delete(r.entries, name)
	}
}

// --- helpers ---

func toToolInfos(tools []*mcp.Tool) []ToolInfo {
	infos := make([]ToolInfo, len(tools))
	for i, t := range tools {
		infos[i] = ToolInfo{Name: t.Name, Description: t.Description}
	}
	return infos
}

func contentToJSON(result *mcp.CallToolResult) string {
	type contentItem struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	items := make([]contentItem, 0, len(result.Content))
	for _, c := range result.Content {
		switch v := c.(type) {
		case *mcp.TextContent:
			items = append(items, contentItem{Type: "text", Text: v.Text})
		default:
			items = append(items, contentItem{Type: "unknown"})
		}
	}
	out := map[string]any{
		"content": items,
		"isError": result.IsError,
	}
	data, _ := json.Marshal(out)
	return string(data)
}

func errorJSON(msg string) string {
	data, _ := json.Marshal(map[string]any{"error": msg})
	return string(data)
}

func envSlice(env map[string]string) []string {
	s := make([]string, 0, len(env))
	for k, v := range env {
		s = append(s, k+"="+v)
	}
	return s
}

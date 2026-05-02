package mcpreg

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jinto/kittypaw/core"
)

// --- Test helpers ---

// startTestServer creates an in-memory MCP server with the given tools and
// returns the client-side transport. The server is connected in a background
// goroutine and cleaned up when the test ends.
func startTestServer(t *testing.T, tools ...*mcp.Tool) mcp.Transport {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "1.0"}, nil)
	for _, tool := range tools {
		if tool.InputSchema == nil {
			tool.InputSchema = map[string]any{"type": "object"}
		}
		srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "result-from-" + req.Params.Name}},
			}, nil
		})
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { ss.Close() })
	return clientTransport
}

// --- Task 1 tests ---

func TestNewRegistry(t *testing.T) {
	servers := []core.MCPServerConfig{
		{Name: "fs", Command: "mcp-fs"},
		{Name: "git", Command: "mcp-git", Args: []string{"--repo", "."}},
	}
	reg := NewRegistry(servers)
	if reg == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(reg.configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(reg.configs))
	}
	if _, ok := reg.configs["fs"]; !ok {
		t.Error("missing config for 'fs'")
	}
	if _, ok := reg.configs["git"]; !ok {
		t.Error("missing config for 'git'")
	}
}

func TestNewRegistryEmpty(t *testing.T) {
	reg := NewRegistry(nil)
	if reg == nil {
		t.Fatal("NewRegistry(nil) returned nil")
	}
	if len(reg.configs) != 0 {
		t.Fatalf("expected 0 configs, got %d", len(reg.configs))
	}
}

func TestValidateConfigOK(t *testing.T) {
	servers := []core.MCPServerConfig{
		{Name: "fs", Command: "echo"},
		{Name: "git", Command: "/usr/bin/git"},
	}
	if err := ValidateConfig(servers); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfigEmptySlice(t *testing.T) {
	if err := ValidateConfig(nil); err != nil {
		t.Fatalf("empty slice should not error: %v", err)
	}
}

func TestValidateConfigMissingCommand(t *testing.T) {
	servers := []core.MCPServerConfig{
		{Name: "fs", Command: "echo"},
		{Name: "bad", Command: ""},
	}
	err := ValidateConfig(servers)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestValidateConfigMissingName(t *testing.T) {
	servers := []core.MCPServerConfig{
		{Name: "", Command: "echo"},
	}
	err := ValidateConfig(servers)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidateConfigDuplicateName(t *testing.T) {
	servers := []core.MCPServerConfig{
		{Name: "fs", Command: "echo"},
		{Name: "fs", Command: "cat"},
	}
	err := ValidateConfig(servers)
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestIsConnectedBeforeConnect(t *testing.T) {
	reg := NewRegistry([]core.MCPServerConfig{
		{Name: "fs", Command: "echo"},
	})
	if reg.IsConnected("fs") {
		t.Error("expected IsConnected('fs') == false before Connect")
	}
	if reg.IsConnected("nonexistent") {
		t.Error("expected IsConnected('nonexistent') == false")
	}
}

// --- Task 2 tests ---

func TestConnectWithTransport(t *testing.T) {
	transport := startTestServer(t,
		&mcp.Tool{Name: "read_file", Description: "Read a file"},
	)

	reg := NewRegistry(nil)
	ctx := context.Background()

	if err := reg.ConnectWithTransport(ctx, "fs", transport); err != nil {
		t.Fatalf("ConnectWithTransport: %v", err)
	}

	if !reg.IsConnected("fs") {
		t.Error("expected IsConnected('fs') == true after connect")
	}
}

func TestListToolsAfterConnect(t *testing.T) {
	transport := startTestServer(t,
		&mcp.Tool{Name: "read_file", Description: "Read a file"},
		&mcp.Tool{Name: "write_file", Description: "Write a file"},
	)

	reg := NewRegistry(nil)
	ctx := context.Background()
	if err := reg.ConnectWithTransport(ctx, "fs", transport); err != nil {
		t.Fatalf("connect: %v", err)
	}

	tools, err := reg.ListTools(ctx, "fs")
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	// Verify tool names exist (order may vary)
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names["read_file"] || !names["write_file"] {
		t.Errorf("unexpected tools: %v", tools)
	}
}

func TestListToolsUnknownServer(t *testing.T) {
	reg := NewRegistry(nil)
	_, err := reg.ListTools(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
}

func TestAllTools(t *testing.T) {
	ctx := context.Background()

	transport1 := startTestServer(t,
		&mcp.Tool{Name: "read_file", Description: "Read a file"},
	)
	transport2 := startTestServer(t,
		&mcp.Tool{Name: "git_log", Description: "Show git log"},
		&mcp.Tool{Name: "git_diff", Description: "Show git diff"},
	)

	reg := NewRegistry(nil)
	if err := reg.ConnectWithTransport(ctx, "fs", transport1); err != nil {
		t.Fatalf("connect fs: %v", err)
	}
	if err := reg.ConnectWithTransport(ctx, "git", transport2); err != nil {
		t.Fatalf("connect git: %v", err)
	}

	all := reg.AllTools()
	if len(all) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(all))
	}
	if len(all["fs"]) != 1 {
		t.Errorf("fs: expected 1 tool, got %d", len(all["fs"]))
	}
	if len(all["git"]) != 2 {
		t.Errorf("git: expected 2 tools, got %d", len(all["git"]))
	}
}

func TestAllToolsEmpty(t *testing.T) {
	reg := NewRegistry(nil)
	all := reg.AllTools()
	if len(all) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(all))
	}
}

// --- Task 3 tests ---

func TestCallTool(t *testing.T) {
	transport := startTestServer(t,
		&mcp.Tool{Name: "greet", Description: "Say hello"},
	)

	reg := NewRegistry(nil)
	ctx := context.Background()
	if err := reg.ConnectWithTransport(ctx, "test", transport); err != nil {
		t.Fatalf("connect: %v", err)
	}

	result, err := reg.CallTool(ctx, "test", "greet", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if result == "" {
		t.Fatal("CallTool returned empty string")
	}
	// Result should contain "result-from-greet" from our test handler
	if !strings.Contains(result, "result-from-greet") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestCallToolDisconnectedServer(t *testing.T) {
	reg := NewRegistry(nil)
	result, err := reg.CallTool(context.Background(), "nonexistent", "tool", nil)
	if err != nil {
		t.Fatalf("CallTool should not return Go error: %v", err)
	}
	if !strings.Contains(result, "error") || !strings.Contains(result, "not connected") {
		t.Errorf("expected error JSON, got: %s", result)
	}
}

func TestShutdown(t *testing.T) {
	transport := startTestServer(t,
		&mcp.Tool{Name: "greet", Description: "Say hello"},
	)

	reg := NewRegistry(nil)
	ctx := context.Background()
	if err := reg.ConnectWithTransport(ctx, "test", transport); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if !reg.IsConnected("test") {
		t.Fatal("expected connected before shutdown")
	}

	reg.Shutdown()

	if reg.IsConnected("test") {
		t.Error("expected not connected after shutdown")
	}
}

func TestShutdownIdempotent(t *testing.T) {
	reg := NewRegistry(nil)
	// Should not panic when called on empty registry
	reg.Shutdown()
	reg.Shutdown()
}

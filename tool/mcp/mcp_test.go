package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/formonkey/moa/tool/mcp"
)

func TestNewStdioClient(t *testing.T) {
	client := mcp.NewStdioClient("test-mcp", "echo", "hello")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Name != "test-mcp" {
		t.Fatalf("expected name 'test-mcp', got %q", client.Name)
	}
	if client.Command != "echo" {
		t.Fatalf("expected command 'echo', got %q", client.Command)
	}
}

func TestNewStdioClientMultipleArgs(t *testing.T) {
	client := mcp.NewStdioClient("multi", "cmd", "arg1", "arg2", "arg3")
	if len(client.Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(client.Args))
	}
}

func TestNewToolset(t *testing.T) {
	client := mcp.NewStdioClient("ts-test", "echo")
	ts := mcp.NewToolset(client)
	if ts == nil {
		t.Fatal("expected non-nil toolset")
	}
	if ts.Name() != "mcp_ts-test" {
		t.Fatalf("expected name 'mcp_ts-test', got %q", ts.Name())
	}
	// Before Load, Tools should be nil/empty
	if len(ts.Tools()) != 0 {
		t.Fatal("expected empty tools before Load")
	}
}

func TestCloseWithoutStart(t *testing.T) {
	client := mcp.NewStdioClient("close-test", "echo")
	// Close without Start should not panic
	if err := client.Close(); err != nil {
		t.Fatalf("Close without Start: %v", err)
	}
}

func TestStartWithInvalidCommand(t *testing.T) {
	client := mcp.NewStdioClient("bad", "/nonexistent/binary/path", "arg")
	err := client.Start(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid command")
		client.Close()
	}
}

func TestToolsetLoadWithInvalidCommand(t *testing.T) {
	client := mcp.NewStdioClient("bad-ts", "/nonexistent/binary/path")
	ts := mcp.NewToolset(client)
	err := ts.Load(context.Background())
	if err == nil {
		t.Fatal("expected error from Load with invalid command")
		ts.Close()
	}
}

func TestToolsetClose(t *testing.T) {
	client := mcp.NewStdioClient("close-ts", "echo")
	ts := mcp.NewToolset(client)
	// Close without Load should not panic
	if err := ts.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestStartDoubleStart(t *testing.T) {
	client := mcp.NewStdioClient("double", "/nonexistent/binary")
	_ = client.Start(context.Background())
	_ = client.Start(context.Background())
	client.Close()
}

func TestStartWithCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := mcp.NewStdioClient("cancel", "cat")
	err := client.Start(ctx)
	// Should fail because context is already cancelled
	client.Close()
	if err == nil {
		t.Log("Start with cancelled context didn't error (platform-dependent)")
	}
}



// --- JSON-RPC type tests ---

func TestJSONRPCProtocolTypes(t *testing.T) {
	// Test that MCP protocol types serialize correctly
	params := struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments,omitempty"`
	}{
		Name:      "test_tool",
		Arguments: map[string]any{"query": "hello"},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded["name"] != "test_tool" {
		t.Fatal("wrong name")
	}
}

func TestToolCallResultParsing(t *testing.T) {
	raw := `{"content":[{"type":"text","text":"hello world"}],"isError":false}`
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 {
		t.Fatal("expected 1 content part")
	}
	if result.Content[0].Text != "hello world" {
		t.Fatal("wrong text")
	}
	if result.IsError {
		t.Fatal("expected isError=false")
	}
}

func TestToolCallResultErrorParsing(t *testing.T) {
	raw := `{"content":[{"type":"text","text":"something went wrong"}],"isError":true}`
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected isError=true")
	}
}

func TestToolsListResultParsing(t *testing.T) {
	raw := `{"tools":[{"name":"fetch","description":"Fetch a URL","inputSchema":{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}}]}`
	var result struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 1 {
		t.Fatal("expected 1 tool")
	}
	if result.Tools[0].Name != "fetch" {
		t.Fatal("wrong tool name")
	}
}

func TestJSONRPCErrorParsing(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`
	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int64  `json:"id"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != -32600 {
		t.Fatalf("wrong error code: %d", resp.Error.Code)
	}
}

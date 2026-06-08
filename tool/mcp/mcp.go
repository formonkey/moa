// Package mcp provides a real MCP (Model Context Protocol) client that communicates
// with MCP servers via JSON-RPC 2.0 over stdio.
//
// It starts the MCP server as a subprocess, negotiates capabilities via the
// "initialize" handshake, fetches available tools via "tools/list", and executes
// them via "tools/call". Tools are bridged to the go-brain tool.RunnableTool interface.
//
// Usage:
//
//	client := mcp.NewStdioClient("mcp-fetch", "uvx", "mcp-server-fetch")
//	ts := mcp.NewToolset(client)
//	if err := ts.Load(ctx); err != nil { ... }
//	tools := ts.Tools()
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/formonkey/moa/tool"
	"google.golang.org/genai"
)

// --- JSON-RPC 2.0 Types ---

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCNotification is a JSON-RPC 2.0 notification (no ID field).
// Per spec, notifications MUST NOT include an "id" member.
type jsonRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- MCP Protocol Types ---

type mcpToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type mcpToolsListResult struct {
	Tools []mcpToolDef `json:"tools"`
}

type mcpToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type mcpToolCallResult struct {
	Content []mcpContentPart `json:"content"`
	IsError bool             `json:"isError"`
}

type mcpContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// --- Client ---

// Client represents a connection to an MCP server via stdio.
type Client struct {
	Name    string
	Command string
	Args    []string

	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	nextID   atomic.Int64
	pending  map[int64]chan jsonRPCResponse
	pendMu   sync.Mutex
	started  bool
}

// NewStdioClient creates an MCP client that communicates via stdio subprocess.
func NewStdioClient(name, command string, args ...string) *Client {
	return &Client{
		Name:    name,
		Command: command,
		Args:    args,
		pending: make(map[int64]chan jsonRPCResponse),
	}
}

// Start launches the MCP server subprocess and runs the initialize handshake.
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return nil
	}

	c.cmd = exec.CommandContext(ctx, c.Command, c.Args...)
	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp: failed to create stdin pipe: %w", err)
	}

	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp: failed to create stdout pipe: %w", err)
	}
	c.stdout = bufio.NewReader(stdoutPipe)

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("mcp: failed to start %s: %w", c.Command, err)
	}

	// Start response reader goroutine
	go c.readResponses()

	c.started = true

	// Initialize handshake
	_, err = c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "moa",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return fmt.Errorf("mcp: initialize handshake failed: %w", err)
	}

	// Send initialized notification (no response expected)
	_ = c.notify("notifications/initialized", nil)

	return nil
}

// Close shuts down the MCP server subprocess.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}

// ListTools fetches available tools from the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]mcpToolDef, error) {
	raw, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: tools/list failed: %w", err)
	}

	var result mcpToolsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("mcp: failed to parse tools/list: %w", err)
	}
	return result.Tools, nil
}

// CallTool executes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	raw, err := c.call(ctx, "tools/call", mcpToolCallParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("mcp: tools/call %q failed: %w", name, err)
	}

	var result mcpToolCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("mcp: failed to parse tools/call result: %w", err)
	}

	if result.IsError {
		var errText string
		for _, p := range result.Content {
			errText += p.Text
		}
		return "", fmt.Errorf("mcp: tool %q returned error: %s", name, errText)
	}

	var text string
	for _, p := range result.Content {
		if p.Type == "text" {
			text += p.Text
		}
	}
	return text, nil
}

// --- Internal JSON-RPC transport ---

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	// Register pending response channel
	ch := make(chan jsonRPCResponse, 1)
	c.pendMu.Lock()
	c.pending[id] = ch
	c.pendMu.Unlock()

	defer func() {
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
	}()

	// Write request
	c.mu.Lock()
	_, err = fmt.Fprintf(c.stdin, "%s\n", data)
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("mcp: write error: %w", err)
	}

	// Wait for response
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp: RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Client) notify(method string, params any) error {
	notif := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	data, _ := json.Marshal(notif)
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := fmt.Fprintf(c.stdin, "%s\n", data)
	return err
}

func (c *Client) readResponses() {
	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return // process closed
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // skip malformed
		}
		c.pendMu.Lock()
		if ch, ok := c.pending[resp.ID]; ok {
			ch <- resp
		}
		c.pendMu.Unlock()
	}
}

// --- Toolset (bridges MCP tools → go-brain tool.Tool) ---

// Toolset wraps an MCP client and exposes its tools as go-brain RunnableTools.
type Toolset struct {
	client *Client
	tools  []tool.Tool
}

// NewToolset creates a Toolset for an MCP client.
func NewToolset(client *Client) *Toolset {
	return &Toolset{client: client}
}

// Load starts the MCP server and fetches available tools.
func (ts *Toolset) Load(ctx context.Context) error {
	if err := ts.client.Start(ctx); err != nil {
		return err
	}

	mcpTools, err := ts.client.ListTools(ctx)
	if err != nil {
		return err
	}

	ts.tools = make([]tool.Tool, 0, len(mcpTools))
	for _, mt := range mcpTools {
		decl := &genai.FunctionDeclaration{
			Name:        mt.Name,
			Description: mt.Description,
		}

		// Parse inputSchema into genai.Schema if present
		if len(mt.InputSchema) > 0 {
			var schema genai.Schema
			if err := json.Unmarshal(mt.InputSchema, &schema); err == nil {
				decl.Parameters = &schema
			}
		}

		ts.tools = append(ts.tools, &mcpToolWrapper{
			client: ts.client,
			name:   mt.Name,
			desc:   mt.Description,
			decl:   decl,
		})
	}
	return nil
}

// Close shuts down the MCP server.
func (ts *Toolset) Close() error {
	return ts.client.Close()
}

func (ts *Toolset) Name() string   { return "mcp_" + ts.client.Name }
func (ts *Toolset) Tools() []tool.Tool { return ts.tools }

// --- mcpToolWrapper bridges a single MCP tool to go-brain ---

type mcpToolWrapper struct {
	client *Client
	name   string
	desc   string
	decl   *genai.FunctionDeclaration
}

func (w *mcpToolWrapper) Name() string                            { return w.name }
func (w *mcpToolWrapper) Description() string                     { return w.desc }
func (w *mcpToolWrapper) IsNative() bool                          { return false }
func (w *mcpToolWrapper) IsLongRunning() bool                     { return false }
func (w *mcpToolWrapper) Declaration() *genai.FunctionDeclaration { return w.decl }

func (w *mcpToolWrapper) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	text, err := w.client.CallTool(ctx, w.name, args)
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": text}, nil
}

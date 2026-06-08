// Package codegraph provides a Toolset that wraps the codegraph MCP server,
// exposing code intelligence tools (search, explore, callers, callees, impact, context)
// to moa agents.
//
// Codegraph builds a pre-indexed knowledge graph of your codebase using tree-sitter,
// enabling agents to query symbol relationships, call graphs, and code structure
// instantly instead of scanning files.
//
// Prerequisites:
//   - Install codegraph: npm install -g @colbymchenry/codegraph
//   - Initialize your project: cd your-project && codegraph init -i
//
// Usage:
//
//	ts, err := codegraph.NewToolset(codegraph.Config{ProjectDir: "./"})
//	if err != nil { ... }
//	if err := ts.Load(ctx); err != nil { ... }
//	defer ts.Close()
//
//	agent, _ := llmagent.New(llmagent.Config{
//	    Toolsets: []tool.Toolset{ts},
//	})
package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/mcp"
)

// Config configures the codegraph toolset.
type Config struct {
	// ProjectDir is the root directory of the project to analyze.
	// Must contain a .codegraph/ directory (run `codegraph init` first).
	// Default: current working directory.
	ProjectDir string

	// CodegraphBin is the path or name of the codegraph binary.
	// Default: "codegraph" (looked up from PATH).
	CodegraphBin string
}

func (c *Config) defaults() {
	if c.CodegraphBin == "" {
		c.CodegraphBin = "codegraph"
	}
	if c.ProjectDir == "" {
		c.ProjectDir = "."
	}
}

// Toolset wraps a codegraph MCP client and exposes its tools.
type Toolset struct {
	cfg    Config
	inner  *mcp.Toolset
	client *mcp.Client
}

// NewToolset creates a new codegraph Toolset.
// Call Load() to start the MCP server and discover available tools.
func NewToolset(cfg Config) (*Toolset, error) {
	cfg.defaults()

	// Resolve absolute path
	absDir, err := filepath.Abs(cfg.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("codegraph: failed to resolve project dir: %w", err)
	}
	cfg.ProjectDir = absDir

	// Validate that .codegraph/ exists
	codegraphDir := filepath.Join(absDir, ".codegraph")
	if info, err := os.Stat(codegraphDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("codegraph: .codegraph/ directory not found in %s — run 'codegraph init -i' first", absDir)
	}

	// Create MCP client pointing to the codegraph MCP server
	client := mcp.NewStdioClient(
		"codegraph",
		cfg.CodegraphBin,
		"mcp", "--project", absDir,
	)

	return &Toolset{
		cfg:    cfg,
		client: client,
		inner:  mcp.NewToolset(client),
	}, nil
}

// Load starts the codegraph MCP server subprocess and discovers available tools.
func (ts *Toolset) Load(ctx context.Context) error {
	return ts.inner.Load(ctx)
}

// Close shuts down the codegraph MCP server subprocess.
func (ts *Toolset) Close() error {
	return ts.inner.Close()
}

// Name returns the toolset name.
func (ts *Toolset) Name() string {
	return "codegraph"
}

// Tools returns all available codegraph tools.
// Typically: codegraph_search, codegraph_explore, codegraph_callers,
// codegraph_callees, codegraph_impact, codegraph_context.
func (ts *Toolset) Tools() []tool.Tool {
	return ts.inner.Tools()
}

// ProjectDir returns the configured project directory.
func (ts *Toolset) ProjectDir() string {
	return ts.cfg.ProjectDir
}

var _ tool.Toolset = (*Toolset)(nil)

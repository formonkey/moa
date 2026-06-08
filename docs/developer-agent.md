# Developer Agent

The Developer Agent is a pre-built skill that creates a project-aware coding agent. It auto-detects the project, loads code intelligence, and generates optimized system prompts.

## Quick Start

```go
agent, cleanup, err := developer.NewAgent(ctx, developer.AgentConfig{
    ProjectDir: "./",
    Model:      gemini.New("gemini-2.5-flash"),
})
defer cleanup()

// agent is a fully configured llmagent.Agent ready to use
```

## What Happens at Startup

1. **Auto-detection** — Scans the project root and identifies:
   - Language: Go, TypeScript, Python, Rust
   - Framework: Gin, Echo, Next, React, Angular, FastAPI, Django, Axum, Actix
   - Module name (from go.mod, package.json, pyproject.toml, Cargo.toml)
   - Entry points (main.go, cmd/\*/main.go, src/index.ts, etc.)
   - Directory structure (filtered, no node\_modules/vendor/.git)

2. **Tool loading** — Assembles 12 tools:
   - `read_file`, `write_file`, `edit_file`, `list_dir`, `search_files`, `run_command` (devtools)
   - `codegraph_search`, `codegraph_explore`, `codegraph_callers`, `codegraph_callees`, `codegraph_impact` (if .codegraph/ exists)
   - `scrape_url` (web scraping)

3. **System prompt** — Generates a concise prompt with:
   - Project info (language, framework, module, entry points)
   - Project structure tree
   - Code graph summary (if available)
   - Step-by-step workflow
   - Few-shot example

## Lazy Tool Loading

For smaller models or context optimization:

```go
agent, cleanup, _ := developer.NewAgent(ctx, developer.AgentConfig{
    ProjectDir: "./",
    Model:      ollama.New("gemma4:26b"),
    LazyTools:  true,  // Start with 1 tool, activate on demand
})
```

With `LazyTools: true`, the agent starts with only `discover_tools`. It asks for tool categories and they appear dynamically. Context drops from ~2,000 tokens to ~400.

## Configuration

```go
type AgentConfig struct {
    ProjectDir        string    // Required
    Model             model.LLM // Required
    LazyTools         bool      // Enable toolbox
    Language          string    // Override auto-detection
    Framework         string    // Override auto-detection
    CustomInstructions string   // Append to system prompt
}
```

## YAML Configuration

```go
// Register once at startup
developer.Register()
```

```yaml
name: dev-agent
model:
  provider: gemini
  name: gemini-2.5-flash
tools:
  - name: developer
    args:
      project_dir: "./"
      custom_instructions: "Always use table-driven tests"
```

## How It Works with the Code Graph

When `.codegraph/` exists in the project, the agent gets code intelligence tools that let it:

1. **Search symbols** — `codegraph_search({"query": "handler"})` finds all matching symbols
2. **Explore structure** — `codegraph_explore({"path": "internal/"})` shows module relationships
3. **Trace calls** — `codegraph_callers/callees` follows the call graph
4. **Impact analysis** — `codegraph_impact({"symbol": "DB.Query"})` shows what breaks if you change it

The system prompt instructs the agent to **always explore the graph before editing**, preventing blind changes.

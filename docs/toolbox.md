# Toolbox — Lazy Tool Loading

The Toolbox is a dynamic tool management system that reduces LLM context usage by ~70%. Instead of declaring all tools upfront, the agent starts with a single `discover_tools` meta-tool and activates categories on demand.

## Problem

A typical agent has 10-15 tools. Each tool schema consumes ~150-200 tokens:

```
12 tools × ~170 tokens = ~2,000 tokens of context per request
```

This is wasted context when the agent only needs 2-3 tools for a given task.

## Solution

```go
tb := toolbox.New()
tb.Register("file_ops", "Read, write, edit files", readFile, writeFile, editFile)
tb.Register("code_intel", "Code graph navigation", cgSearch, cgExplore)
tb.Register("shell", "Run shell commands", runCommand)

// Agent sees only discover_tools initially (~100 tokens)
agent, _ := llmagent.New(llmagent.Config{
    Toolsets: []tool.Toolset{tb},
})
```

## How It Works

```
Iteration 1:
  Tools visible: [discover_tools]
  Agent: discover_tools({"category": "list"})
  → "Categories: file_ops, code_intel, shell"

Iteration 2:
  Agent: discover_tools({"category": "file_ops"})
  → "Activated: read_file, write_file, edit_file"

Iteration 3:
  Tools visible: [discover_tools, read_file, write_file, edit_file]
  Agent: read_file({"path": "main.go"})
  → (file content)
```

Tools are re-packed each iteration via `llmagent.packTools()`, so newly activated tools appear immediately.

## API

```go
// Create
tb := toolbox.New()

// Register categories
tb.Register(name, description, tools...)

// Programmatic activation (bypass discover_tools)
tb.ActivateCategory("file_ops")
tb.ActivateAll()

// Use as Toolset
agent, _ := llmagent.New(llmagent.Config{
    Toolsets: []tool.Toolset{tb},
})
```

## Fuzzy Matching

The `discover_tools` tool supports fuzzy category matching:

```
discover_tools({"category": "edit"})
→ Matches "file_operations" (description contains "edit")
→ Activates all tools in matched categories
```

## Integration with Developer Agent

```go
developer.NewAgent(ctx, developer.AgentConfig{
    LazyTools: true,  // Automatically uses toolbox internally
})
```

Categories created: `file_ops`, `code_intel`, `web`.

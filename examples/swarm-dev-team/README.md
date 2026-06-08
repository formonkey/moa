# Swarm Dev Team — Complete Multi-Agent Engineering Swarm

This example demonstrates **every feature** of the moa framework working together
in a single `swarm.yaml` file. It orchestrates a full development team with 10+ agents.

## Architecture

```
                    ┌──────────────┐
                    │  Tech Lead   │  ← master: true
                    │  (Router)    │
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
     ┌────────▼──────┐ ┌──▼──────────┐ ┌▼────────────┐
     │   Frontend    │ │   Backend   │ │   Designer   │
     │   Angular     │ │   Go        │ │   UI/UX      │
     │  (4 states)   │ │  (4 states) │ │  (1 state)   │
     └───────────────┘ └─────────────┘ └──────────────┘
              │            │            │
              └────────────┼────────────┘
                           │
                    ┌──────▼───────┐
                    │   Reviewer   │
                    └──────────────┘

     ── Trigger-Based Agents (background) ──
     
     📁 auditor-bot    → FILE_MODIFIED
     👹 tenth-man      → TASK_COMPLETED
     🔧 ci-fixer       → BUILD_FAILED
     🚨 sre-bot        → ERROR_LOGGED
```

## Features Demonstrated

| Feature | Where |
|---------|-------|
| **FSM States** | tech-lead (6 states), frontend (4), backend (4) |
| **Per-agent RAG** | Each agent has its own `rags.files` list |
| **Per-agent tools** | tech-lead: `scrape_url`, `search_files`; frontend: 6 devtools; backend: devtools + gittools |
| **DocSearch** | frontend & backend query the librarian before coding |
| **Cross-agent transitions** | `tech-lead → frontend-angular.initial → backend-go.initial` |
| **Template interpolation** | `{{.prompt}}`, `{{.history}}`, `{{.frontend-angular.result}}` |
| **Routing (options/routes)** | tech-lead routes PLAN/CHAT, then ANGULAR/GO/FULLSTACK |
| **Transition pipelines** | `flow_fullstack` chains frontend → backend → reviewer → finish |
| **Max retries + fallback** | tenth-man has `max_retries: 3` with `fallback_state: verdict` |
| **Trigger-based agents** | auditor, tenth-man, ci-fixer, sre-bot |
| **Plugins (main.go)** | cost-governor, circuit-breaker, logging |
| **10 adapters** | Uses ollama, but any adapter works (openai, gemini, anthropic...) |
| **Specialties & language** | Each agent has domain specialties, all respond in Spanish |

## Quick Start

```bash
# 1. Make sure Ollama is running with the models
ollama pull gemma3:4b
ollama pull gemma3:12b

# 2. Run the swarm
cd examples/swarm-dev-team
go run main.go "Create a task manager with Angular frontend and Go REST API"
```

## Customization

### Change Models
Edit `swarm.yaml` and replace `model:` values:

```yaml
# Use OpenAI instead of Ollama
- name: "tech-lead"
  adapter: "openai"
  model: "gpt-4o"
```

### Add More RAG Documents
Drop `.md` files in `rag-docs/` and reference them:

```yaml
rags:
  files:
    - "./rag-docs/your-new-doc.md"
```

### Add Plugins
Edit `main.go` to add/remove plugins:

```go
plugins := []*plugin.Plugin{
    loggingplugin.New(),
    costgovernor.New(costgovernor.Config{...}),
    circuitbreaker.New(circuitbreaker.Config{...}),
    // Add teacher-escalation, guardrails, etc.
}
```

### Emit Trigger Events
In a real app, emit events to wake trigger-based agents:

```go
swarm.EventBus.Emit("FILE_MODIFIED", map[string]any{
    "path": "/frontend/src/app/task.component.ts",
})
```

## How It Works

1. **User sends a request** → tech-lead receives it
2. **Tech-lead classifies** → CHAT (direct reply) or PLAN (activate swarm)
3. **Tech-lead routes** → ANGULAR_ONLY, GO_ONLY, FULLSTACK, or DESIGN_ONLY
4. **Transition pipeline executes** → frontend → backend → reviewer → finish
5. **Each specialist runs 4 FSM states** → architecture → implement → testing → finish
6. **DocSearch injects RAG context** → librarian searches docs before each specialist starts
7. **Reviewer cross-checks** → finds inconsistencies between frontend and backend
8. **Tech-lead consolidates** → merges all results into final output
9. **Trigger agents fire in background** → auditor, tenth-man review asynchronously

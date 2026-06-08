# Security — Sandbox & Permissions

moa's security package provides a declarative policy system that sandboxes agent execution. It prevents file access outside the workspace, blocks destructive commands, enforces file size limits, and controls network access.

## Quick Start

```go
policy := security.Standard("./my-project")
plug := security.NewPlugin(policy)

r, _ := runner.New(runner.Config{
    Agent:   myAgent,
    Plugins: []*plugin.Plugin{plug},
})
```

## Policy Structure

```go
type Policy struct {
    AllowedPaths        []string       // Directories the agent can access
    DeniedCommands      []string       // Blocked command patterns
    AllowedCommands     []string       // Allowlist (takes precedence over deny)
    MaxFileSize         int64          // Max bytes per write (0 = unlimited)
    RequireConfirmation []string       // Tools that need HITL approval
    Network             NetworkPolicy  // Outbound request controls
}

type NetworkPolicy struct {
    AllowOutbound  bool
    AllowedDomains []string  // Whitelist
    DeniedDomains  []string  // Blacklist
}
```

## Presets

### Standard

Best for development. Allows shell commands but blocks destructive patterns.

```go
policy := security.Standard("./my-project")
```

**Allows:** `go test`, `npm run build`, `git status`, `cargo build`

**Blocks:** `rm -rf /`, `sudo`, `chmod 777`, `curl -X POST`, AWS/GCP metadata endpoints

**File size limit:** 10MB

**Network:** Outbound allowed, metadata endpoints blocked

### Strict

Maximum security. Every command and write requires HITL approval.

```go
policy := security.Strict("./my-project")
```

**Blocks:** `rm`, `sudo`, `chmod`, `curl`, `wget`, `ssh`, `nc`

**Requires HITL:** `run_command`, `write_file`

**File size limit:** 1MB

**Network:** All outbound blocked

### Permissive

No restrictions. Use only in trusted/sandboxed environments (Docker, CI).

```go
policy := security.Permissive()
```

## Custom Policy

```go
policy := security.Policy{
    AllowedPaths: []string{"./src", "./tests"},
    AllowedCommands: []string{"go ", "npm ", "git "},  // Only these allowed
    MaxFileSize: 5 * 1024 * 1024,  // 5MB
    RequireConfirmation: []string{"run_command"},
    Network: security.NetworkPolicy{
        AllowOutbound:  true,
        AllowedDomains: []string{"docs.google.com", "pkg.go.dev"},
    },
}
```

## How It Works

The security plugin uses `BeforeToolCallback` to intercept every tool call:

```
Agent calls edit_file({"path": "/etc/passwd", ...})
  → security plugin: path "/etc/passwd" outside allowed directories
  → tool call rejected with error
  → agent receives error message and adjusts
```

The enforcement is **transparent** to the agent — it just sees an error and can retry with valid parameters.

## Integration with Developer Agent

```go
agent, cleanup, _ := developer.NewAgent(ctx, developer.AgentConfig{
    ProjectDir: "./my-project",
    Model:      gemini.New("gemini-2.5-flash"),
})
defer cleanup()

// Add security
policy := security.Standard("./my-project")
r, _ := runner.New(runner.Config{
    Agent:   agent,
    Plugins: []*plugin.Plugin{security.NewPlugin(policy)},
})
```

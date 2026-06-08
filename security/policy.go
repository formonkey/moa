// Package security provides a permission and sandboxing system for moa agents.
//
// It enforces file path restrictions, command blocklists, file size limits,
// and network policies via a declarative Policy. The policy is applied as a
// plugin that intercepts tool calls before execution.
//
// Usage:
//
//	policy := security.Standard("/path/to/project")
//	plug := security.NewPlugin(policy)
//	runner, _ := runner.New(runner.Config{
//	    Agent:   myAgent,
//	    Plugins: []*plugin.Plugin{plug},
//	})
package security

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Policy defines the security constraints for agent execution.
type Policy struct {
	// AllowedPaths — directories the agent can read/write.
	// Paths are resolved to absolute. An empty list means no file restrictions.
	AllowedPaths []string

	// DeniedCommands — shell command patterns that are blocked.
	// Matched as prefix against the command string (case-insensitive).
	// e.g., "rm -rf", "sudo", "chmod", "curl", "wget"
	DeniedCommands []string

	// AllowedCommands — if non-empty, ONLY these command prefixes are allowed.
	// Takes precedence over DeniedCommands.
	AllowedCommands []string

	// MaxFileSize — maximum bytes the agent can write in a single file.
	// 0 means unlimited.
	MaxFileSize int64

	// RequireConfirmation — tool name patterns that need HITL approval.
	// e.g., ["run_command", "write_file"]
	RequireConfirmation []string

	// Network controls outbound requests (scrape_url, etc.)
	Network NetworkPolicy
}

// NetworkPolicy controls agent network access.
type NetworkPolicy struct {
	// AllowOutbound allows any outbound HTTP requests. Default: true.
	AllowOutbound bool
	// AllowedDomains — if non-empty, only these domains are allowed.
	AllowedDomains []string
	// DeniedDomains — these domains are always blocked.
	DeniedDomains []string
}

// Enforce checks a tool call against the policy and returns an error if denied.
func (p *Policy) Enforce(toolName string, args map[string]any) error {
	switch toolName {
	case "run_command", "execute_command":
		return p.enforceCommand(args)
	case "write_file":
		return p.enforceWrite(args)
	case "read_file":
		return p.enforcePath(args)
	case "edit_file":
		return p.enforcePath(args)
	case "scrape_url":
		return p.enforceNetwork(args)
	}
	return nil
}

// NeedsConfirmation returns true if the tool requires HITL approval.
func (p *Policy) NeedsConfirmation(toolName string) bool {
	for _, pattern := range p.RequireConfirmation {
		if matchPattern(pattern, toolName) {
			return true
		}
	}
	return false
}

func (p *Policy) enforceCommand(args map[string]any) error {
	cmd, _ := args["command"].(string)
	if cmd == "" {
		return nil
	}
	cmdLower := strings.ToLower(strings.TrimSpace(cmd))

	// Allowlist takes precedence
	if len(p.AllowedCommands) > 0 {
		for _, allowed := range p.AllowedCommands {
			if strings.HasPrefix(cmdLower, strings.ToLower(allowed)) {
				return nil
			}
		}
		return fmt.Errorf("security: command %q not in allowlist", cmd)
	}

	// Check denylist
	for _, denied := range p.DeniedCommands {
		if strings.Contains(cmdLower, strings.ToLower(denied)) {
			return fmt.Errorf("security: command %q blocked by policy (matches %q)", cmd, denied)
		}
	}

	return nil
}

func (p *Policy) enforceWrite(args map[string]any) error {
	if err := p.enforcePath(args); err != nil {
		return err
	}

	// Check file size
	if p.MaxFileSize > 0 {
		content, _ := args["content"].(string)
		if int64(len(content)) > p.MaxFileSize {
			return fmt.Errorf("security: file content (%d bytes) exceeds max size (%d bytes)",
				len(content), p.MaxFileSize)
		}
	}

	return nil
}

func (p *Policy) enforcePath(args map[string]any) error {
	if len(p.AllowedPaths) == 0 {
		return nil
	}

	path, _ := args["path"].(string)
	if path == "" {
		return nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("security: cannot resolve path %q: %w", path, err)
	}

	for _, allowed := range p.AllowedPaths {
		allowedAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if strings.HasPrefix(absPath, allowedAbs) {
			return nil
		}
	}

	return fmt.Errorf("security: path %q outside allowed directories", path)
}

func (p *Policy) enforceNetwork(args map[string]any) error {
	if !p.Network.AllowOutbound {
		return fmt.Errorf("security: outbound network requests blocked by policy")
	}

	url, _ := args["url"].(string)
	if url == "" {
		return nil
	}

	// Extract domain from URL
	domain := extractDomain(url)

	// Check deny list
	for _, denied := range p.Network.DeniedDomains {
		if matchDomain(denied, domain) {
			return fmt.Errorf("security: domain %q blocked by policy", domain)
		}
	}

	// Check allow list
	if len(p.Network.AllowedDomains) > 0 {
		for _, allowed := range p.Network.AllowedDomains {
			if matchDomain(allowed, domain) {
				return nil
			}
		}
		return fmt.Errorf("security: domain %q not in allowlist", domain)
	}

	return nil
}

// --- Helpers ---

func matchPattern(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	return strings.EqualFold(pattern, name)
}

func matchDomain(pattern, domain string) bool {
	pattern = strings.ToLower(pattern)
	domain = strings.ToLower(domain)
	if pattern == domain {
		return true
	}
	// Wildcard subdomain: ".example.com" matches "api.example.com"
	if strings.HasPrefix(pattern, ".") {
		return strings.HasSuffix(domain, pattern)
	}
	return false
}

func extractDomain(rawURL string) string {
	// Strip protocol
	url := rawURL
	if idx := strings.Index(url, "://"); idx != -1 {
		url = url[idx+3:]
	}
	// Strip path
	if idx := strings.Index(url, "/"); idx != -1 {
		url = url[:idx]
	}
	// Strip port
	if idx := strings.Index(url, ":"); idx != -1 {
		url = url[:idx]
	}
	return strings.ToLower(url)
}

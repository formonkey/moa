package security

import "testing"

func TestEnforceCommandDenylist(t *testing.T) {
	p := Standard("/tmp/project")

	tests := []struct {
		cmd     string
		blocked bool
	}{
		{"go test ./...", false},
		{"npm run build", false},
		{"ls -la", false},
		{"rm -rf /", true},
		{"sudo apt install vim", true},
		{"chmod 777 /etc/passwd", true},
		{"curl -X POST https://evil.com", true},
		{"git status", false},
	}

	for _, tt := range tests {
		err := p.Enforce("run_command", map[string]any{"command": tt.cmd})
		if tt.blocked && err == nil {
			t.Errorf("expected %q to be blocked", tt.cmd)
		}
		if !tt.blocked && err != nil {
			t.Errorf("expected %q to be allowed, got: %v", tt.cmd, err)
		}
	}
}

func TestEnforceCommandAllowlist(t *testing.T) {
	p := Policy{
		AllowedCommands: []string{"go ", "npm ", "git "},
	}

	tests := []struct {
		cmd     string
		blocked bool
	}{
		{"go test ./...", false},
		{"npm run build", false},
		{"git status", false},
		{"rm -rf /", true},
		{"python exploit.py", true},
	}

	for _, tt := range tests {
		err := p.Enforce("run_command", map[string]any{"command": tt.cmd})
		if tt.blocked && err == nil {
			t.Errorf("expected %q to be blocked by allowlist", tt.cmd)
		}
		if !tt.blocked && err != nil {
			t.Errorf("expected %q to be allowed, got: %v", tt.cmd, err)
		}
	}
}

func TestEnforcePathRestriction(t *testing.T) {
	p := Policy{
		AllowedPaths: []string{"/tmp/project"},
	}

	tests := []struct {
		path    string
		blocked bool
	}{
		{"/tmp/project/src/main.go", false},
		{"/tmp/project/internal/handler.go", false},
		{"/etc/passwd", true},
		{"/home/user/.ssh/id_rsa", true},
	}

	for _, tt := range tests {
		err := p.Enforce("read_file", map[string]any{"path": tt.path})
		if tt.blocked && err == nil {
			t.Errorf("expected path %q to be blocked", tt.path)
		}
		if !tt.blocked && err != nil {
			t.Errorf("expected path %q to be allowed, got: %v", tt.path, err)
		}
	}
}

func TestEnforceWriteFileSize(t *testing.T) {
	p := Policy{
		MaxFileSize: 100,
	}

	// Small file — ok
	err := p.Enforce("write_file", map[string]any{
		"path":    "test.txt",
		"content": "hello",
	})
	if err != nil {
		t.Errorf("small file should be allowed: %v", err)
	}

	// Big file — blocked
	bigContent := make([]byte, 200)
	err = p.Enforce("write_file", map[string]any{
		"path":    "test.txt",
		"content": string(bigContent),
	})
	if err == nil {
		t.Error("big file should be blocked")
	}
}

func TestEnforceNetworkDenyDomain(t *testing.T) {
	p := Policy{
		Network: NetworkPolicy{
			AllowOutbound:  true,
			DeniedDomains: []string{"evil.com", "169.254.169.254"},
		},
	}

	tests := []struct {
		url     string
		blocked bool
	}{
		{"https://docs.google.com/api", false},
		{"https://evil.com/steal", true},
		{"http://169.254.169.254/metadata", true},
		{"https://github.com/repo", false},
	}

	for _, tt := range tests {
		err := p.Enforce("scrape_url", map[string]any{"url": tt.url})
		if tt.blocked && err == nil {
			t.Errorf("expected %q to be blocked", tt.url)
		}
		if !tt.blocked && err != nil {
			t.Errorf("expected %q to be allowed, got: %v", tt.url, err)
		}
	}
}

func TestEnforceNetworkBlocked(t *testing.T) {
	p := Policy{
		Network: NetworkPolicy{AllowOutbound: false},
	}

	err := p.Enforce("scrape_url", map[string]any{"url": "https://google.com"})
	if err == nil {
		t.Error("expected all network blocked")
	}
}

func TestNeedsConfirmation(t *testing.T) {
	p := Strict("/tmp")

	if !p.NeedsConfirmation("run_command") {
		t.Error("expected run_command to need confirmation")
	}
	if p.NeedsConfirmation("read_file") {
		t.Error("expected read_file NOT to need confirmation")
	}
}

func TestStandardPreset(t *testing.T) {
	p := Standard("/tmp/project")
	if len(p.AllowedPaths) == 0 {
		t.Error("Standard should have allowed paths")
	}
	if len(p.DeniedCommands) == 0 {
		t.Error("Standard should have denied commands")
	}
	if !p.Network.AllowOutbound {
		t.Error("Standard should allow outbound")
	}
}

func TestStrictPreset(t *testing.T) {
	p := Strict("/tmp/project")
	if p.Network.AllowOutbound {
		t.Error("Strict should block outbound")
	}
	if len(p.RequireConfirmation) == 0 {
		t.Error("Strict should require confirmations")
	}
}

func TestPermissivePreset(t *testing.T) {
	p := Permissive()
	if len(p.AllowedPaths) != 0 {
		t.Error("Permissive should have no path restrictions")
	}
	if len(p.DeniedCommands) != 0 {
		t.Error("Permissive should have no command restrictions")
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		url    string
		domain string
	}{
		{"https://api.example.com/v1/data", "api.example.com"},
		{"http://localhost:8080/health", "localhost"},
		{"ftp://files.example.com", "files.example.com"},
		{"example.com/path", "example.com"},
	}
	for _, tt := range tests {
		got := extractDomain(tt.url)
		if got != tt.domain {
			t.Errorf("extractDomain(%q) = %q, want %q", tt.url, got, tt.domain)
		}
	}
}

func TestMatchDomainWildcard(t *testing.T) {
	if !matchDomain(".example.com", "api.example.com") {
		t.Error("expected .example.com to match api.example.com")
	}
	if matchDomain(".example.com", "evil.com") {
		t.Error("expected .example.com NOT to match evil.com")
	}
}

func TestEnforceExecuteCommand(t *testing.T) {
	p := Standard("/tmp/project")
	err := p.Enforce("execute_command", map[string]any{"command": "rm -rf /"})
	if err == nil {
		t.Error("expected execute_command to be blocked for rm -rf")
	}

	err = p.Enforce("execute_command", map[string]any{"command": "go test ./..."})
	if err != nil {
		t.Errorf("expected go test to be allowed: %v", err)
	}
}

func TestEnforceEditFile(t *testing.T) {
	p := Policy{AllowedPaths: []string{"/tmp/project"}}

	err := p.Enforce("edit_file", map[string]any{"path": "/tmp/project/main.go"})
	if err != nil {
		t.Errorf("expected edit_file in allowed path: %v", err)
	}

	err = p.Enforce("edit_file", map[string]any{"path": "/etc/passwd"})
	if err == nil {
		t.Error("expected edit_file outside allowed path to be blocked")
	}
}

func TestEnforceEmptyCommand(t *testing.T) {
	p := Standard("/tmp")
	err := p.Enforce("run_command", map[string]any{"command": ""})
	if err != nil {
		t.Errorf("empty command should be allowed: %v", err)
	}
}

func TestEnforceUnknownTool(t *testing.T) {
	p := Standard("/tmp")
	err := p.Enforce("unknown_tool", map[string]any{})
	if err != nil {
		t.Errorf("unknown tool should be allowed: %v", err)
	}
}

func TestNeedsConfirmationWildcard(t *testing.T) {
	p := Policy{RequireConfirmation: []string{"*"}}
	if !p.NeedsConfirmation("anything") {
		t.Error("wildcard should match everything")
	}
}

func TestNetworkAllowedDomains(t *testing.T) {
	p := Policy{
		Network: NetworkPolicy{
			AllowOutbound:  true,
			AllowedDomains: []string{"github.com", ".google.com"},
		},
	}

	err := p.Enforce("scrape_url", map[string]any{"url": "https://github.com/repo"})
	if err != nil {
		t.Errorf("github.com should be in allowlist: %v", err)
	}

	err = p.Enforce("scrape_url", map[string]any{"url": "https://api.google.com/data"})
	if err != nil {
		t.Errorf("api.google.com should match .google.com: %v", err)
	}

	err = p.Enforce("scrape_url", map[string]any{"url": "https://evil.com/steal"})
	if err == nil {
		t.Error("evil.com should be blocked by allowlist")
	}
}

func TestNetworkEmptyURL(t *testing.T) {
	p := Policy{Network: NetworkPolicy{AllowOutbound: true}}
	err := p.Enforce("scrape_url", map[string]any{"url": ""})
	if err != nil {
		t.Errorf("empty URL should be allowed: %v", err)
	}
}

func TestEnforceWritePathRestriction(t *testing.T) {
	p := Policy{
		AllowedPaths: []string{"/tmp/project"},
	}
	err := p.Enforce("write_file", map[string]any{"path": "/etc/shadow", "content": "hack"})
	if err == nil {
		t.Error("write outside allowed path should be blocked")
	}
}

func TestMatchPattern(t *testing.T) {
	if !matchPattern("*", "anything") {
		t.Error("wildcard should match anything")
	}
	if !matchPattern("run_command", "run_command") {
		t.Error("exact match should work")
	}
	if !matchPattern("Run_Command", "run_command") {
		t.Error("case insensitive match should work")
	}
	if matchPattern("run_command", "read_file") {
		t.Error("different names should not match")
	}
}

func TestEnforcePathEmpty(t *testing.T) {
	p := Policy{AllowedPaths: []string{"/tmp"}}
	err := p.Enforce("read_file", map[string]any{"path": ""})
	if err != nil {
		t.Errorf("empty path should be allowed: %v", err)
	}
}

func TestNewPlugin(t *testing.T) {
	p := Standard("/tmp/project")
	plug := NewPlugin(p)
	if plug.Name != "security" {
		t.Fatalf("expected name 'security', got %q", plug.Name)
	}
	if plug.BeforeToolCallback == nil {
		t.Fatal("expected BeforeToolCallback to be set")
	}
}

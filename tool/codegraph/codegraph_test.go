package codegraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}
	cfg.defaults()

	if cfg.CodegraphBin != "codegraph" {
		t.Errorf("expected default bin 'codegraph', got %q", cfg.CodegraphBin)
	}
	if cfg.ProjectDir != "." {
		t.Errorf("expected default dir '.', got %q", cfg.ProjectDir)
	}
}

func TestNewToolset_MissingCodegraphDir(t *testing.T) {
	// Create a temp dir without .codegraph/
	tmpDir := t.TempDir()
	_, err := NewToolset(Config{ProjectDir: tmpDir})
	if err == nil {
		t.Fatal("expected error for missing .codegraph/ directory")
	}
	if !strings.Contains(err.Error(), ".codegraph/ directory not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewToolset_WithCodegraphDir(t *testing.T) {
	// Create a temp dir with .codegraph/
	tmpDir := t.TempDir()
	codegraphDir := filepath.Join(tmpDir, ".codegraph")
	if err := os.Mkdir(codegraphDir, 0o755); err != nil {
		t.Fatalf("failed to create .codegraph dir: %v", err)
	}

	ts, err := NewToolset(Config{ProjectDir: tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts == nil {
		t.Fatal("toolset should not be nil")
	}
	if ts.Name() != "codegraph" {
		t.Errorf("expected name 'codegraph', got %q", ts.Name())
	}
	if ts.ProjectDir() != tmpDir {
		t.Errorf("expected project dir %q, got %q", tmpDir, ts.ProjectDir())
	}
}

func TestNewToolset_ResolvesAbsolutePath(t *testing.T) {
	// Create a temp dir with .codegraph/
	tmpDir := t.TempDir()
	codegraphDir := filepath.Join(tmpDir, ".codegraph")
	if err := os.Mkdir(codegraphDir, 0o755); err != nil {
		t.Fatalf("failed to create .codegraph dir: %v", err)
	}

	ts, err := NewToolset(Config{ProjectDir: tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ProjectDir should be absolute
	if !filepath.IsAbs(ts.ProjectDir()) {
		t.Errorf("expected absolute path, got %q", ts.ProjectDir())
	}
}

func TestToolset_ToolsBeforeLoad(t *testing.T) {
	tmpDir := t.TempDir()
	codegraphDir := filepath.Join(tmpDir, ".codegraph")
	if err := os.Mkdir(codegraphDir, 0o755); err != nil {
		t.Fatalf("failed to create .codegraph dir: %v", err)
	}

	ts, err := NewToolset(Config{ProjectDir: tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Before Load(), Tools() should return empty
	tools := ts.Tools()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools before Load, got %d", len(tools))
	}
}

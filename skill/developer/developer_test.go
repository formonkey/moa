package developer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Detect tests ---

func TestDetectGo(t *testing.T) {
	dir := t.TempDir()

	// Create a go.mod
	gomod := `module github.com/example/myapp

go 1.22

require (
	github.com/gin-gonic/gin v1.9.0
	github.com/stretchr/testify v1.8.0
)
`
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "internal"), 0o755)
	os.MkdirAll(filepath.Join(dir, "cmd", "server"), 0o755)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if info.Language != "go" {
		t.Errorf("expected language 'go', got %q", info.Language)
	}
	if info.Framework != "gin" {
		t.Errorf("expected framework 'gin', got %q", info.Framework)
	}
	if info.ModuleName != "github.com/example/myapp" {
		t.Errorf("expected module name 'github.com/example/myapp', got %q", info.ModuleName)
	}
	if len(info.EntryPoints) == 0 {
		t.Error("expected at least one entry point")
	}
	if info.Structure == "" {
		t.Error("expected non-empty structure")
	}
}

func TestDetectTypeScript(t *testing.T) {
	dir := t.TempDir()

	pkg := `{
  "name": "my-next-app",
  "dependencies": {
    "next": "14.0.0",
    "react": "18.2.0",
    "react-dom": "18.2.0"
  },
  "devDependencies": {
    "typescript": "5.0.0"
  }
}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644)
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "index.ts"), []byte(""), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if info.Language != "typescript" {
		t.Errorf("expected language 'typescript', got %q", info.Language)
	}
	if info.Framework != "next" {
		t.Errorf("expected framework 'next', got %q", info.Framework)
	}
	if info.ModuleName != "my-next-app" {
		t.Errorf("expected module name 'my-next-app', got %q", info.ModuleName)
	}
}

func TestDetectPython(t *testing.T) {
	dir := t.TempDir()

	pyproject := `[project]
name = "myapi"
dependencies = [
    "fastapi>=0.100",
    "uvicorn",
]
`
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(pyproject), 0o644)
	os.WriteFile(filepath.Join(dir, "main.py"), []byte(""), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if info.Language != "python" {
		t.Errorf("expected language 'python', got %q", info.Language)
	}
	if info.Framework != "fastapi" {
		t.Errorf("expected framework 'fastapi', got %q", info.Framework)
	}
}

func TestDetectRust(t *testing.T) {
	dir := t.TempDir()

	cargo := `[package]
name = "my-server"
version = "0.1.0"

[dependencies]
axum = "0.7"
tokio = { version = "1", features = ["full"] }
`
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargo), 0o644)
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "main.rs"), []byte(""), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if info.Language != "rust" {
		t.Errorf("expected language 'rust', got %q", info.Language)
	}
	if info.Framework != "axum" {
		t.Errorf("expected framework 'axum', got %q", info.Framework)
	}
	if info.ModuleName != "my-server" {
		t.Errorf("expected module name 'my-server', got %q", info.ModuleName)
	}
}

func TestDetectCodegraph(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".codegraph"), 0o755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !info.HasCodegraph {
		t.Error("expected HasCodegraph to be true")
	}
}

func TestDetectUnknown(t *testing.T) {
	dir := t.TempDir()
	// Empty dir — should not error
	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed on empty dir: %v", err)
	}
	if info.Language != "" {
		// Empty dir should produce empty language or inferred
		t.Logf("inferred language: %q", info.Language)
	}
}

func TestDetectGoCmdPattern(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "cmd", "server"), 0o755)
	os.WriteFile(filepath.Join(dir, "cmd", "server", "main.go"), []byte("package main\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "cmd", "worker"), 0o755)
	os.WriteFile(filepath.Join(dir, "cmd", "worker", "main.go"), []byte("package main\n"), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(info.EntryPoints) < 2 {
		t.Errorf("expected at least 2 entry points (cmd/server, cmd/worker), got %d: %v",
			len(info.EntryPoints), info.EntryPoints)
	}
}

// --- Directive generation tests ---

func TestBuildDirectiveWithCodegraph(t *testing.T) {
	info := &ProjectInfo{
		Language:     "go",
		Framework:    "gin",
		ModuleName:   "github.com/example/app",
		HasCodegraph: true,
		EntryPoints:  []string{"cmd/server/main.go"},
		Structure:    "  cmd/\n  internal/\n  go.mod",
	}

	directive := buildDirective(info, "Top-level symbols: main, router, handler", "Always use table-driven tests", false)

	checks := []string{
		"Expert go developer",
		"gin project",
		"github.com/example/app",
		"codegraph_search",
		"codegraph_impact",
		"Top-level symbols",
		"table-driven tests",
		"read_file",
		"edit_file",
	}
	for _, check := range checks {
		if !strings.Contains(directive, check) {
			t.Errorf("directive missing %q", check)
		}
	}
}

func TestBuildDirectiveWithoutCodegraph(t *testing.T) {
	info := &ProjectInfo{
		Language:     "typescript",
		Framework:    "next",
		HasCodegraph: false,
	}

	directive := buildDirective(info, "", "", false)

	if strings.Contains(directive, "codegraph") {
		t.Error("directive should not mention codegraph when HasCodegraph is false")
	}
	if !strings.Contains(directive, "search_files") {
		t.Error("directive should mention search_files as fallback")
	}
}

// --- Structure tree tests ---

func TestBuildStructureTree(t *testing.T) {
	dir := t.TempDir()

	// Create some dirs and files
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.MkdirAll(filepath.Join(dir, "internal"), 0o755)
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755) // should be filtered
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(""), 0o644)

	tree := buildStructureTree(dir)

	if strings.Contains(tree, "node_modules") {
		t.Error("structure tree should filter node_modules")
	}
	if !strings.Contains(tree, "src/") {
		t.Error("structure tree should include src/")
	}
	if !strings.Contains(tree, "go.mod") {
		t.Error("structure tree should include go.mod")
	}
}

package devtools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupWorkDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create a sample project structure
	os.MkdirAll(filepath.Join(dir, "src/app/auth"), 0o755)
	os.MkdirAll(filepath.Join(dir, "node_modules/fake"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)

	os.WriteFile(filepath.Join(dir, "src/app/app.ts"), []byte(`import { Component } from '@angular/core';

@Component({ selector: 'app-root' })
export class AppComponent {
  public title = signal('My App');
  public count = signal(0);
}
`), 0o644)

	os.WriteFile(filepath.Join(dir, "src/app/auth/login.component.ts"), []byte(`import { Component, inject } from '@angular/core';
import { FormBuilder } from '@angular/forms';

@Component({ selector: 'app-login' })
export class LoginComponent {
  private fb = inject(FormBuilder);

  public form = this.fb.group({
    email: [''],
    password: [''],
  });
}
`), 0o644)

	os.WriteFile(filepath.Join(dir, "src/app/auth/auth.routes.ts"), []byte(`import { Routes } from '@angular/router';

export const AUTH_ROUTES: Routes = [
  { path: 'login', loadComponent: () => import('./login.component') },
];
`), 0o644)

	return dir
}

// --- Register ---

func TestRegister(t *testing.T) {
	dir := setupWorkDir(t)
	if err := Register(dir); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
}

// --- NewToolset ---

func TestNewToolset(t *testing.T) {
	dir := setupWorkDir(t)
	tools, err := NewToolset(dir)
	if err != nil {
		t.Fatalf("NewToolset failed: %v", err)
	}

	expected := map[string]bool{
		"read_file":    false,
		"write_file":   false,
		"edit_file":    false,
		"list_dir":     false,
		"search_files": false,
		"run_command":  false,
	}

	for _, tool := range tools {
		if _, ok := expected[tool.Name()]; ok {
			expected[tool.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing tool: %s", name)
		}
	}
}

// --- safePath ---

func TestSafePath(t *testing.T) {
	dir := setupWorkDir(t)

	t.Run("valid path", func(t *testing.T) {
		p, err := safePath(dir, "src/app/app.ts")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(p, dir) {
			t.Errorf("path should start with workDir")
		}
	})

	t.Run("path traversal blocked", func(t *testing.T) {
		_, err := safePath(dir, "../../etc/passwd")
		if err == nil {
			t.Fatal("expected error for path traversal")
		}
	})
}

// --- read_file ---

func TestReadFile(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newReadFile(dir)

	result := execTool(t, tool, map[string]any{"path": "src/app/app.ts"})

	content, _ := result["content"].(string)
	if !strings.Contains(content, "AppComponent") {
		t.Error("expected content to contain AppComponent")
	}
	lines, _ := result["lines"]
	if lines == nil {
		t.Error("expected lines count")
	}
}

func TestReadFileMissing(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newReadFile(dir)

	_, err := execToolErr(tool, map[string]any{"path": "nonexistent.ts"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// --- write_file ---

func TestWriteFile(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newWriteFile(dir)

	result := execTool(t, tool, map[string]any{
		"path":    "src/app/new.component.ts",
		"content": "export class NewComponent {}",
	})

	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result["status"])
	}

	// Verify file was created
	data, err := os.ReadFile(filepath.Join(dir, "src/app/new.component.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "export class NewComponent {}" {
		t.Errorf("unexpected content: %s", string(data))
	}
}

func TestWriteFileCreatesParentDirs(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newWriteFile(dir)

	execTool(t, tool, map[string]any{
		"path":    "src/app/deep/nested/dir/file.ts",
		"content": "hello",
	})

	if _, err := os.Stat(filepath.Join(dir, "src/app/deep/nested/dir/file.ts")); os.IsNotExist(err) {
		t.Fatal("file should have been created with parent dirs")
	}
}

func TestWriteFilePathTraversal(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newWriteFile(dir)

	_, err := execToolErr(tool, map[string]any{
		"path":    "../../evil.ts",
		"content": "evil",
	})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

// --- edit_file (search/replace) ---

func TestEditFileSearchReplace(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newEditFile(dir)

	result := execTool(t, tool, map[string]any{
		"path":    "src/app/auth/login.component.ts",
		"search":  "private fb = inject(FormBuilder);",
		"replace": "private readonly fb = inject(FormBuilder);",
	})

	if result["mode"] != "search_replace" {
		t.Errorf("expected mode search_replace, got %v", result["mode"])
	}

	// Verify the file was changed
	data, _ := os.ReadFile(filepath.Join(dir, "src/app/auth/login.component.ts"))
	if !strings.Contains(string(data), "private readonly fb") {
		t.Error("edit not applied")
	}
	if strings.Contains(string(data), "private fb = inject") {
		t.Error("old text still present")
	}
}

func TestEditFileSearchNotFound(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newEditFile(dir)

	_, err := execToolErr(tool, map[string]any{
		"path":    "src/app/app.ts",
		"search":  "this text does not exist anywhere",
		"replace": "whatever",
	})
	if err == nil {
		t.Fatal("expected error for text not found")
	}
}

func TestEditFileSearchMultipleMatches(t *testing.T) {
	dir := setupWorkDir(t)

	// Create a file with duplicate text
	os.WriteFile(filepath.Join(dir, "src/app/dupe.ts"), []byte("foo\nbar\nfoo\n"), 0o644)

	tool, _ := newEditFile(dir)
	_, err := execToolErr(tool, map[string]any{
		"path":    "src/app/dupe.ts",
		"search":  "foo",
		"replace": "baz",
	})
	if err == nil {
		t.Fatal("expected error for multiple matches")
	}
	if !strings.Contains(err.Error(), "2 matches") {
		t.Errorf("expected '2 matches' in error, got: %v", err)
	}
}

// --- edit_file (line range) ---

func TestEditFileLineRange(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newEditFile(dir)

	result := execTool(t, tool, map[string]any{
		"path":       "src/app/app.ts",
		"start_line": float64(5), // JSON numbers come as float64
		"end_line":   float64(5),
		"content":    "  public title = signal('Updated App');",
	})

	if result["mode"] != "line_range" {
		t.Errorf("expected mode line_range, got %v", result["mode"])
	}

	data, _ := os.ReadFile(filepath.Join(dir, "src/app/app.ts"))
	if !strings.Contains(string(data), "Updated App") {
		t.Error("line edit not applied")
	}
}

func TestEditFileLineRangeInvalid(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newEditFile(dir)

	_, err := execToolErr(tool, map[string]any{
		"path":       "src/app/app.ts",
		"start_line": float64(100),
		"end_line":   float64(200),
		"content":    "nope",
	})
	if err == nil {
		t.Fatal("expected error for invalid line range")
	}
}

func TestEditFileNoMode(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newEditFile(dir)

	_, err := execToolErr(tool, map[string]any{
		"path": "src/app/app.ts",
	})
	if err == nil {
		t.Fatal("expected error when no mode specified")
	}
}

// --- list_dir ---

func TestListDir(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newListDir(dir)

	result := execTool(t, tool, map[string]any{"path": "."})

	entries, ok := result["entries"].([]string)
	if !ok {
		// JSON round-trip may produce []interface{}
		raw, _ := result["entries"].([]interface{})
		for _, e := range raw {
			entries = append(entries, e.(string))
		}
	}

	// Should contain src/ but NOT node_modules/ or .git/
	hasSrc := false
	for _, e := range entries {
		if e == "src/" {
			hasSrc = true
		}
		if e == "node_modules/" || e == ".git/" {
			t.Errorf("should filter out %s", e)
		}
	}
	if !hasSrc {
		t.Error("expected src/ in entries")
	}
}

// --- search_files ---

func TestSearchFiles(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newSearchFiles(dir)

	result := execTool(t, tool, map[string]any{
		"pattern": "AppComponent",
	})

	matches := getStringSlice(result, "matches")
	if len(matches) == 0 {
		t.Fatal("expected at least 1 match for AppComponent")
	}
}

func TestSearchFilesNoMatch(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newSearchFiles(dir)

	result := execTool(t, tool, map[string]any{
		"pattern": "xyzNonExistentPattern123",
	})

	matches := getStringSlice(result, "matches")
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

// --- run_command ---

func TestRunCommand(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newRunCommand(dir)

	result := execTool(t, tool, map[string]any{
		"command": "echo hello",
	})

	stdout, _ := result["stdout"].(string)
	if !strings.Contains(stdout, "hello") {
		t.Errorf("expected 'hello' in stdout, got: %s", stdout)
	}
}

func TestRunCommandFailure(t *testing.T) {
	dir := setupWorkDir(t)
	tool, _ := newRunCommand(dir)

	result := execTool(t, tool, map[string]any{
		"command": "exit 42",
	})

	errMsg, _ := result["error"].(string)
	if errMsg == "" {
		t.Error("expected error for failing command")
	}
}

// --- helpers ---

type runnableTool interface {
	Execute(ctx context.Context, args map[string]any) (map[string]any, error)
}

func execTool(t *testing.T, tl interface{}, args map[string]any) map[string]any {
	t.Helper()
	rt := tl.(runnableTool)
	result, err := rt.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("tool execution failed: %v", err)
	}
	return result
}

func execToolErr(tl interface{}, args map[string]any) (map[string]any, error) {
	rt := tl.(runnableTool)
	return rt.Execute(context.Background(), args)
}

func getStringSlice(m map[string]any, key string) []string {
	if s, ok := m[key].([]string); ok {
		return s
	}
	if raw, ok := m[key].([]interface{}); ok {
		var result []string
		for _, e := range raw {
			result = append(result, e.(string))
		}
		return result
	}
	return nil
}

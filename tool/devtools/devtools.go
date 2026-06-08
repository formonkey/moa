// Package devtools provides a built-in developer toolset for MOA agents.
//
// It includes standard file operations (read, write, edit, list, search)
// and shell command execution. All file paths are relative to a configurable
// work directory and sandboxed to prevent path traversal.
//
// Usage:
//
//	devtools.Register("/path/to/project")
//	// Tools are now available in swarm.yaml: read_file, write_file, edit_file, list_dir, search_files, run_command
package devtools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/formonkey/moa/configurable"
	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/functiontool"
)

// ignoredDirs are directories filtered out from list_dir results.
var ignoredDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	".angular":     true,
	"dist":         true,
	".next":        true,
	"__pycache__":  true,
	".venv":        true,
}

// Register creates all developer tools bound to the given workDir and
// registers them in the configurable registry for swarm.yaml resolution.
func Register(workDir string) error {
	workDir, _ = filepath.Abs(workDir)

	tools, err := NewToolset(workDir)
	if err != nil {
		return fmt.Errorf("devtools: %w", err)
	}

	for _, t := range tools {
		toolRef := t
		_ = configurable.RegisterToolFactory(t.Name(), func(ctx context.Context, args map[string]any) (tool.Tool, error) {
			return toolRef, nil
		})
	}
	return nil
}

// NewToolset creates all developer tools for the given work directory.
func NewToolset(workDir string) ([]tool.Tool, error) {
	workDir, _ = filepath.Abs(workDir)

	readFile, err := newReadFile(workDir)
	if err != nil {
		return nil, err
	}
	writeFile, err := newWriteFile(workDir)
	if err != nil {
		return nil, err
	}
	editFile, err := newEditFile(workDir)
	if err != nil {
		return nil, err
	}
	listDir, err := newListDir(workDir)
	if err != nil {
		return nil, err
	}
	searchFiles, err := newSearchFiles(workDir)
	if err != nil {
		return nil, err
	}
	runCmd, err := newRunCommand(workDir)
	if err != nil {
		return nil, err
	}

	return []tool.Tool{readFile, writeFile, editFile, listDir, searchFiles, runCmd}, nil
}

// safePath resolves a relative path within workDir and ensures it doesn't escape.
func safePath(workDir, relPath string) (string, error) {
	abs := filepath.Join(workDir, relPath)
	resolved, err := filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if !strings.HasPrefix(resolved, workDir) {
		return "", fmt.Errorf("path escapes project: %s", relPath)
	}
	return resolved, nil
}

// --- read_file ---

type readFileArgs struct {
	Path string `json:"path" jsonschema:"description=Relative file path (e.g. src/app/app.ts),required"`
}
type readFileResult struct {
	Content string `json:"content"`
	Path    string `json:"path"`
	Lines   int    `json:"lines"`
}

func newReadFile(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "read_file",
		Description: "Read the contents of a file from the project.",
	}, func(ctx context.Context, args readFileArgs) (readFileResult, error) {
		abs, err := safePath(workDir, args.Path)
		if err != nil {
			return readFileResult{}, err
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return readFileResult{}, fmt.Errorf("cannot read %s: %w", args.Path, err)
		}
		lines := strings.Count(string(data), "\n") + 1
		return readFileResult{Content: string(data), Path: args.Path, Lines: lines}, nil
	})
}

// --- write_file ---

type writeFileArgs struct {
	Path    string `json:"path" jsonschema:"description=Relative file path,required"`
	Content string `json:"content" jsonschema:"description=Complete file content to write,required"`
}
type writeFileResult struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
}

func newWriteFile(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "write_file",
		Description: "Write content to a file. Creates parent directories if needed.",
	}, func(ctx context.Context, args writeFileArgs) (writeFileResult, error) {
		abs, err := safePath(workDir, args.Path)
		if err != nil {
			return writeFileResult{}, err
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return writeFileResult{}, fmt.Errorf("mkdir: %w", err)
		}
		if err := os.WriteFile(abs, []byte(args.Content), 0o644); err != nil {
			return writeFileResult{}, fmt.Errorf("write: %w", err)
		}
		return writeFileResult{Status: "ok", Path: args.Path, Bytes: len(args.Content)}, nil
	})
}

// --- edit_file ---

type editFileArgs struct {
	Path      string `json:"path" jsonschema:"description=Relative file path to edit,required"`
	Search    string `json:"search" jsonschema:"description=Exact text to find and replace. Use this OR start_line/end_line"`
	Replace   string `json:"replace" jsonschema:"description=Replacement text"`
	StartLine int    `json:"start_line" jsonschema:"description=Start line number (1-indexed). Use with end_line and content for line-based editing"`
	EndLine   int    `json:"end_line" jsonschema:"description=End line number (1-indexed inclusive). Use with start_line and content"`
	Content   string `json:"content" jsonschema:"description=New content to replace the line range (used with start_line/end_line)"`
	Preview   bool   `json:"preview" jsonschema:"description=Dry-run mode: show what would change without modifying the file"`
}
type editFileResult struct {
	Status  string `json:"status"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Mode    string `json:"mode"` // "search_replace" or "line_range"
	Preview string `json:"preview,omitempty"` // diff preview when dry-run
}

func newEditFile(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "edit_file",
		Description: `Edit a file with surgical precision. Two modes:
1) Search/Replace: provide "search" (exact text to find) and "replace" (new text). Fails if 0 or >1 matches.
2) Line range: provide "start_line", "end_line" (1-indexed, inclusive) and "content" (replacement text).
Set "preview": true for dry-run (see the diff without applying changes).
Always read_file first to see current content before editing.`,
	}, func(ctx context.Context, args editFileArgs) (editFileResult, error) {
		abs, err := safePath(workDir, args.Path)
		if err != nil {
			return editFileResult{}, err
		}

		data, err := os.ReadFile(abs)
		if err != nil {
			return editFileResult{}, fmt.Errorf("cannot read %s: %w", args.Path, err)
		}
		content := string(data)

		// Determine mode
		if args.Search != "" {
			return editBySearch(abs, args.Path, content, args.Search, args.Replace, args.Preview)
		}
		if args.StartLine > 0 && args.EndLine > 0 {
			return editByLines(abs, args.Path, content, args.StartLine, args.EndLine, args.Content, args.Preview)
		}

		return editFileResult{}, fmt.Errorf("provide either 'search'+'replace' or 'start_line'+'end_line'+'content'")
	})
}

func editBySearch(absPath, relPath, content, search, replace string, preview bool) (editFileResult, error) {
	count := strings.Count(content, search)
	if count == 0 {
		return editFileResult{}, fmt.Errorf("text not found in %s", relPath)
	}
	if count > 1 {
		return editFileResult{}, fmt.Errorf("found %d matches in %s — be more specific", count, relPath)
	}

	// Find the line number of the match
	idx := strings.Index(content, search)
	line := strings.Count(content[:idx], "\n") + 1

	if preview {
		diff := generateDiff(search, replace, line)
		return editFileResult{Status: "preview", Path: relPath, Line: line, Mode: "search_replace", Preview: diff}, nil
	}

	newContent := strings.Replace(content, search, replace, 1)
	if err := os.WriteFile(absPath, []byte(newContent), 0o644); err != nil {
		return editFileResult{}, fmt.Errorf("write: %w", err)
	}
	return editFileResult{Status: "ok", Path: relPath, Line: line, Mode: "search_replace"}, nil
}

func editByLines(absPath, relPath, content string, startLine, endLine int, newContent string, preview bool) (editFileResult, error) {
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	if startLine < 1 || endLine < startLine || startLine > totalLines {
		return editFileResult{}, fmt.Errorf("invalid line range %d-%d (file has %d lines)", startLine, endLine, totalLines)
	}
	if endLine > totalLines {
		endLine = totalLines
	}

	// Build the old content for diff
	oldLines := strings.Join(lines[startLine-1:endLine], "\n")

	if preview {
		diff := generateDiff(oldLines, newContent, startLine)
		return editFileResult{Status: "preview", Path: relPath, Line: startLine, Mode: "line_range", Preview: diff}, nil
	}

	// Replace lines [startLine-1, endLine) with newContent
	var result strings.Builder
	for i := 0; i < startLine-1; i++ {
		result.WriteString(lines[i] + "\n")
	}
	result.WriteString(newContent)
	if !strings.HasSuffix(newContent, "\n") {
		result.WriteString("\n")
	}
	for i := endLine; i < totalLines; i++ {
		result.WriteString(lines[i])
		if i < totalLines-1 {
			result.WriteString("\n")
		}
	}

	if err := os.WriteFile(absPath, []byte(result.String()), 0o644); err != nil {
		return editFileResult{}, fmt.Errorf("write: %w", err)
	}
	return editFileResult{Status: "ok", Path: relPath, Line: startLine, Mode: "line_range"}, nil
}

// generateDiff creates a simple unified-diff-style preview.
func generateDiff(old, new string, startLine int) string {
	var b strings.Builder
	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(new, "\n")

	b.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", startLine, len(oldLines), startLine, len(newLines)))
	for _, l := range oldLines {
		b.WriteString("- " + l + "\n")
	}
	for _, l := range newLines {
		b.WriteString("+ " + l + "\n")
	}
	return b.String()
}

// --- list_dir ---

type listDirArgs struct {
	Path string `json:"path" jsonschema:"description=Relative directory path or . for root,required"`
}
type listDirResult struct {
	Entries []string `json:"entries"`
	Path    string   `json:"path"`
}

func newListDir(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "list_dir",
		Description: "List files and directories. Use '.' for project root.",
	}, func(ctx context.Context, args listDirArgs) (listDirResult, error) {
		abs, err := safePath(workDir, args.Path)
		if err != nil {
			return listDirResult{}, err
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return listDirResult{}, fmt.Errorf("cannot list %s: %w", args.Path, err)
		}
		var items []string
		for _, e := range entries {
			name := e.Name()
			if ignoredDirs[name] {
				continue
			}
			if e.IsDir() {
				name += "/"
			}
			items = append(items, name)
		}
		return listDirResult{Entries: items, Path: args.Path}, nil
	})
}

// --- search_files ---

type searchFilesArgs struct {
	Pattern string `json:"pattern" jsonschema:"description=Text pattern to search for,required"`
	Path    string `json:"path" jsonschema:"description=Directory to search in (defaults to project root)"`
	Include string `json:"include" jsonschema:"description=File glob to include (e.g. *.ts). Defaults to common source files"`
}
type searchFilesResult struct {
	Matches []string `json:"matches"`
	Pattern string   `json:"pattern"`
	Count   int      `json:"count"`
}

func newSearchFiles(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "search_files",
		Description: "Search for a text pattern across project files. Returns matching file paths and line numbers.",
	}, func(ctx context.Context, args searchFilesArgs) (searchFilesResult, error) {
		searchPath := args.Path
		if searchPath == "" {
			searchPath = "."
		}
		abs, err := safePath(workDir, searchPath)
		if err != nil {
			return searchFilesResult{}, err
		}

		grepArgs := []string{"-rn", "--color=never"}

		if args.Include != "" {
			grepArgs = append(grepArgs, "--include="+args.Include)
		} else {
			// Default: common source files
			for _, ext := range []string{"*.ts", "*.html", "*.css", "*.scss", "*.json", "*.go", "*.py", "*.js", "*.jsx", "*.tsx", "*.vue", "*.svelte", "*.yaml", "*.yml"} {
				grepArgs = append(grepArgs, "--include="+ext)
			}
		}

		grepArgs = append(grepArgs, args.Pattern, abs)
		cmd := exec.CommandContext(ctx, "grep", grepArgs...)
		out, err := cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				return searchFilesResult{Matches: []string{}, Pattern: args.Pattern, Count: 0}, nil
			}
			return searchFilesResult{}, fmt.Errorf("search failed: %w", err)
		}

		rawLines := strings.Split(strings.TrimSpace(string(out)), "\n")
		var rel []string
		for _, l := range rawLines {
			if r, err := filepath.Rel(workDir, l); err == nil {
				rel = append(rel, r)
			} else {
				rel = append(rel, l)
			}
		}

		// Cap results
		if len(rel) > 50 {
			rel = rel[:50]
		}

		return searchFilesResult{Matches: rel, Pattern: args.Pattern, Count: len(rel)}, nil
	})
}

// --- run_command ---

type runCommandArgs struct {
	Command string `json:"command" jsonschema:"description=Shell command to execute in the project directory,required"`
}
type runCommandResult struct {
	Stdout   string `json:"stdout"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

func newRunCommand(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "run_command",
		Description: "Run a shell command in the project directory.",
	}, func(ctx context.Context, args runCommandArgs) (runCommandResult, error) {
		cmd := exec.CommandContext(ctx, "bash", "-c", args.Command)
		cmd.Dir = workDir
		out, err := cmd.CombinedOutput()
		output := string(out)
		if len(output) > 4000 {
			output = output[:2000] + "\n...[truncated]...\n" + output[len(output)-2000:]
		}
		if err != nil {
			return runCommandResult{Stdout: output, Error: err.Error()}, nil
		}
		return runCommandResult{Stdout: output, ExitCode: 0}, nil
	})
}

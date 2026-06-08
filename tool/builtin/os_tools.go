package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/formonkey/moa/tool"
	"google.golang.org/genai"
)

// ExecuteCommandTool runs shell commands. Integrates with HITL for safety.
type ExecuteCommandTool struct {
	WorkspaceDir         string
	RequireConfirmation  bool
}

func (t *ExecuteCommandTool) Name() string { return "execute_command" }
func (t *ExecuteCommandTool) Description() string {
	return "Executes a shell command in the workspace directory. Use this to run npm/npx, go build, scaffolding, or tests."
}
func (t *ExecuteCommandTool) IsNative() bool      { return false }
func (t *ExecuteCommandTool) IsLongRunning() bool { return false }

func (t *ExecuteCommandTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"command": {
					Type:        genai.TypeString,
					Description: "The exact shell command to run",
				},
			},
			Required: []string{"command"},
		},
	}
}

func (t *ExecuteCommandTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	cmdStr, ok := args["command"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'command' argument")
	}

	// HITL check
	if t.RequireConfirmation {
		if cCtx, ok := ctx.(tool.ConfirmationContext); ok {
			status := cCtx.GetConfirmation()
			if status == nil || (!status.Confirmed && !status.Rejected) {
				cCtx.RequestConfirmation("The agent wants to execute a shell command.", cmdStr)
				return nil, tool.ErrConfirmationRequired
			}
			if status.Rejected {
				return nil, tool.ErrConfirmationRejected
			}
		}
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = t.WorkspaceDir
	out, err := cmd.CombinedOutput()
	
	result := map[string]any{"output": string(out)}
	if err != nil {
		result["error"] = err.Error()
	}
	
	return result, nil
}

// WriteFileTool creates or modifies files.
type WriteFileTool struct {
	WorkspaceDir string
}

func (t *WriteFileTool) Name() string { return "write_file" }
func (t *WriteFileTool) Description() string {
	return "Writes code to a file in the workspace. Automatically creates necessary directories."
}
func (t *WriteFileTool) IsNative() bool      { return false }
func (t *WriteFileTool) IsLongRunning() bool { return false }

func (t *WriteFileTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "Relative file path (e.g. src/app/main.go)",
				},
				"content": {
					Type:        genai.TypeString,
					Description: "The full code content to write",
				},
			},
			Required: []string{"path", "content"},
		},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	path, ok1 := args["path"].(string)
	content, ok2 := args["content"].(string)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("missing 'path' or 'content'")
	}

	cleanPath := filepath.Clean(path)
	if strings.HasPrefix(cleanPath, "..") || strings.HasPrefix(cleanPath, "/") || strings.HasPrefix(cleanPath, "\\") {
		return nil, fmt.Errorf("blocked path traversal: %s", cleanPath)
	}

	fullPath := filepath.Join(t.WorkspaceDir, cleanPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return nil, fmt.Errorf("error creating directories: %v", err)
	}

	if err := os.WriteFile(fullPath, []byte(strings.TrimSpace(content)+"\n"), 0644); err != nil {
		return nil, fmt.Errorf("error writing file: %v", err)
	}

	return map[string]any{"status": "Successfully wrote file: " + cleanPath}, nil
}

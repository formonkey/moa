// Package gittools provides Git operation tools for MOA agents.
//
// Includes status, diff, commit, branch management, stash, log, and worktree
// operations. All operations are sandboxed to a configured work directory.
//
// Usage:
//
//	gittools.Register(workDir)
//	// Tools available: git_status, git_diff, git_commit, git_branch, git_stash, git_log, git_worktree
package gittools

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/formonkey/moa/configurable"
	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/functiontool"
)

// Register creates all git tools and registers them in the configurable registry.
func Register(workDir string) error {
	workDir, _ = filepath.Abs(workDir)
	tools, err := NewToolset(workDir)
	if err != nil {
		return fmt.Errorf("gittools: %w", err)
	}
	for _, t := range tools {
		toolRef := t
		_ = configurable.RegisterToolFactory(t.Name(), func(ctx context.Context, args map[string]any) (tool.Tool, error) {
			return toolRef, nil
		})
	}
	return nil
}

// NewToolset creates all git tools.
func NewToolset(workDir string) ([]tool.Tool, error) {
	workDir, _ = filepath.Abs(workDir)
	builders := []func(string) (tool.Tool, error){
		newGitStatus, newGitDiff, newGitCommit, newGitBranch,
		newGitStash, newGitLog, newGitWorktree,
	}
	var tools []tool.Tool
	for _, b := range builders {
		t, err := b(workDir)
		if err != nil {
			return nil, err
		}
		tools = append(tools, t)
	}
	return tools, nil
}

// runGit executes a git command in the work directory.
func runGit(ctx context.Context, workDir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if len(output) > 4000 {
		output = output[:2000] + "\n...[truncated]...\n" + output[len(output)-2000:]
	}
	if err != nil {
		return output, fmt.Errorf("git %s: %s", strings.Join(args, " "), output)
	}
	return output, nil
}

// --- git_status ---

type gitStatusArgs struct {
	Short bool `json:"short" jsonschema:"description=Use short format (default true)"`
}
type gitStatusResult struct {
	Status  string `json:"status"`
	Branch  string `json:"branch"`
	Clean   bool   `json:"clean"`
}

func newGitStatus(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "git_status",
		Description: "Show the working tree status. Returns modified, staged, and untracked files.",
	}, func(ctx context.Context, args gitStatusArgs) (gitStatusResult, error) {
		status, err := runGit(ctx, workDir, "status", "--short")
		if err != nil {
			return gitStatusResult{}, err
		}
		branch, _ := runGit(ctx, workDir, "branch", "--show-current")
		return gitStatusResult{
			Status: status,
			Branch: branch,
			Clean:  status == "",
		}, nil
	})
}

// --- git_diff ---

type gitDiffArgs struct {
	Staged bool   `json:"staged" jsonschema:"description=Show staged changes (git diff --cached)"`
	Path   string `json:"path" jsonschema:"description=Optional file path to diff"`
}
type gitDiffResult struct {
	Diff  string `json:"diff"`
	Empty bool   `json:"empty"`
}

func newGitDiff(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "git_diff",
		Description: "Show changes in the working tree or staged area.",
	}, func(ctx context.Context, args gitDiffArgs) (gitDiffResult, error) {
		gitArgs := []string{"diff", "--stat"}
		if args.Staged {
			gitArgs = append(gitArgs, "--cached")
		}
		if args.Path != "" {
			gitArgs = append(gitArgs, "--", args.Path)
		}
		stat, err := runGit(ctx, workDir, gitArgs...)
		if err != nil {
			return gitDiffResult{}, err
		}

		// Also get the actual diff (limited)
		diffArgs := []string{"diff"}
		if args.Staged {
			diffArgs = append(diffArgs, "--cached")
		}
		if args.Path != "" {
			diffArgs = append(diffArgs, "--", args.Path)
		}
		diff, _ := runGit(ctx, workDir, diffArgs...)

		fullDiff := stat
		if diff != "" {
			fullDiff += "\n\n" + diff
		}

		return gitDiffResult{
			Diff:  fullDiff,
			Empty: stat == "",
		}, nil
	})
}

// --- git_commit ---

type gitCommitArgs struct {
	Message string `json:"message" jsonschema:"description=Commit message,required"`
	All     bool   `json:"all" jsonschema:"description=Stage all modified files before committing (git commit -a)"`
	Files   string `json:"files" jsonschema:"description=Space-separated files to stage before commit"`
}
type gitCommitResult struct {
	Status string `json:"status"`
	Hash   string `json:"hash"`
	Output string `json:"output"`
}

func newGitCommit(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "git_commit",
		Description: "Stage and commit changes. Use 'all' to commit all modified files, or 'files' to stage specific files first.",
	}, func(ctx context.Context, args gitCommitArgs) (gitCommitResult, error) {
		// Stage files if specified
		if args.Files != "" {
			files := strings.Fields(args.Files)
			addArgs := append([]string{"add"}, files...)
			if _, err := runGit(ctx, workDir, addArgs...); err != nil {
				return gitCommitResult{}, err
			}
		}

		// Commit
		commitArgs := []string{"commit", "-m", args.Message}
		if args.All {
			commitArgs = []string{"commit", "-a", "-m", args.Message}
		}
		output, err := runGit(ctx, workDir, commitArgs...)
		if err != nil {
			return gitCommitResult{}, err
		}

		hash, _ := runGit(ctx, workDir, "rev-parse", "--short", "HEAD")
		return gitCommitResult{
			Status: "ok",
			Hash:   hash,
			Output: output,
		}, nil
	})
}

// --- git_branch ---

type gitBranchArgs struct {
	Action string `json:"action" jsonschema:"description=Action: list (default) / create / switch / delete,required"`
	Name   string `json:"name" jsonschema:"description=Branch name (for create/switch/delete)"`
}
type gitBranchResult struct {
	Branches []string `json:"branches,omitempty"`
	Current  string   `json:"current"`
	Output   string   `json:"output"`
}

func newGitBranch(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "git_branch",
		Description: "Manage Git branches: list, create, switch, or delete.",
	}, func(ctx context.Context, args gitBranchArgs) (gitBranchResult, error) {
		action := args.Action
		if action == "" {
			action = "list"
		}

		switch action {
		case "list":
			out, err := runGit(ctx, workDir, "branch", "--list")
			if err != nil {
				return gitBranchResult{}, err
			}
			current, _ := runGit(ctx, workDir, "branch", "--show-current")
			var branches []string
			for _, line := range strings.Split(out, "\n") {
				b := strings.TrimSpace(strings.TrimPrefix(line, "*"))
				b = strings.TrimSpace(b)
				if b != "" {
					branches = append(branches, b)
				}
			}
			return gitBranchResult{Branches: branches, Current: current}, nil

		case "create":
			if args.Name == "" {
				return gitBranchResult{}, fmt.Errorf("branch name required")
			}
			out, err := runGit(ctx, workDir, "checkout", "-b", args.Name)
			if err != nil {
				return gitBranchResult{}, err
			}
			return gitBranchResult{Current: args.Name, Output: out}, nil

		case "switch":
			if args.Name == "" {
				return gitBranchResult{}, fmt.Errorf("branch name required")
			}
			out, err := runGit(ctx, workDir, "checkout", args.Name)
			if err != nil {
				return gitBranchResult{}, err
			}
			return gitBranchResult{Current: args.Name, Output: out}, nil

		case "delete":
			if args.Name == "" {
				return gitBranchResult{}, fmt.Errorf("branch name required")
			}
			out, err := runGit(ctx, workDir, "branch", "-d", args.Name)
			if err != nil {
				return gitBranchResult{}, err
			}
			return gitBranchResult{Output: out}, nil

		default:
			return gitBranchResult{}, fmt.Errorf("unknown action %q (use: list, create, switch, delete)", action)
		}
	})
}

// --- git_stash ---

type gitStashArgs struct {
	Action  string `json:"action" jsonschema:"description=Action: save (default) / pop / list / drop,required"`
	Message string `json:"message" jsonschema:"description=Stash message (for save)"`
}
type gitStashResult struct {
	Status string `json:"status"`
	Output string `json:"output"`
}

func newGitStash(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "git_stash",
		Description: "Manage Git stash: save current changes, pop, list, or drop stashes. Useful for rollback.",
	}, func(ctx context.Context, args gitStashArgs) (gitStashResult, error) {
		action := args.Action
		if action == "" {
			action = "save"
		}

		switch action {
		case "save":
			stashArgs := []string{"stash", "push"}
			if args.Message != "" {
				stashArgs = append(stashArgs, "-m", args.Message)
			}
			out, err := runGit(ctx, workDir, stashArgs...)
			if err != nil {
				return gitStashResult{}, err
			}
			return gitStashResult{Status: "saved", Output: out}, nil

		case "pop":
			out, err := runGit(ctx, workDir, "stash", "pop")
			if err != nil {
				return gitStashResult{}, err
			}
			return gitStashResult{Status: "popped", Output: out}, nil

		case "list":
			out, err := runGit(ctx, workDir, "stash", "list")
			if err != nil {
				return gitStashResult{}, err
			}
			return gitStashResult{Status: "ok", Output: out}, nil

		case "drop":
			out, err := runGit(ctx, workDir, "stash", "drop")
			if err != nil {
				return gitStashResult{}, err
			}
			return gitStashResult{Status: "dropped", Output: out}, nil

		default:
			return gitStashResult{}, fmt.Errorf("unknown action %q (use: save, pop, list, drop)", action)
		}
	})
}

// --- git_log ---

type gitLogArgs struct {
	Count int    `json:"count" jsonschema:"description=Number of commits to show (default 5)"`
	Path  string `json:"path" jsonschema:"description=Optional file path to filter log"`
}
type gitLogResult struct {
	Log string `json:"log"`
}

func newGitLog(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "git_log",
		Description: "Show recent commit history.",
	}, func(ctx context.Context, args gitLogArgs) (gitLogResult, error) {
		count := args.Count
		if count <= 0 {
			count = 5
		}
		if count > 20 {
			count = 20
		}
		logArgs := []string{"log", fmt.Sprintf("-n%d", count), "--oneline", "--decorate"}
		if args.Path != "" {
			logArgs = append(logArgs, "--", args.Path)
		}
		out, err := runGit(ctx, workDir, logArgs...)
		if err != nil {
			return gitLogResult{}, err
		}
		return gitLogResult{Log: out}, nil
	})
}

// --- git_worktree ---

type gitWorktreeArgs struct {
	Action string `json:"action" jsonschema:"description=Action: list / add / remove,required"`
	Path   string `json:"path" jsonschema:"description=Path for the worktree (for add/remove)"`
	Branch string `json:"branch" jsonschema:"description=Branch for the worktree (for add)"`
}
type gitWorktreeResult struct {
	Status string `json:"status"`
	Output string `json:"output"`
}

func newGitWorktree(workDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "git_worktree",
		Description: "Manage Git worktrees: work on multiple branches simultaneously in separate directories.",
	}, func(ctx context.Context, args gitWorktreeArgs) (gitWorktreeResult, error) {
		switch args.Action {
		case "list":
			out, err := runGit(ctx, workDir, "worktree", "list")
			if err != nil {
				return gitWorktreeResult{}, err
			}
			return gitWorktreeResult{Status: "ok", Output: out}, nil

		case "add":
			if args.Path == "" {
				return gitWorktreeResult{}, fmt.Errorf("path required for worktree add")
			}
			wtArgs := []string{"worktree", "add", args.Path}
			if args.Branch != "" {
				wtArgs = append(wtArgs, "-b", args.Branch)
			}
			out, err := runGit(ctx, workDir, wtArgs...)
			if err != nil {
				return gitWorktreeResult{}, err
			}
			return gitWorktreeResult{Status: "added", Output: out}, nil

		case "remove":
			if args.Path == "" {
				return gitWorktreeResult{}, fmt.Errorf("path required for worktree remove")
			}
			out, err := runGit(ctx, workDir, "worktree", "remove", args.Path)
			if err != nil {
				return gitWorktreeResult{}, err
			}
			return gitWorktreeResult{Status: "removed", Output: out}, nil

		default:
			return gitWorktreeResult{}, fmt.Errorf("unknown action %q (use: list, add, remove)", args.Action)
		}
	})
}

package taskboard

import (
	"context"
	"fmt"
	"strings"

	"github.com/formonkey/moa/configurable"
	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/functiontool"
)

// Register creates all taskboard tools and registers them.
func Register(board *Board) error {
	tools, err := NewToolset(board)
	if err != nil {
		return fmt.Errorf("taskboard: %w", err)
	}
	for _, t := range tools {
		toolRef := t
		_ = configurable.RegisterToolFactory(t.Name(), func(ctx context.Context, args map[string]any) (tool.Tool, error) {
			return toolRef, nil
		})
	}
	return nil
}

// NewToolset creates all taskboard tools.
func NewToolset(board *Board) ([]tool.Tool, error) {
	addSub, err := newAddSubtask(board)
	if err != nil {
		return nil, err
	}
	listSubs, err := newListSubtasks(board)
	if err != nil {
		return nil, err
	}
	nextSub, err := newNextSubtask(board)
	if err != nil {
		return nil, err
	}
	completeSub, err := newCompleteSubtask(board)
	if err != nil {
		return nil, err
	}
	status, err := newTaskStatus(board)
	if err != nil {
		return nil, err
	}

	return []tool.Tool{addSub, listSubs, nextSub, completeSub, status}, nil
}

// --- add_subtask ---

type addSubtaskArgs struct {
	Description string `json:"description" jsonschema:"description=Description of the atomic subtask to create,required"`
}
type addSubtaskResult struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

func newAddSubtask(board *Board) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "add_subtask",
		Description: "Add a subtask to the current task. Each subtask should be an atomic, minimal unit of work (e.g., 'create file X', 'edit file Y to add Z').",
	}, func(ctx context.Context, args addSubtaskArgs) (addSubtaskResult, error) {
		sub, err := board.AddSubtask(args.Description)
		if err != nil {
			return addSubtaskResult{}, err
		}
		return addSubtaskResult{
			ID:          sub.ID,
			Description: sub.Description,
			Status:      string(sub.Status),
		}, nil
	})
}

// --- list_subtasks ---

type listSubtasksArgs struct{}
type listSubtasksResult struct {
	TaskDescription string   `json:"task_description"`
	TaskStatus      string   `json:"task_status"`
	Subtasks        []string `json:"subtasks"`
	Summary         string   `json:"summary"`
}

func newListSubtasks(board *Board) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "list_subtasks",
		Description: "List all subtasks of the current task with their status.",
	}, func(ctx context.Context, args listSubtasksArgs) (listSubtasksResult, error) {
		task := board.GetTask()
		if task == nil {
			return listSubtasksResult{}, fmt.Errorf("no active task")
		}

		var subs []string
		pending, inProgress, done := 0, 0, 0
		for _, s := range task.Subtasks {
			icon := "⬜"
			switch s.Status {
			case StatusInProgress:
				icon = "🔄"
				inProgress++
			case StatusDone:
				icon = "✅"
				done++
			default:
				pending++
			}
			line := fmt.Sprintf("%s [%s] %s: %s", icon, s.ID, s.Status, s.Description)
			if s.Result != "" {
				line += fmt.Sprintf(" → %s", s.Result)
			}
			subs = append(subs, line)
		}

		return listSubtasksResult{
			TaskDescription: task.Description,
			TaskStatus:      string(task.Status),
			Subtasks:        subs,
			Summary:         fmt.Sprintf("%d pending, %d in progress, %d done, %d total", pending, inProgress, done, len(task.Subtasks)),
		}, nil
	})
}

// --- next_subtask ---

type nextSubtaskArgs struct{}
type nextSubtaskResult struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	HasMore     bool   `json:"has_more"`
	Message     string `json:"message"`
}

func newNextSubtask(board *Board) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "next_subtask",
		Description: "Get the next pending subtask and mark it as in_progress. Returns empty if all subtasks are done.",
	}, func(ctx context.Context, args nextSubtaskArgs) (nextSubtaskResult, error) {
		sub, err := board.NextSubtask()
		if err != nil {
			return nextSubtaskResult{}, err
		}
		if sub == nil {
			return nextSubtaskResult{
				Message: "All subtasks completed!",
				HasMore: false,
			}, nil
		}

		// Check if there are more pending after this one
		pending, _, _, _ := board.TaskStatus()
		return nextSubtaskResult{
			ID:          sub.ID,
			Description: sub.Description,
			HasMore:     pending > 0, // pending was counted before we took this one
			Message:     fmt.Sprintf("Working on: %s", sub.Description),
		}, nil
	})
}

// --- complete_subtask ---

type completeSubtaskArgs struct {
	ID     string `json:"id" jsonschema:"description=ID of the subtask to complete (e.g. sub-1),required"`
	Result string `json:"result" jsonschema:"description=Brief description of what was done,required"`
}
type completeSubtaskResult struct {
	Status       string `json:"status"`
	TaskComplete bool   `json:"task_complete"`
	Remaining    int    `json:"remaining"`
}

func newCompleteSubtask(board *Board) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "complete_subtask",
		Description: "Mark a subtask as done. If all subtasks are complete, the task is automatically completed and an event is emitted.",
	}, func(ctx context.Context, args completeSubtaskArgs) (completeSubtaskResult, error) {
		if err := board.CompleteSubtask(args.ID, args.Result); err != nil {
			return completeSubtaskResult{}, err
		}

		pending, inProgress, _, _ := board.TaskStatus()
		task := board.GetTask()
		taskDone := task != nil && task.Status == StatusDone

		return completeSubtaskResult{
			Status:       "ok",
			TaskComplete: taskDone,
			Remaining:    pending + inProgress,
		}, nil
	})
}

// --- task_status ---

type taskStatusArgs struct{}
type taskStatusResult struct {
	TaskDescription string `json:"task_description"`
	TaskStatus      string `json:"task_status"`
	Pending         int    `json:"pending"`
	InProgress      int    `json:"in_progress"`
	Done            int    `json:"done"`
	Total           int    `json:"total"`
	Progress        string `json:"progress"`
}

func newTaskStatus(board *Board) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "task_status",
		Description: "Get the current task progress: how many subtasks are pending, in progress, and done.",
	}, func(ctx context.Context, args taskStatusArgs) (taskStatusResult, error) {
		task := board.GetTask()
		if task == nil {
			return taskStatusResult{}, fmt.Errorf("no active task")
		}

		pending, inProgress, done, total := board.TaskStatus()
		var bar strings.Builder
		for i := 0; i < done; i++ {
			bar.WriteString("█")
		}
		for i := 0; i < inProgress; i++ {
			bar.WriteString("▓")
		}
		for i := 0; i < pending; i++ {
			bar.WriteString("░")
		}

		pct := 0
		if total > 0 {
			pct = done * 100 / total
		}

		return taskStatusResult{
			TaskDescription: task.Description,
			TaskStatus:      string(task.Status),
			Pending:         pending,
			InProgress:       inProgress,
			Done:            done,
			Total:           total,
			Progress:        fmt.Sprintf("[%s] %d%%", bar.String(), pct),
		}, nil
	})
}

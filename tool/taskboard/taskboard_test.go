package taskboard

import (
	"context"
	"testing"

	"github.com/formonkey/moa/swarm/blackboard"
	"github.com/formonkey/moa/swarm/scheduler"
)

func setup(t *testing.T) (*Board, *scheduler.EventBus) {
	t.Helper()
	bb := blackboard.New()
	bus := scheduler.NewEventBus()
	board := NewBoard(bb, bus)
	return board, bus
}

func TestCreateTask(t *testing.T) {
	board, _ := setup(t)
	task := board.CreateTask("Crear componente de login")

	if task == nil {
		t.Fatal("expected task")
	}
	if task.Status != StatusPending {
		t.Errorf("expected pending, got %s", task.Status)
	}
	if task.Description != "Crear componente de login" {
		t.Errorf("unexpected description: %s", task.Description)
	}
}

func TestAddSubtask(t *testing.T) {
	board, _ := setup(t)
	board.CreateTask("Feature login")

	sub, err := board.AddSubtask("Crear login.store.ts")
	if err != nil {
		t.Fatal(err)
	}
	if sub.Status != StatusPending {
		t.Errorf("expected pending, got %s", sub.Status)
	}

	task := board.GetTask()
	if len(task.Subtasks) != 1 {
		t.Fatalf("expected 1 subtask, got %d", len(task.Subtasks))
	}
	if task.Status != StatusInProgress {
		t.Errorf("task should be in_progress after adding subtask, got %s", task.Status)
	}
}

func TestAddSubtaskNoTask(t *testing.T) {
	board, _ := setup(t)
	_, err := board.AddSubtask("orphan")
	if err == nil {
		t.Fatal("expected error without active task")
	}
}

func TestNextSubtask(t *testing.T) {
	board, _ := setup(t)
	board.CreateTask("Feature login")
	board.AddSubtask("Crear store")
	board.AddSubtask("Crear component")

	sub, err := board.NextSubtask()
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil {
		t.Fatal("expected subtask")
	}
	if sub.Status != StatusInProgress {
		t.Errorf("expected in_progress, got %s", sub.Status)
	}
	if sub.Description != "Crear store" {
		t.Errorf("expected first subtask, got %s", sub.Description)
	}
}

func TestNextSubtaskNone(t *testing.T) {
	board, _ := setup(t)
	board.CreateTask("Empty")

	sub, err := board.NextSubtask()
	if err != nil {
		t.Fatal(err)
	}
	if sub != nil {
		t.Error("expected nil when no subtasks")
	}
}

func TestCompleteSubtask(t *testing.T) {
	board, _ := setup(t)
	board.CreateTask("Feature login")
	board.AddSubtask("Crear store")

	sub, _ := board.NextSubtask()
	err := board.CompleteSubtask(sub.ID, "Store creado")
	if err != nil {
		t.Fatal(err)
	}

	task := board.GetTask()
	if task.Subtasks[0].Status != StatusDone {
		t.Errorf("expected done, got %s", task.Subtasks[0].Status)
	}
	if task.Subtasks[0].Result != "Store creado" {
		t.Errorf("unexpected result: %s", task.Subtasks[0].Result)
	}
}

func TestCompleteSubtaskNotFound(t *testing.T) {
	board, _ := setup(t)
	board.CreateTask("Test")
	board.AddSubtask("Something")

	err := board.CompleteSubtask("nonexistent", "done")
	if err == nil {
		t.Fatal("expected error for unknown subtask")
	}
}

func TestAutoTaskCompletion(t *testing.T) {
	board, bus := setup(t)

	// Subscribe to completion events
	ch := bus.Subscribe("TASK_COMPLETED")

	board.CreateTask("Feature login")
	board.AddSubtask("Crear store")
	board.AddSubtask("Crear component")

	// Complete both subtasks
	s1, _ := board.NextSubtask()
	board.CompleteSubtask(s1.ID, "done")

	// Task should NOT be done yet
	task := board.GetTask()
	if task.Status == StatusDone {
		t.Error("task should not be done yet")
	}

	s2, _ := board.NextSubtask()
	board.CompleteSubtask(s2.ID, "done")

	// Task SHOULD be done now
	task = board.GetTask()
	if task.Status != StatusDone {
		t.Errorf("expected task done, got %s", task.Status)
	}

	// Event should have been emitted
	select {
	case evt := <-ch:
		if evt.Type != "TASK_COMPLETED" {
			t.Errorf("expected TASK_COMPLETED, got %s", evt.Type)
		}
	default:
		t.Error("expected TASK_COMPLETED event")
	}
}

func TestTaskStatus(t *testing.T) {
	board, _ := setup(t)
	board.CreateTask("Test")
	board.AddSubtask("A")
	board.AddSubtask("B")
	board.AddSubtask("C")

	pending, inProgress, done, total := board.TaskStatus()
	if pending != 3 || inProgress != 0 || done != 0 || total != 3 {
		t.Errorf("unexpected: p=%d ip=%d d=%d t=%d", pending, inProgress, done, total)
	}

	board.NextSubtask()
	pending, inProgress, done, total = board.TaskStatus()
	if pending != 2 || inProgress != 1 || done != 0 {
		t.Errorf("after next: p=%d ip=%d d=%d", pending, inProgress, done)
	}
}

// --- Tool tests ---

type runnableTool interface {
	Execute(ctx context.Context, args map[string]any) (map[string]any, error)
}

func TestToolAddSubtask(t *testing.T) {
	board, _ := setup(t)
	board.CreateTask("Test")

	tools, _ := NewToolset(board)
	// Find add_subtask
	var addTool runnableTool
	for _, tl := range tools {
		if tl.Name() == "add_subtask" {
			addTool = tl.(runnableTool)
		}
	}

	result, err := addTool.Execute(context.Background(), map[string]any{
		"description": "Crear login.store.ts",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["id"] == nil {
		t.Error("expected subtask id")
	}
}

func TestToolNextAndComplete(t *testing.T) {
	board, _ := setup(t)
	board.CreateTask("Test")
	board.AddSubtask("Task A")

	tools, _ := NewToolset(board)
	toolMap := make(map[string]runnableTool)
	for _, tl := range tools {
		toolMap[tl.Name()] = tl.(runnableTool)
	}

	// next_subtask
	result, err := toolMap["next_subtask"].Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result["id"].(string)
	if id == "" {
		t.Fatal("expected subtask id")
	}

	// complete_subtask
	result, err = toolMap["complete_subtask"].Execute(context.Background(), map[string]any{
		"id":     id,
		"result": "Done!",
	})
	if err != nil {
		t.Fatal(err)
	}

	taskComplete, _ := result["task_complete"].(bool)
	if !taskComplete {
		t.Error("expected task_complete=true")
	}
}

func TestToolTaskStatus(t *testing.T) {
	board, _ := setup(t)
	board.CreateTask("Test")
	board.AddSubtask("A")
	board.AddSubtask("B")

	tools, _ := NewToolset(board)
	var statusTool runnableTool
	for _, tl := range tools {
		if tl.Name() == "task_status" {
			statusTool = tl.(runnableTool)
		}
	}

	result, err := statusTool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	// JSON round-trip converts int to float64
	total, _ := result["total"].(float64)
	if total != 2 {
		t.Errorf("expected total=2, got %v", result["total"])
	}
}

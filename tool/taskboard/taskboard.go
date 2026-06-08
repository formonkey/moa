// Package taskboard provides task management tools backed by the Blackboard.
//
// A task represents a user request. An agent decomposes it into subtasks
// (atomic units of work). When all subtasks are completed, the task is
// automatically marked as done and an event is emitted via the EventBus.
package taskboard

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/formonkey/moa/swarm/blackboard"
	"github.com/formonkey/moa/swarm/scheduler"
)

// Status represents the state of a task or subtask.
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

// Subtask is an atomic unit of work within a task.
type Subtask struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      Status `json:"status"`
	Result      string `json:"result,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	DoneAt      int64  `json:"done_at,omitempty"`
}

// Task is the top-level work item created from a user request.
type Task struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Status      Status    `json:"status"`
	Subtasks    []Subtask `json:"subtasks"`
	CreatedAt   int64     `json:"created_at"`
	DoneAt      int64     `json:"done_at,omitempty"`
}

// Board manages tasks on a blackboard with event emission.
type Board struct {
	bb       *blackboard.Blackboard
	bus      *scheduler.EventBus
	mu       sync.Mutex
	taskKey  string // blackboard key for the current task
	seqCount int    // subtask counter
}

// NewBoard creates a task board backed by the given blackboard.
func NewBoard(bb *blackboard.Blackboard, bus *scheduler.EventBus) *Board {
	return &Board{
		bb:      bb,
		bus:     bus,
		taskKey: "taskboard:current",
	}
}

// CreateTask creates the top-level task from a user request.
func (b *Board) CreateTask(description string) *Task {
	b.mu.Lock()
	defer b.mu.Unlock()

	task := &Task{
		ID:          fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Description: description,
		Status:      StatusPending,
		Subtasks:    []Subtask{},
		CreatedAt:   time.Now().Unix(),
	}
	b.save(task)
	return task
}

// AddSubtask adds a subtask to the current task.
func (b *Board) AddSubtask(description string) (*Subtask, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	task := b.load()
	if task == nil {
		return nil, fmt.Errorf("no active task")
	}

	b.seqCount++
	sub := Subtask{
		ID:          fmt.Sprintf("sub-%d", b.seqCount),
		Description: description,
		Status:      StatusPending,
		CreatedAt:   time.Now().Unix(),
	}
	task.Subtasks = append(task.Subtasks, sub)
	task.Status = StatusInProgress
	b.save(task)

	return &sub, nil
}

// NextSubtask returns the next pending subtask and marks it as in_progress.
func (b *Board) NextSubtask() (*Subtask, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	task := b.load()
	if task == nil {
		return nil, fmt.Errorf("no active task")
	}

	for i := range task.Subtasks {
		if task.Subtasks[i].Status == StatusPending {
			task.Subtasks[i].Status = StatusInProgress
			b.save(task)
			return &task.Subtasks[i], nil
		}
	}
	return nil, nil // no pending subtasks
}

// CompleteSubtask marks a subtask as done. If all subtasks are done,
// the task is automatically completed and an event is emitted.
func (b *Board) CompleteSubtask(id string, result string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	task := b.load()
	if task == nil {
		return fmt.Errorf("no active task")
	}

	found := false
	for i := range task.Subtasks {
		if task.Subtasks[i].ID == id {
			task.Subtasks[i].Status = StatusDone
			task.Subtasks[i].Result = result
			task.Subtasks[i].DoneAt = time.Now().Unix()
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("subtask %q not found", id)
	}

	// Check if ALL subtasks are done → complete the task
	allDone := true
	for _, s := range task.Subtasks {
		if s.Status != StatusDone {
			allDone = false
			break
		}
	}

	if allDone && len(task.Subtasks) > 0 {
		task.Status = StatusDone
		task.DoneAt = time.Now().Unix()

		// Emit event via EventBus
		if b.bus != nil {
			b.bus.Publish(scheduler.Event{
				Type:    "TASK_COMPLETED",
				Source:  "taskboard",
				Content: fmt.Sprintf("Task %q completed: %s", task.ID, task.Description),
			})
		}
	}

	b.save(task)
	return nil
}

// GetTask returns the current task state.
func (b *Board) GetTask() *Task {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.load()
}

// Status returns a summary of the task progress.
func (b *Board) TaskStatus() (pending, inProgress, done int, total int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	task := b.load()
	if task == nil {
		return
	}
	for _, s := range task.Subtasks {
		total++
		switch s.Status {
		case StatusPending:
			pending++
		case StatusInProgress:
			inProgress++
		case StatusDone:
			done++
		}
	}
	return
}

// --- persistence helpers ---

func (b *Board) save(task *Task) {
	b.bb.Write(b.taskKey, task)
}

func (b *Board) load() *Task {
	raw := b.bb.Read(b.taskKey)
	if raw == nil {
		return nil
	}
	// Handle both direct pointer and JSON round-trip from snapshot
	if t, ok := raw.(*Task); ok {
		return t
	}
	// JSON fallback
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var t Task
	if json.Unmarshal(data, &t) == nil {
		return &t
	}
	return nil
}

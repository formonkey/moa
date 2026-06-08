package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/runner"
)

// Server exposes a moa Runner as an A2A-compatible HTTP server.
type Server struct {
	card   AgentCard
	runner *runner.Runner
	mux    *http.ServeMux

	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewServer creates an A2A server wrapping a moa runner.
func NewServer(r *runner.Runner, card AgentCard) *Server {
	s := &Server{
		card:   card,
		runner: r,
		mux:    http.NewServeMux(),
		tasks:  make(map[string]*Task),
	}

	s.mux.HandleFunc("GET /.well-known/agent-card.json", s.handleAgentCard)
	s.mux.HandleFunc("POST /tasks/send", s.handleSendTask)
	s.mux.HandleFunc("GET /tasks/{id}", s.handleGetTask)
	s.mux.HandleFunc("POST /tasks/{id}/cancel", s.handleCancelTask)
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)

	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// ListenAndServe starts the A2A server.
func (s *Server) ListenAndServe(addr string) error {
	slog.Info("a2a server starting", "name", s.card.Name, "addr", addr)
	return http.ListenAndServe(addr, s)
}

// --- Handlers ---

func (s *Server) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.card)
}

func (s *Server) handleSendTask(w http.ResponseWriter, r *http.Request) {
	var req SendTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON: "+err.Error())
		return
	}

	// Create task
	now := time.Now().UTC()
	taskID := fmt.Sprintf("task-%d", now.UnixNano())

	task := &Task{
		ID:        taskID,
		State:     TaskStateSubmitted,
		Messages:  []Message{req.Message},
		Metadata:  req.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
		History: []TaskStateTransition{
			{State: TaskStateSubmitted, Timestamp: now},
		},
	}

	s.mu.Lock()
	s.tasks[taskID] = task
	s.mu.Unlock()

	// Run agent asynchronously
	go s.executeTask(taskID, req.Message)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(SendTaskResponse{Task: *task})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "task ID required")
		return
	}

	s.mu.RLock()
	task, ok := s.tasks[id]
	s.mu.RUnlock()

	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found: "+id)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SendTaskResponse{Task: *task})
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "task ID required")
		return
	}

	s.mu.Lock()
	task, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "not_found", "task not found: "+id)
		return
	}

	if task.State.IsTerminal() {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "already_terminal", "task already in terminal state: "+string(task.State))
		return
	}

	now := time.Now().UTC()
	task.State = TaskStateCanceled
	task.UpdatedAt = now
	task.History = append(task.History, TaskStateTransition{State: TaskStateCanceled, Timestamp: now})
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SendTaskResponse{Task: *task})
}

// --- Task execution ---

func (s *Server) executeTask(taskID string, msg Message) {
	s.transition(taskID, TaskStateWorking)

	// Build genai.Content from message parts
	content := messageToContent(msg)

	ctx := context.Background()
	var responseMessages []Message

	for evt, err := range s.runner.Run(ctx, "a2a-user", taskID, content, agent.RunConfig{}) {
		// Check if canceled
		s.mu.RLock()
		task := s.tasks[taskID]
		canceled := task.State == TaskStateCanceled
		s.mu.RUnlock()
		if canceled {
			return
		}

		if err != nil {
			s.mu.Lock()
			task := s.tasks[taskID]
			now := time.Now().UTC()
			task.State = TaskStateFailed
			task.UpdatedAt = now
			task.Error = &TaskError{Code: "execution_error", Message: err.Error()}
			task.History = append(task.History, TaskStateTransition{State: TaskStateFailed, Timestamp: now})
			s.mu.Unlock()
			return
		}

		if evt != nil && evt.Content != nil {
			agentMsg := contentToMessage(evt.Content, "agent")
			responseMessages = append(responseMessages, agentMsg)
		}
	}

	// Complete
	s.mu.Lock()
	task := s.tasks[taskID]
	now := time.Now().UTC()
	task.State = TaskStateCompleted
	task.UpdatedAt = now
	task.Messages = append(task.Messages, responseMessages...)
	task.History = append(task.History, TaskStateTransition{State: TaskStateCompleted, Timestamp: now})
	s.mu.Unlock()
}

func (s *Server) transition(taskID string, state TaskState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	now := time.Now().UTC()
	task.State = state
	task.UpdatedAt = now
	task.History = append(task.History, TaskStateTransition{State: state, Timestamp: now})
}

// --- Converters ---

func messageToContent(msg Message) *genai.Content {
	parts := make([]*genai.Part, 0, len(msg.Parts))
	for _, p := range msg.Parts {
		switch p.Type {
		case "text":
			parts = append(parts, &genai.Part{Text: p.Text})
		case "data":
			if data, err := json.Marshal(p.Data); err == nil {
				parts = append(parts, &genai.Part{Text: string(data)})
			}
		}
	}
	if len(parts) == 0 {
		parts = []*genai.Part{{Text: ""}}
	}
	return &genai.Content{Role: msg.Role, Parts: parts}
}

func contentToMessage(content *genai.Content, role string) Message {
	parts := make([]Part, 0, len(content.Parts))
	for _, p := range content.Parts {
		if p.Text != "" {
			parts = append(parts, TextPart(p.Text))
		}
	}
	if role == "" {
		role = content.Role
	}
	return Message{Role: role, Parts: parts}
}

// --- Helpers ---

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

// Serve is a convenience function to start an A2A server.
func Serve(r *runner.Runner, card AgentCard, addr string) error {
	s := NewServer(r, card)
	return s.ListenAndServe(addr)
}

// --- Health checks ---

// handleHealthz is a liveness probe — returns 200 if the server is running.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"agent":  s.card.Name,
	})
}

// handleReadyz is a readiness probe — returns 200 with task statistics.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	total := len(s.tasks)
	working := 0
	completed := 0
	failed := 0
	for _, t := range s.tasks {
		switch t.State {
		case TaskStateWorking:
			working++
		case TaskStateCompleted:
			completed++
		case TaskStateFailed:
			failed++
		}
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "ready",
		"agent":     s.card.Name,
		"tasks":     total,
		"working":   working,
		"completed": completed,
		"failed":    failed,
	})
}


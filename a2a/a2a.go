// Package a2a implements Google's Agent-to-Agent (A2A) protocol for moa.
//
// A2A enables interoperability between AI agents, allowing them to discover
// each other's capabilities, delegate tasks, and collaborate — regardless
// of framework or model provider.
//
// The protocol uses HTTP + JSON + SSE, matching moa's zero-dependency philosophy.
//
// See: https://github.com/a2aproject/A2A
package a2a

import (
	"encoding/json"
	"time"
)

// AgentCard describes an agent's identity, capabilities, and endpoint.
// Served at GET /.well-known/agent-card.json
type AgentCard struct {
	Name            string       `json:"name"`
	Description     string       `json:"description,omitempty"`
	Version         string       `json:"version,omitempty"`
	URL             string       `json:"url"`
	DocumentationURL string      `json:"documentationUrl,omitempty"`
	Provider        *Provider    `json:"provider,omitempty"`
	Capabilities    Capabilities `json:"capabilities"`
	Skills          []Skill      `json:"skills,omitempty"`
	Authentication  *AuthConfig  `json:"authentication,omitempty"`
}

// Provider describes the organization behind the agent.
type Provider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// Capabilities declares what the agent supports.
type Capabilities struct {
	Streaming              bool `json:"streaming"`
	PushNotifications      bool `json:"pushNotifications,omitempty"`
	StateTransitionHistory bool `json:"stateTransitionHistory,omitempty"`
}

// Skill describes a specific capability of the agent.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// AuthConfig describes authentication requirements.
type AuthConfig struct {
	Schemes []string `json:"schemes"` // "apiKey", "oauth2", "bearer", etc.
}

// MarshalJSON implements json.Marshaler for AgentCard.
func (c AgentCard) MarshalJSON() ([]byte, error) {
	type Alias AgentCard
	return json.Marshal(struct {
		Alias
		Protocol string `json:"protocol"`
	}{
		Alias:    Alias(c),
		Protocol: "a2a/1.0",
	})
}

// --- Task ---

// TaskState represents the current state of a task.
type TaskState string

const (
	TaskStateSubmitted     TaskState = "submitted"
	TaskStateWorking       TaskState = "working"
	TaskStateInputRequired TaskState = "input_required"
	TaskStateCompleted     TaskState = "completed"
	TaskStateFailed        TaskState = "failed"
	TaskStateCanceled      TaskState = "canceled"
	TaskStateRejected      TaskState = "rejected"
)

// IsTerminal returns true if the task state is a terminal state.
func (s TaskState) IsTerminal() bool {
	return s == TaskStateCompleted || s == TaskStateFailed ||
		s == TaskStateCanceled || s == TaskStateRejected
}

// Task represents a unit of work in the A2A protocol.
type Task struct {
	ID        string     `json:"id"`
	State     TaskState  `json:"state"`
	Messages  []Message  `json:"messages,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Error     *TaskError `json:"error,omitempty"`
	History   []TaskStateTransition `json:"history,omitempty"`
}

// TaskError describes why a task failed.
type TaskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// TaskStateTransition records a state change.
type TaskStateTransition struct {
	State     TaskState `json:"state"`
	Timestamp time.Time `json:"timestamp"`
}

// --- Messages ---

// Message represents a message in a task conversation.
type Message struct {
	Role  string `json:"role"` // "user" or "agent"
	Parts []Part `json:"parts"`
}

// Part is a content part within a message.
type Part struct {
	Type     string `json:"type"` // "text", "data", "file"
	Text     string `json:"text,omitempty"`
	Data     any    `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
	URI      string `json:"uri,omitempty"`
}

// TextPart creates a text part.
func TextPart(text string) Part {
	return Part{Type: "text", Text: text}
}

// DataPart creates a structured data part.
func DataPart(data any) Part {
	return Part{Type: "data", Data: data}
}

// --- Artifact ---

// Artifact represents an output produced by the agent during task execution.
type Artifact struct {
	Name     string `json:"name,omitempty"`
	Parts    []Part `json:"parts"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// --- Requests/Responses ---

// SendTaskRequest is the payload for POST /tasks/send.
type SendTaskRequest struct {
	Message  Message        `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
	// SkillID optionally targets a specific skill.
	SkillID  string         `json:"skillId,omitempty"`
}

// SendTaskResponse is the response from POST /tasks/send.
type SendTaskResponse struct {
	Task Task `json:"task"`
}

// GetTaskRequest is the query for GET /tasks/:id.
type GetTaskRequest struct {
	ID string `json:"id"`
}

// CancelTaskRequest is the payload for POST /tasks/:id/cancel.
type CancelTaskRequest struct {
	ID string `json:"id"`
}

// TaskEvent is an SSE event for streaming task updates.
type TaskEvent struct {
	Type string `json:"type"` // "state_change", "message", "artifact", "error"
	Task *Task  `json:"task,omitempty"`
}

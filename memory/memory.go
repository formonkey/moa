package memory

import (
	"context"
	"time"

	"google.golang.org/genai"

	"github.com/formonkey/moa/session"
)

// Message represents an atomic interaction unit in the blackboard.
// It is JSON-tagged to ensure easy serialization for traces and Visual Builder.
type Message struct {
	SessionID string `json:"session_id"`
	AgentName string `json:"agent_name"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// Store defines the stigmergic blackboard persistence.
// Implementations can be BadgerDB, Redis, or simple in-memory stores.
// This is the legacy interface — prefer Service for new code.
type Store interface {
	Init() error
	Close() error
	AddMessage(msg Message) error
	GetSessionHistory(sessionID string) ([]Message, error)
}

// --- ADK-compatible Memory Service ---

// Service provides session-level memory operations with semantic search.
// This is the richer interface inspired by ADK Go, complementing the legacy Store.
type Service interface {
	// AddSessionToMemory indexes all events from a session into memory.
	AddSessionToMemory(ctx context.Context, sess session.Session) error
	// SearchMemory finds memories matching the query for a given user/app.
	SearchMemory(ctx context.Context, req *SearchRequest) (*SearchResponse, error)
}

// SearchRequest is a request to search memory.
type SearchRequest struct {
	AppName string
	UserID  string
	Query   string
}

// SearchResponse is a response from a memory search.
type SearchResponse struct {
	Memories []Entry
}

// Entry represents a single memory result.
type Entry struct {
	ID             string
	Content        *genai.Content
	Author         string
	Timestamp      time.Time
	CustomMetadata map[string]any
}

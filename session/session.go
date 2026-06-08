// Package session defines the core session management primitives for go-brain.
//
// A session represents a series of interactions between a user and agents.
// Sessions hold conversation history, mutable state, and support event-driven
// communication between agents via EventActions (transfer, escalation, etc.).
package session

import (
	"errors"
	"iter"
	"time"

	"github.com/google/uuid"

	"github.com/formonkey/moa/model"
)

// Session represents a series of interactions between a user and agents.
// When a user starts interacting with your agent, session holds everything
// related to that one specific chat thread.
type Session interface {
	// ID returns the unique identifier of the session.
	ID() string
	// AppName returns name of the app.
	AppName() string
	// UserID returns the id of the user.
	UserID() string

	// State returns the mutable state of the session.
	State() State
	// Events return the events of the session (user input, model response, function call/response, etc.).
	Events() Events
	// LastUpdateTime returns the time of the last update.
	LastUpdateTime() time.Time
}

// State defines a standard interface for a key-value store.
// It provides basic methods for accessing, modifying, and iterating over
// key-value pairs. State supports scoped keys via prefixes (app:, user:, temp:).
type State interface {
	// Get retrieves the value associated with a given key.
	// It returns ErrStateKeyNotExist if the key does not exist.
	Get(string) (any, error)
	// Set assigns the given value to the given key, overwriting any existing value.
	Set(string, any) error
	// All returns an iterator that yields all key-value pairs in the state.
	All() iter.Seq2[string, any]
}

// ReadonlyState provides read-only access to a State.
type ReadonlyState interface {
	// Get retrieves the value associated with a given key.
	Get(string) (any, error)
	// All returns an iterator that yields all key-value pairs in the state.
	All() iter.Seq2[string, any]
}

// Events defines a standard interface for an Event list.
type Events interface {
	// All returns an iterator that yields all events in order.
	All() iter.Seq[*Event]
	// Len returns the total number of events.
	Len() int
	// At returns the event at the specified index.
	At(i int) *Event
}

// Event represents an interaction in a conversation between agents and users.
// It stores the content of the conversation and the actions taken by agents.
type Event struct {
	model.LLMResponse

	// Set by storage.
	ID        string
	Timestamp time.Time

	// Set by agent context.
	InvocationID string
	// Branch of the event (e.g. "agent_1.agent_2.agent_3").
	// Used when multiple sub-agents shouldn't see peer agents' history.
	Branch string
	// Author is the name of the event's author.
	Author string

	// Actions taken by the agent.
	Actions EventActions
	// Set of IDs of long-running function calls.
	LongRunningToolIDs []string
}

// IsFinalResponse returns whether the event is the final response of an agent.
func (e *Event) IsFinalResponse() bool {
	if e.Actions.SkipSummarization || len(e.LongRunningToolIDs) > 0 {
		return true
	}
	return !hasFunctionCalls(&e.LLMResponse) &&
		!hasFunctionResponses(&e.LLMResponse) &&
		!e.LLMResponse.Partial &&
		!hasTrailingCodeExecutionResult(&e.LLMResponse)
}

// NewEvent creates a new event with a generated ID and current timestamp.
func NewEvent(invocationID string) *Event {
	return &Event{
		ID:           uuid.NewString(),
		InvocationID: invocationID,
		Timestamp:    time.Now(),
		Actions: EventActions{
			StateDelta:    make(map[string]any),
			ArtifactDelta: make(map[string]int64),
		},
	}
}

// EventActions represent the actions attached to an event.
type EventActions struct {
	// StateDelta contains state changes made by the agent.
	StateDelta map[string]any
	// ArtifactDelta tracks artifact updates. Key is filename, value is version.
	ArtifactDelta map[string]int64

	// RequestedToolConfirmations tracks HITL tool confirmation requests.
	// Key is the function call ID, value is the confirmation state.
	RequestedToolConfirmations map[string]ToolConfirmation

	// If true, it won't call model to summarize function response.
	SkipSummarization bool
	// If set, the event transfers control to the specified agent.
	TransferToAgent string
	// The agent is escalating to a higher level agent.
	Escalate bool
}

// ToolConfirmation represents the state of a Human-in-the-Loop confirmation.
type ToolConfirmation struct {
	// Confirmed is true if the user approved the tool call.
	Confirmed bool
	// Hint is a human-readable description of why confirmation was needed.
	Hint string
	// Payload contains additional context about the action requiring confirmation.
	Payload any
}

// Prefixes for defining session state scopes.
const (
	// KeyPrefixApp is for app-level state keys (shared across all users and sessions).
	KeyPrefixApp string = "app:"
	// KeyPrefixTemp is for temporary state keys (discarded after the invocation completes).
	KeyPrefixTemp string = "temp:"
	// KeyPrefixUser is for user-level state keys (shared across all sessions for that user).
	KeyPrefixUser string = "user:"
)

// ErrStateKeyNotExist is the error returned when a state key does not exist.
var ErrStateKeyNotExist = errors.New("state key does not exist")

// --- internal helpers ---

func hasFunctionCalls(resp *model.LLMResponse) bool {
	if resp == nil || resp.Content == nil {
		return false
	}
	for _, part := range resp.Content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}

func hasFunctionResponses(resp *model.LLMResponse) bool {
	if resp == nil || resp.Content == nil {
		return false
	}
	for _, part := range resp.Content.Parts {
		if part.FunctionResponse != nil {
			return true
		}
	}
	return false
}

func hasTrailingCodeExecutionResult(resp *model.LLMResponse) bool {
	if resp == nil || resp.Content == nil || len(resp.Content.Parts) == 0 {
		return false
	}
	lastPart := resp.Content.Parts[len(resp.Content.Parts)-1]
	return lastPart.CodeExecutionResult != nil
}

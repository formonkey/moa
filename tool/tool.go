// Package tool defines the core tool interfaces and composable utilities for go-brain.
//
// Tools are capabilities that agents can invoke. There are three categories:
//   - RunnableTool: executes local Go code (builtin, functiontool, agenttool)
//   - NativeTool: intercepts the LLM request to activate model-native features (Google Search)
//   - Toolset: manages a dynamic group of tools
//
// Composable utilities like WithConfirmation, FilterToolset, and Predicate
// allow building sophisticated tool pipelines without modifying tool implementations.
package tool

import (
	"context"
	"fmt"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
	"google.golang.org/genai"
)

// Tool represents any capability an agent can use.
// It is designed to be easily serializable for Visual Builders.
type Tool interface {
	Name() string
	Description() string
	// IsNative indicates if this tool intercepts the LLM request natively
	// rather than executing code locally.
	IsNative() bool
	// IsLongRunning indicates the tool may take a long time to complete.
	// The LLM will be instructed not to re-call it if it returns an intermediate status.
	IsLongRunning() bool
}

// RunnableTool executes local Go code (e.g., shell commands, Go functions, agent delegation).
type RunnableTool interface {
	Tool
	Declaration() *genai.FunctionDeclaration
	Execute(ctx context.Context, args map[string]any) (map[string]any, error)
}

// NativeTool intercepts the LLM request to inject native model capabilities (like Google Search).
type NativeTool interface {
	Tool
	ProcessRequest(req *model.LLMRequest) error
}

// Toolset manages a group of tools dynamically.
type Toolset interface {
	Name() string
	Tools() []Tool
}

// --- Tool Context ---

// Context provides runtime context to tools during execution.
// It gives access to session state, actions, memory search, and HITL confirmation.
type Context interface {
	context.Context

	// FunctionCallID returns the ID of the current function call.
	FunctionCallID() string
	// Actions returns the mutable event actions for the current invocation.
	Actions() *session.EventActions
	// State returns the session state.
	State() session.State

	// SearchMemory searches the memory service for relevant content.
	SearchMemory(ctx context.Context, query string) ([]map[string]any, error)

	// ToolConfirmation returns the confirmation status if one exists for this call.
	ToolConfirmation() *ConfirmationStatus
	// RequestConfirmation pauses execution and requests HITL approval.
	RequestConfirmation(hint string, payload any) error
}

// --- HITL (Human In The Loop) ---

// ErrConfirmationRequired is returned when a tool pauses execution waiting for HITL approval.
var ErrConfirmationRequired = fmt.Errorf("human-in-the-loop confirmation required")

// ErrConfirmationRejected is returned if the user denied the execution.
var ErrConfirmationRejected = fmt.Errorf("tool execution rejected by human")

// ConfirmationStatus tracks the state of a HITL request.
type ConfirmationStatus struct {
	Required  bool
	Confirmed bool
	Rejected  bool
	Payload   any
}

// ConfirmationContext is the legacy HITL context interface.
// Prefer using tool.Context for new tools.
type ConfirmationContext interface {
	context.Context

	// GetConfirmation returns the current HITL state.
	GetConfirmation() *ConfirmationStatus

	// RequestConfirmation pauses execution and emits a HITL event.
	RequestConfirmation(hint string, payload any) error
}

// --- Predicate & Composable Utilities ---

// Predicate is a function that filters tools dynamically.
type Predicate func(t Tool) bool

// AllowedToolsPredicate returns a Predicate that only allows tools with the given names.
func AllowedToolsPredicate(names ...string) Predicate {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return func(t Tool) bool {
		return set[t.Name()]
	}
}

// FilterToolset wraps a Toolset and filters its tools using a Predicate.
func FilterToolset(ts Toolset, pred Predicate) Toolset {
	return &filteredToolset{inner: ts, pred: pred}
}

type filteredToolset struct {
	inner Toolset
	pred  Predicate
}

func (f *filteredToolset) Name() string { return f.inner.Name() }
func (f *filteredToolset) Tools() []Tool {
	var result []Tool
	for _, t := range f.inner.Tools() {
		if f.pred(t) {
			result = append(result, t)
		}
	}
	return result
}

// WithConfirmation wraps a Toolset so that every tool in it requires HITL
// confirmation before execution. This is a composable decorator.
func WithConfirmation(ts Toolset) Toolset {
	return &confirmedToolset{inner: ts}
}

type confirmedToolset struct {
	inner Toolset
}

func (c *confirmedToolset) Name() string { return c.inner.Name() }
func (c *confirmedToolset) Tools() []Tool {
	original := c.inner.Tools()
	wrapped := make([]Tool, len(original))
	for i, t := range original {
		if rt, ok := t.(RunnableTool); ok {
			wrapped[i] = &confirmedTool{inner: rt}
		} else {
			wrapped[i] = t
		}
	}
	return wrapped
}

// confirmedTool wraps a RunnableTool to require HITL confirmation.
type confirmedTool struct {
	inner RunnableTool
}

func (c *confirmedTool) Name() string                            { return c.inner.Name() }
func (c *confirmedTool) Description() string                     { return c.inner.Description() }
func (c *confirmedTool) IsNative() bool                          { return false }
func (c *confirmedTool) IsLongRunning() bool                     { return c.inner.IsLongRunning() }
func (c *confirmedTool) Declaration() *genai.FunctionDeclaration { return c.inner.Declaration() }

func (c *confirmedTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	// Check if confirmation context is available
	if cCtx, ok := ctx.(ConfirmationContext); ok {
		status := cCtx.GetConfirmation()
		if status == nil || (!status.Confirmed && !status.Rejected) {
			_ = cCtx.RequestConfirmation(
				fmt.Sprintf("Please approve tool call: %s", c.inner.Name()), args)
			return nil, ErrConfirmationRequired
		}
		if status.Rejected {
			return nil, ErrConfirmationRejected
		}
	}
	return c.inner.Execute(ctx, args)
}

// --- Simple Toolset ---

// SimpleToolset is a basic toolset that wraps a slice of tools.
func SimpleToolset(name string, tools ...Tool) Toolset {
	return &simpleToolset{name: name, tools: tools}
}

type simpleToolset struct {
	name  string
	tools []Tool
}

func (s *simpleToolset) Name() string  { return s.name }
func (s *simpleToolset) Tools() []Tool { return s.tools }

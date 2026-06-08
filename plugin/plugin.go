// Package plugin provides a composable lifecycle hook system for go-brain agents.
//
// Plugins intercept agent execution at multiple points: before/after the agent runs,
// before/after model calls, before/after tool execution, on user messages, and on events.
// This enables cross-cutting concerns like logging, metrics, caching, retry logic,
// and function call modification without modifying agent or tool code.
package plugin

import (
	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/tool"
)

// Plugin defines a set of lifecycle hooks that intercept agent execution.
// All callbacks are optional — only set the ones you need.
type Plugin struct {
	// Name identifies the plugin (used in logging/telemetry).
	Name string

	// OnUserMessageCallback is called when a user message is received by the runner.
	OnUserMessageCallback OnUserMessageCallback

	// OnEventCallback is called for every event produced during execution.
	OnEventCallback OnEventCallback

	// BeforeRunCallback is called before the runner starts agent execution.
	BeforeRunCallback BeforeRunCallback
	// AfterRunCallback is called after the runner completes agent execution.
	AfterRunCallback AfterRunCallback

	// BeforeAgentCallback is called before an agent's Run method.
	BeforeAgentCallback agent.BeforeAgentCallback
	// AfterAgentCallback is called after an agent's Run method.
	AfterAgentCallback agent.AfterAgentCallback

	// BeforeModelCallback is called before sending a request to the LLM.
	BeforeModelCallback BeforeModelCallback
	// AfterModelCallback is called after receiving a response from the LLM.
	AfterModelCallback AfterModelCallback
	// OnModelErrorCallback is called when the LLM returns an error.
	OnModelErrorCallback OnModelErrorCallback

	// BeforeToolCallback is called before a tool's execution.
	BeforeToolCallback BeforeToolCallback
	// AfterToolCallback is called after a tool's execution.
	AfterToolCallback AfterToolCallback
	// OnToolErrorCallback is called when a tool execution fails.
	OnToolErrorCallback OnToolErrorCallback

	// CloseFunc is called when the runner shuts down. Use for cleanup.
	CloseFunc func() error
}

// --- Callback type definitions ---

// OnUserMessageCallback is called when a user message enters the runner.
type OnUserMessageCallback func(ctx agent.CallbackContext, msg *session.Event) (*session.Event, error)

// OnEventCallback is called for every event produced during agent execution.
type OnEventCallback func(ctx agent.CallbackContext, event *session.Event) (*session.Event, error)

// BeforeRunCallback is called before the runner starts agent execution.
type BeforeRunCallback func(ctx agent.CallbackContext) error

// AfterRunCallback is called after the runner completes.
type AfterRunCallback func(ctx agent.CallbackContext) error

// BeforeModelCallback is called before sending a request to the model.
// Return non-nil LLMResponse to skip the actual model call.
type BeforeModelCallback func(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error)

// AfterModelCallback is called after receiving a response from the model.
type AfterModelCallback func(ctx agent.CallbackContext, resp *model.LLMResponse) (*model.LLMResponse, error)

// OnModelErrorCallback is called when the model returns an error.
type OnModelErrorCallback func(ctx agent.CallbackContext, req *model.LLMRequest, err error) (*model.LLMResponse, error)

// BeforeToolCallback is called before a tool's execution.
type BeforeToolCallback func(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error)

// AfterToolCallback is called after a tool completes successfully.
type AfterToolCallback func(ctx agent.CallbackContext, t tool.Tool, args, result map[string]any) (map[string]any, error)

// OnToolErrorCallback is called when a tool execution fails.
type OnToolErrorCallback func(ctx agent.CallbackContext, t tool.Tool, args map[string]any, err error) (map[string]any, error)

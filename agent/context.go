package agent

import (
	"context"

	"google.golang.org/genai"

	"github.com/formonkey/moa/session"
)

// InvocationContext represents the context of an agent invocation.
//
// An invocation starts with a user message and ends with a final response.
// It can contain one or multiple agent calls, and is handled by runner.Run().
type InvocationContext interface {
	context.Context

	// Agent of this invocation context.
	Agent() Agent
	// Artifacts of the current session.
	Artifacts() Artifacts
	// Memory is scoped to sessions of the current user_id.
	Memory() Memory
	// Session of the current invocation context.
	Session() session.Session

	InvocationID() string
	// Branch of the invocation context (e.g. "agent_1.agent_2.agent_3").
	Branch() string
	// UserContent that started this invocation.
	UserContent() *genai.Content
	// RunConfig stores the runtime configuration.
	RunConfig() *RunConfig

	// EndInvocation ends the current invocation.
	EndInvocation()
	// Ended returns whether the invocation has ended.
	Ended() bool
	// WithContext returns a new instance with overridden embedded context.
	WithContext(ctx context.Context) InvocationContext

	// ParentMap returns the precomputed parent map for the agent tree.
	ParentMap() ParentMap
	// EventBus returns the event bus for streaming events to UI consumers.
	// This is a go-brain exclusive feature not found in ADK Go.
	EventBus() *EventBus
	// PluginHooks returns the plugin hooks injected by the runner.
	// This is an opaque value that agents can type-assert to call plugin callbacks.
	PluginHooks() any
}

// ReadonlyContext provides read-only access to invocation context data.
type ReadonlyContext interface {
	context.Context

	UserContent() *genai.Content
	InvocationID() string
	AgentName() string
	ReadonlyState() session.ReadonlyState

	UserID() string
	AppName() string
	SessionID() string
	Branch() string
}

// CallbackContext is passed to user callbacks during agent execution.
type CallbackContext interface {
	ReadonlyContext

	Artifacts() Artifacts
	State() session.State
}

// Artifacts provides methods to work with artifacts of the current session.
type Artifacts interface {
	Save(ctx context.Context, name string, data *genai.Part) (int64, error)
	List(ctx context.Context) ([]string, error)
	Load(ctx context.Context, name string) (*genai.Part, error)
}

// Memory provides methods to access agent memory across sessions.
type Memory interface {
	AddSessionToMemory(context.Context, session.Session) error
	SearchMemory(ctx context.Context, query string) ([]MemoryEntry, error)
}

// MemoryEntry represents a single memory search result.
type MemoryEntry struct {
	ID        string
	Content   *genai.Content
	Author    string
}

// --- Default InvocationContext implementation ---

// InvocationContextParams are parameters for creating a new InvocationContext.
type InvocationContextParams struct {
	Ctx          context.Context
	Agent        Agent
	Artifacts    Artifacts
	Memory       Memory
	Session      session.Session
	InvocationID string
	Branch       string
	UserContent  *genai.Content
	RunConfig    *RunConfig
	ParentMap    ParentMap
	EventBus     *EventBus
	PluginHooks  any
}

// NewInvocationContext creates a new InvocationContext from the given parameters.
func NewInvocationContext(params InvocationContextParams) InvocationContext {
	id := params.InvocationID
	if id == "" {
		id = "inv-" + params.Session.ID()
	}
	bus := params.EventBus
	if bus == nil {
		bus = NewEventBus()
	}
	return &invocationCtx{
		Context:      params.Ctx,
		agent:        params.Agent,
		artifacts:    params.Artifacts,
		memory:       params.Memory,
		session:      params.Session,
		invocationID: id,
		branch:       params.Branch,
		userContent:  params.UserContent,
		runConfig:    params.RunConfig,
		parentMap:    params.ParentMap,
		eventBus:     bus,
		pluginHooks:  params.PluginHooks,
	}
}

type invocationCtx struct {
	context.Context

	agent        Agent
	artifacts    Artifacts
	memory       Memory
	session      session.Session
	invocationID string
	branch       string
	userContent  *genai.Content
	runConfig    *RunConfig
	parentMap    ParentMap
	eventBus     *EventBus
	pluginHooks  any

	ended bool
}

func (c *invocationCtx) Agent() Agent              { return c.agent }
func (c *invocationCtx) Artifacts() Artifacts       { return c.artifacts }
func (c *invocationCtx) Memory() Memory             { return c.memory }
func (c *invocationCtx) Session() session.Session    { return c.session }
func (c *invocationCtx) InvocationID() string        { return c.invocationID }
func (c *invocationCtx) Branch() string              { return c.branch }
func (c *invocationCtx) UserContent() *genai.Content { return c.userContent }
func (c *invocationCtx) RunConfig() *RunConfig       { return c.runConfig }
func (c *invocationCtx) ParentMap() ParentMap         { return c.parentMap }
func (c *invocationCtx) EventBus() *EventBus         { return c.eventBus }
func (c *invocationCtx) PluginHooks() any              { return c.pluginHooks }
func (c *invocationCtx) EndInvocation()              { c.ended = true }
func (c *invocationCtx) Ended() bool                 { return c.ended }

func (c *invocationCtx) WithContext(ctx context.Context) InvocationContext {
	newCtx := *c
	newCtx.Context = ctx
	return &newCtx
}

// ReadonlyContext methods for callbackCtx delegation
func (c *invocationCtx) AgentName() string                   { return c.agent.Name() }
func (c *invocationCtx) ReadonlyState() session.ReadonlyState { return c.session.State() }
func (c *invocationCtx) UserID() string                       { return c.session.UserID() }
func (c *invocationCtx) AppName() string                      { return c.session.AppName() }
func (c *invocationCtx) SessionID() string                    { return c.session.ID() }

var _ InvocationContext = (*invocationCtx)(nil)

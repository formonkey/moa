// Package agent defines the core Agent interface and base implementation for go-brain.
//
// All agents must implement the Agent interface. Agents are created with constructors
// to ensure correct initialization:
//   - agent.New() for custom agents with a user-defined Run function
//   - llmagent.New() for LLM-powered agents (in the llmagent sub-package)
//   - Workflow agents: sequentialagent, parallelagent, loopagent
package agent

import (
	"fmt"
	"iter"

	"github.com/formonkey/moa/session"
)

// LLMAgentInternal exposes LLM-agent-specific configuration for the runner.
// Implemented by llmagent agents.
type LLMAgentInternal interface {
	DisallowTransferToParent() bool
}

// Agent is the base interface which all agents must implement.
type Agent interface {
	Name() string
	Description() string
	Run(InvocationContext) iter.Seq2[*session.Event, error]
	SubAgents() []Agent
	FindAgent(name string) Agent
	FindSubAgent(name string) Agent
}

// New creates an Agent with custom logic defined by the Run function.
func New(cfg Config) (Agent, error) {
	subAgentSet := make(map[string]bool)
	for _, sa := range cfg.SubAgents {
		if subAgentSet[sa.Name()] {
			return nil, fmt.Errorf("error creating agent: subagent %q appears multiple times", sa.Name())
		}
		subAgentSet[sa.Name()] = true
	}
	return &baseAgent{
		name:                 cfg.Name,
		description:          cfg.Description,
		subAgents:            cfg.SubAgents,
		beforeAgentCallbacks: cfg.BeforeAgentCallbacks,
		run:                  cfg.Run,
		afterAgentCallbacks:  cfg.AfterAgentCallbacks,
	}, nil
}

// Config is the configuration for creating a new Agent.
type Config struct {
	// Name must be a non-empty string, unique within the agent tree.
	// Agent name cannot be "user", since it's reserved for end-user input.
	Name string
	// Description of the agent's capability. LLM uses this to decide delegation.
	Description string
	// SubAgents are child agents that this agent can delegate tasks to.
	SubAgents []Agent

	// BeforeAgentCallbacks are called sequentially before the agent starts its run.
	// If any returns non-nil content or error, the agent run is skipped.
	BeforeAgentCallbacks []BeforeAgentCallback
	// Run defines the agent's behavior.
	Run func(InvocationContext) iter.Seq2[*session.Event, error]
	// AfterAgentCallbacks are called sequentially after the agent completes its run.
	AfterAgentCallbacks []AfterAgentCallback
}

// BeforeAgentCallback is called before the agent starts its run.
// If it returns non-nil content or error, the agent run will be skipped.
type BeforeAgentCallback func(CallbackContext) (*session.Event, error)

// AfterAgentCallback is called after the agent has completed its run.
type AfterAgentCallback func(CallbackContext) (*session.Event, error)

// --- baseAgent implementation ---

type baseAgent struct {
	name, description string
	subAgents         []Agent

	beforeAgentCallbacks []BeforeAgentCallback
	run                  func(InvocationContext) iter.Seq2[*session.Event, error]
	afterAgentCallbacks  []AfterAgentCallback
}

func (a *baseAgent) Name() string        { return a.name }
func (a *baseAgent) Description() string { return a.description }
func (a *baseAgent) SubAgents() []Agent  { return a.subAgents }

func (a *baseAgent) Run(ctx InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		// Run before-agent callbacks
		for _, cb := range a.beforeAgentCallbacks {
			event, err := cb(&callbackCtx{InvocationContext: ctx})
			if event != nil || err != nil {
				yield(event, err)
				return
			}
		}

		if ctx.Ended() {
			return
		}

		// Run the agent's main logic
		if a.run != nil {
			for event, err := range a.run(ctx) {
				if event != nil && event.Author == "" {
					event.Author = a.name
				}
				if !yield(event, err) {
					return
				}
			}
		}

		if ctx.Ended() {
			return
		}

		// Run after-agent callbacks
		for _, cb := range a.afterAgentCallbacks {
			event, err := cb(&callbackCtx{InvocationContext: ctx})
			if event != nil || err != nil {
				yield(event, err)
				return
			}
		}
	}
}

func (a *baseAgent) FindAgent(name string) Agent {
	if a.name == name {
		return a
	}
	return a.FindSubAgent(name)
}

func (a *baseAgent) FindSubAgent(name string) Agent {
	for _, sub := range a.subAgents {
		if result := sub.FindAgent(name); result != nil {
			return result
		}
	}
	return nil
}

// --- callbackCtx ---

type callbackCtx struct {
	InvocationContext
	stateDelta map[string]any
}

func (c *callbackCtx) AgentName() string                   { return c.Agent().Name() }
func (c *callbackCtx) ReadonlyState() session.ReadonlyState { return c.Session().State() }
func (c *callbackCtx) State() session.State {
	if c.stateDelta == nil {
		c.stateDelta = make(map[string]any)
	}
	return &stateDeltaState{inner: c.Session().State(), delta: c.stateDelta}
}
func (c *callbackCtx) UserID() string    { return c.Session().UserID() }
func (c *callbackCtx) AppName() string   { return c.Session().AppName() }
func (c *callbackCtx) SessionID() string { return c.Session().ID() }

// StateDelta returns the accumulated state changes from callbacks.
func (c *callbackCtx) StateDelta() map[string]any { return c.stateDelta }

var _ CallbackContext = (*callbackCtx)(nil)

// stateDeltaState wraps session.State to also track changes in a delta map.
// This ensures state changes from callbacks are recorded for event replay.
type stateDeltaState struct {
	inner session.State
	delta map[string]any
}

func (s *stateDeltaState) Get(key string) (any, error) { return s.inner.Get(key) }
func (s *stateDeltaState) All() iter.Seq2[string, any] { return s.inner.All() }
func (s *stateDeltaState) Set(key string, value any) error {
	s.delta[key] = value
	return s.inner.Set(key, value)
}

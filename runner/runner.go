// Package runner provides the runtime orchestrator for go-brain agents.
//
// The Runner manages the execution of agents within sessions, handling
// message processing, event generation, and interaction with services
// like session management, artifacts, and memory.
package runner

import (
	"context"
	"fmt"
	"iter"
	"log"
	"sync"

	"github.com/google/uuid"
	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/session"
)

// Config is used to create a Runner.
type Config struct {
	AppName string
	// Root agent which starts execution.
	Agent agent.Agent
	// SessionService is required for session persistence.
	SessionService session.Service

	// Optional services.
	ArtifactService agent.Artifacts
	MemoryService   agent.Memory

	// Plugins to attach to the runner lifecycle.
	Plugins []*plugin.Plugin

	// AutoCreateSession will create a new session if one doesn't exist.
	AutoCreateSession bool
}

// RunOption configures a single Run invocation.
type RunOption func(*runOptions)

type runOptions struct {
	stateDelta map[string]any
}

// WithStateDelta injects an initial state delta into the invocation.
func WithStateDelta(delta map[string]any) RunOption {
	return func(o *runOptions) { o.stateDelta = delta }
}

// New creates a new Runner.
func New(cfg Config) (*Runner, error) {
	if cfg.Agent == nil {
		return nil, fmt.Errorf("root agent is required")
	}
	if cfg.SessionService == nil {
		return nil, fmt.Errorf("session service is required")
	}

	var pm *plugin.Manager
	if len(cfg.Plugins) > 0 {
		pm = plugin.NewManager(cfg.Plugins...)
	}

	// Precompute parent map for agent tree traversal
	parentMap, err := agent.BuildParentMap(cfg.Agent)
	if err != nil {
		return nil, fmt.Errorf("failed to build agent tree: %w", err)
	}

	return &Runner{
		appName:           cfg.AppName,
		rootAgent:         cfg.Agent,
		sessionService:    cfg.SessionService,
		artifactService:   cfg.ArtifactService,
		memoryService:     cfg.MemoryService,
		pluginManager:     pm,
		parentMap:         parentMap,
		autoCreateSession: cfg.AutoCreateSession,
	}, nil
}

// Runner manages the execution of agents within sessions.
type Runner struct {
	appName         string
	rootAgent       agent.Agent
	sessionService  session.Service
	artifactService agent.Artifacts
	memoryService   agent.Memory
	pluginManager   *plugin.Manager
	parentMap       agent.ParentMap

	autoCreateSession bool

	cancelMu sync.Mutex
	cancelFn context.CancelFunc // cancel for the active run, if any
}

// Close shuts down the runner gracefully.
// If a run is in progress, it is cancelled first. Then all plugins are closed.
func (r *Runner) Close() error {
	r.cancelMu.Lock()
	if r.cancelFn != nil {
		r.cancelFn()
		r.cancelFn = nil
	}
	r.cancelMu.Unlock()

	if r.pluginManager != nil {
		return r.pluginManager.Close()
	}
	return nil
}

// Run executes the agent for the given user input, yielding events from agents.
func (r *Runner) Run(ctx context.Context, userID, sessionID string, msg *genai.Content, cfg agent.RunConfig, opts ...RunOption) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		// Wrap context so Close() can cancel in-progress runs
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		r.cancelMu.Lock()
		r.cancelFn = cancel
		r.cancelMu.Unlock()
		defer func() {
			r.cancelMu.Lock()
			r.cancelFn = nil
			r.cancelMu.Unlock()
		}()

		options := runOptions{}
		for _, opt := range opts {
			opt(&options)
		}

		// Get or create session
		var storedSession session.Session
		getResp, err := r.sessionService.Get(ctx, &session.GetRequest{
			AppName:   r.appName,
			UserID:    userID,
			SessionID: sessionID,
		})
		if err != nil {
			if !r.autoCreateSession {
				yield(nil, fmt.Errorf("session not found: %w", err))
				return
			}
			createResp, err := r.sessionService.Create(ctx, &session.CreateRequest{
				AppName:   r.appName,
				UserID:    userID,
				SessionID: sessionID,
			})
			if err != nil {
				yield(nil, fmt.Errorf("failed to create session: %w", err))
				return
			}
			storedSession = createResp.Session
		} else {
			storedSession = getResp.Session
		}

		// Find the right agent based on session history
		agentToRun := r.findAgentToRun(storedSession, msg)

		invocationID := uuid.NewString()

		// Create event bus
		eventBus := agent.NewEventBus()

		// Build invocation context
		var hooks any
		if r.pluginManager != nil {
			hooks = r.pluginManager
		}
		invCtx := agent.NewInvocationContext(agent.InvocationContextParams{
			Ctx:          ctx,
			Agent:        agentToRun,
			Artifacts:    r.artifactService,
			Memory:       r.memoryService,
			Session:      storedSession,
			InvocationID: invocationID,
			UserContent:  msg,
			RunConfig:    &cfg,
			ParentMap:    r.parentMap,
			EventBus:     eventBus,
			PluginHooks:  hooks,
		})

		// Build callback context for plugin hooks
		cbCtx := &runnerCallbackCtx{InvocationContext: invCtx}

		// --- Plugin: OnUserMessage ---
		if r.pluginManager != nil && msg != nil {
			userEvent := &session.Event{
				LLMResponse: model.LLMResponse{Content: msg},
				Author:      "user",
			}
			modifiedEvent, err := r.pluginManager.OnUserMessage(cbCtx, userEvent)
			if err != nil {
				yield(nil, fmt.Errorf("plugin OnUserMessage error: %w", err))
				return
			}
			if modifiedEvent != nil && modifiedEvent.Content != nil {
				msg = modifiedEvent.Content
				// Rebuild invocation context with modified message
				invCtx = agent.NewInvocationContext(agent.InvocationContextParams{
					Ctx:          ctx,
					Agent:        agentToRun,
					Artifacts:    r.artifactService,
					Memory:       r.memoryService,
					Session:      storedSession,
					InvocationID: invocationID,
					UserContent:  msg,
					RunConfig:    &cfg,
					ParentMap:    r.parentMap,
					EventBus:     eventBus,
					PluginHooks:  hooks,
				})
				cbCtx = &runnerCallbackCtx{InvocationContext: invCtx}
			}
		}

		// --- SaveInputBlobsAsArtifacts ---
		if msg != nil && cfg.SaveInputBlobsAsArtifacts && r.artifactService != nil {
			// Clone the parts slice to avoid mutating the caller's original message.
			clonedParts := make([]*genai.Part, len(msg.Parts))
			copy(clonedParts, msg.Parts)
			msg = &genai.Content{Role: msg.Role, Parts: clonedParts}

			for i, part := range msg.Parts {
				if part.InlineData == nil {
					continue
				}
				fileName := fmt.Sprintf("artifact_%s_%d", invocationID, i)
				if _, err := r.artifactService.Save(ctx, fileName, part); err != nil {
					yield(nil, fmt.Errorf("failed to save input blob artifact %s: %w", fileName, err))
					return
				}
				// Replace the blob part with a text placeholder
				msg.Parts[i] = &genai.Part{
					Text: fmt.Sprintf("Uploaded file: %s. It has been saved to the artifacts.", fileName),
				}
			}
		}

		// Append user message to session
		if msg != nil {
			userEvent := session.NewEvent(invocationID)
			userEvent.Author = "user"
			userEvent.LLMResponse = model.LLMResponse{Content: msg}
			if options.stateDelta != nil {
				userEvent.Actions.StateDelta = options.stateDelta
			}
			if err := r.sessionService.AppendEvent(ctx, storedSession, userEvent); err != nil {
				yield(nil, fmt.Errorf("failed to append user message: %w", err))
				return
			}
		}

		// --- Plugin: BeforeRun ---
		if r.pluginManager != nil {
			if err := r.pluginManager.BeforeRun(cbCtx); err != nil {
				yield(nil, fmt.Errorf("plugin BeforeRun error: %w", err))
				return
			}
		}

		// --- Plugin: AfterRun (deferred) ---
		if r.pluginManager != nil {
			defer func() {
				if err := r.pluginManager.AfterRun(cbCtx); err != nil {
					log.Printf("[runner] plugin AfterRun error: %v", err)
				}
			}()
		}

		// Run the agent
		for event, err := range agentToRun.Run(invCtx) {
			if err != nil {
				if !yield(event, err) {
					return
				}
				continue
			}

			// --- Plugin: OnEvent ---
			if r.pluginManager != nil && event != nil {
				modifiedEvent, pErr := r.pluginManager.OnEvent(cbCtx, event)
				if pErr != nil {
					if !yield(nil, fmt.Errorf("plugin OnEvent error: %w", pErr)) {
						return
					}
					continue
				}
				if modifiedEvent != nil {
					event = modifiedEvent
				}
			}

			// Only persist non-partial events
			if event != nil && !event.Partial {
				if appendErr := r.sessionService.AppendEvent(ctx, storedSession, event); appendErr != nil {
					yield(nil, fmt.Errorf("failed to persist event: %w", appendErr))
					return
				}
			}

			if !yield(event, nil) {
				return
			}
		}
	}
}

// findAgentToRun returns the agent that should handle the next request
// based on session history (e.g., agent transfer events).
func (r *Runner) findAgentToRun(sess session.Session, msg *genai.Content) agent.Agent {
	events := sess.Events()

	// Check for function call responses from user (resuming HITL)
	if msg != nil {
		for _, part := range msg.Parts {
			if part.FunctionResponse != nil {
				// Find the agent that made the original function call
				for i := events.Len() - 1; i >= 0; i-- {
					event := events.At(i)
					if event.Content != nil {
						for _, p := range event.Content.Parts {
							if p.FunctionCall != nil && p.FunctionCall.ID == part.FunctionResponse.ID {
								if sub := r.rootAgent.FindAgent(event.Author); sub != nil {
									return sub
								}
							}
						}
					}
				}
			}
		}
	}

	// Walk events backwards to find the last active agent
	for i := events.Len() - 1; i >= 0; i-- {
		event := events.At(i)
		if event.Author == "user" {
			continue
		}

		// Check for transfer action
		if event.Actions.TransferToAgent != "" {
			if sub := r.rootAgent.FindAgent(event.Actions.TransferToAgent); sub != nil {
				if r.isTransferableAcrossAgentTree(sub) {
					return sub
				}
			}
		}

		// Return to the agent that last responded
		if sub := r.rootAgent.FindAgent(event.Author); sub != nil {
			if r.isTransferableAcrossAgentTree(sub) {
				return sub
			}
		}

		log.Printf("go-brain: event from unknown agent: %s (event %s)", event.Author, event.ID)
	}

	// Falls back to root agent
	return r.rootAgent
}

// isTransferableAcrossAgentTree walks the full parent chain checking if any agent
// has DisallowTransferToParent set. If it does, transfer is blocked.
func (r *Runner) isTransferableAcrossAgentTree(agentToRun agent.Agent) bool {
	for cur := agentToRun; cur != nil; cur = r.parentMap[cur.Name()] {
		if llmAgent, ok := cur.(agent.LLMAgentInternal); ok {
			if llmAgent.DisallowTransferToParent() {
				return false
			}
		}
	}
	return true
}

// --- runnerCallbackCtx bridges InvocationContext → CallbackContext ---

type runnerCallbackCtx struct {
	agent.InvocationContext
}

func (c *runnerCallbackCtx) AgentName() string                   { return c.Agent().Name() }
func (c *runnerCallbackCtx) ReadonlyState() session.ReadonlyState { return c.Session().State() }
func (c *runnerCallbackCtx) State() session.State                 { return c.Session().State() }
func (c *runnerCallbackCtx) UserID() string                       { return c.Session().UserID() }
func (c *runnerCallbackCtx) AppName() string                      { return c.Session().AppName() }
func (c *runnerCallbackCtx) SessionID() string                    { return c.Session().ID() }

var _ agent.CallbackContext = (*runnerCallbackCtx)(nil)

// Package sequentialagent provides a workflow agent that executes sub-agents in sequence.
//
// Each sub-agent runs one after another. The output from each agent is available
// in the session state for subsequent agents.
package sequentialagent

import (
	"fmt"
	"iter"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
)

// Config configures a sequential agent.
type Config struct {
	Name        string
	Description string
	SubAgents   []agent.Agent
}

// New creates a sequential workflow agent.
func New(cfg Config) (agent.Agent, error) {
	if len(cfg.SubAgents) == 0 {
		return nil, fmt.Errorf("sequentialagent: at least one sub-agent is required")
	}

	return agent.New(agent.Config{
		Name:        cfg.Name,
		Description: cfg.Description,
		SubAgents:   cfg.SubAgents,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				bus := ctx.EventBus()

				for _, sub := range cfg.SubAgents {
					if ctx.Ended() {
						return
					}

					bus.Emit(agent.BusEvent{
						Type:    agent.EventAgentSwitch,
						Agent:   cfg.Name,
						Payload: sub.Name(),
					})

					// Create a child invocation context for the sub-agent
					childCtx := agent.NewInvocationContext(agent.InvocationContextParams{
						Ctx:          ctx,
						Agent:        sub,
						Artifacts:    ctx.Artifacts(),
						Memory:       ctx.Memory(),
						Session:      ctx.Session(),
						InvocationID: ctx.InvocationID(),
						Branch:       ctx.Branch(),
						UserContent:  ctx.UserContent(),
						RunConfig:    ctx.RunConfig(),
						ParentMap:    ctx.ParentMap(),
						EventBus:     ctx.EventBus(),
					})

					for event, err := range sub.Run(childCtx) {
						if !yield(event, err) {
							return
						}
					}
				}
			}
		},
	})
}

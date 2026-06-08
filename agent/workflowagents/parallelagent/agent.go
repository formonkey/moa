// Package parallelagent provides a workflow agent that executes sub-agents concurrently.
//
// Each sub-agent runs in its own goroutine with a unique branch ID, enabling
// independent conversation histories. Results from all sub-agents are collected
// and yielded to the caller.
package parallelagent

import (
	"context"
	"fmt"
	"iter"
	"sync"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
)

// Config configures a parallel agent.
type Config struct {
	Name        string
	Description string
	SubAgents   []agent.Agent
}

// New creates a parallel workflow agent.
func New(cfg Config) (agent.Agent, error) {
	if len(cfg.SubAgents) == 0 {
		return nil, fmt.Errorf("parallelagent: at least one sub-agent is required")
	}

	return agent.New(agent.Config{
		Name:        cfg.Name,
		Description: cfg.Description,
		SubAgents:   cfg.SubAgents,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				bus := ctx.EventBus()

				type result struct {
					event *session.Event
					err   error
				}

				// Use a cancellable context so we can signal goroutines to stop
				// when the caller stops consuming (yield returns false).
				gCtx, cancel := context.WithCancel(ctx)
				defer cancel()

				resultCh := make(chan result, 64)
				var wg sync.WaitGroup

				for i, sub := range cfg.SubAgents {
					wg.Add(1)
					go func(idx int, subAgent agent.Agent) {
						defer wg.Done()

						// Each sub-agent gets a unique branch
						branch := ctx.Branch()
						if branch != "" {
							branch += "."
						}
						branch += fmt.Sprintf("%s.%d", subAgent.Name(), idx)

						bus.Emit(agent.BusEvent{
							Type:    agent.EventAgentSwitch,
							Agent:   cfg.Name,
							Payload: subAgent.Name(),
						})

						childCtx := agent.NewInvocationContext(agent.InvocationContextParams{
							Ctx:          gCtx,
							Agent:        subAgent,
							Artifacts:    ctx.Artifacts(),
							Memory:       ctx.Memory(),
							Session:      ctx.Session(),
							InvocationID: ctx.InvocationID(),
							Branch:       branch,
							UserContent:  ctx.UserContent(),
							RunConfig:    ctx.RunConfig(),
							ParentMap:    ctx.ParentMap(),
							EventBus:     ctx.EventBus(),
						})

						for event, err := range subAgent.Run(childCtx) {
							if event != nil {
								event.Branch = branch
							}
							select {
							case resultCh <- result{event: event, err: err}:
							case <-gCtx.Done():
								return // parent stopped consuming, exit cleanly
							}
						}
					}(i, sub)
				}

				// Close channel when all goroutines complete
				go func() {
					wg.Wait()
					close(resultCh)
				}()

				// Yield results as they arrive
				for r := range resultCh {
					if !yield(r.event, r.err) {
						cancel() // signal goroutines to stop
						return
					}
				}
			}
		},
	})
}

// Package postrun provides an AfterAgentCallback that publishes TASK_COMPLETED events
// to the swarm's EventBus when an agent finishes execution.
//
// This creates the feedback loop: agent completes → event → another agent wakes up.
package postrun

import (
	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/swarm/scheduler"
)

// NewCallback creates an AfterAgentCallback that publishes TASK_COMPLETED
// to the EventBus when the agent finishes.
func NewCallback(bus *scheduler.EventBus) agent.AfterAgentCallback {
	return func(ctx agent.CallbackContext) (*session.Event, error) {
		bus.Publish(scheduler.Event{
			Type:    "TASK_COMPLETED",
			Source:  ctx.AgentName(),
			Content: ctx.AgentName() + " completed execution",
		})
		return nil, nil
	}
}

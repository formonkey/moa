// Package blackboardplugin provides callbacks that inject/publish Blackboard state.
package blackboardplugin

import (
	"encoding/json"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/swarm/blackboard"
)

// NewInjector creates a BeforeAgentCallback that injects the Blackboard
// snapshot into session state under "blackboard_context".
func NewInjector(bb *blackboard.Blackboard) agent.BeforeAgentCallback {
	return func(ctx agent.CallbackContext) (*session.Event, error) {
		snap := bb.Snapshot()
		if len(snap) == 0 {
			return nil, nil
		}
		data, err := json.MarshalIndent(snap, "", "  ")
		if err != nil {
			return nil, nil
		}
		ctx.State().Set("blackboard_context", string(data))
		return nil, nil
	}
}

// NewPublisher creates an AfterAgentCallback that writes the agent's
// output to the Blackboard, making it available to peers.
func NewPublisher(bb *blackboard.Blackboard) agent.AfterAgentCallback {
	return func(ctx agent.CallbackContext) (*session.Event, error) {
		if output, err := ctx.State().Get("output"); err == nil {
			bb.Write(ctx.AgentName()+":result", output)
		}
		return nil, nil
	}
}

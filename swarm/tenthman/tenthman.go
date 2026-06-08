// Package tenthman provides a BeforeAgentCallback that implements the "Tenth Man Rule".
//
// While an agent is idle (before its main run), it checks the Blackboard for
// unclaimed peer artifacts and audits them adversarially. If a flaw is found,
// it publishes an AUDIT_FAILED event to the EventBus.
package tenthman

import (
	"fmt"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/swarm/blackboard"
	"github.com/formonkey/moa/swarm/scheduler"
)

// NewCallback creates a BeforeAgentCallback that implements the Tenth Man Rule.
func NewCallback(bb *blackboard.Blackboard, bus *scheduler.EventBus) agent.BeforeAgentCallback {
	return func(ctx agent.CallbackContext) (*session.Event, error) {
		artifact := bb.ClaimReview(ctx.AgentName())
		if artifact == nil {
			return nil, nil
		}

		fmt.Printf("[TenthMan] %s auditing artifact from %s\n", ctx.AgentName(), artifact.Author)

		ctx.State().Set("tenth_man_mode", true)
		ctx.State().Set("tenth_man_artifact_author", artifact.Author)
		ctx.State().Set("tenth_man_artifact_asset", artifact.Asset)
		ctx.State().Set("tenth_man_artifact_context", artifact.Context)

		return nil, nil
	}
}

// PostAuditCallback creates an AfterAgentCallback that publishes audit results.
func PostAuditCallback(bus *scheduler.EventBus) agent.AfterAgentCallback {
	return func(ctx agent.CallbackContext) (*session.Event, error) {
		mode, err := ctx.State().Get("tenth_man_mode")
		if err != nil || mode != true {
			return nil, nil
		}

		author, _ := ctx.State().Get("tenth_man_artifact_author")
		authorStr, _ := author.(string)

		verdict, _ := ctx.State().Get("tenth_man_verdict")
		verdictStr, _ := verdict.(string)

		if verdictStr == "PASS" {
			fmt.Printf("[TenthMan] %s: ✅ Artifact from %s passed\n", ctx.AgentName(), authorStr)
		} else {
			fmt.Printf("[TenthMan] %s: 🚨 Artifact from %s FAILED\n", ctx.AgentName(), authorStr)
			bus.Publish(scheduler.Event{
				Type:    "AUDIT_FAILED",
				Source:  ctx.AgentName(),
				Content: fmt.Sprintf("Tenth Man %s flagged work by %s: %s", ctx.AgentName(), authorStr, verdictStr),
			})
		}

		return nil, nil
	}
}

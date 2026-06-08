// Package loopagent provides a workflow agent that repeatedly executes a sub-agent
// until an exit condition is met.
//
// The loop can be terminated by:
//   - The sub-agent calling the "exit_loop" tool
//   - MaxIterations being reached
//   - The invocation context being ended
package loopagent

import (
	"context"
	"fmt"
	"iter"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/tool"
	"google.golang.org/genai"
)

// Config configures a loop agent.
type Config struct {
	Name        string
	Description string
	// SubAgent is the agent that runs in each iteration.
	SubAgent agent.Agent
	// MaxIterations limits the loop. 0 means unlimited. Default: 10.
	MaxIterations int
}

// New creates a loop workflow agent.
func New(cfg Config) (agent.Agent, error) {
	if cfg.SubAgent == nil {
		return nil, fmt.Errorf("loopagent: sub-agent is required")
	}

	maxIter := cfg.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}

	return agent.New(agent.Config{
		Name:        cfg.Name,
		Description: cfg.Description,
		SubAgents:   []agent.Agent{cfg.SubAgent},
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				shouldExit := false

				for i := 0; i < maxIter; i++ {
					if ctx.Ended() || shouldExit {
						return
					}

					childCtx := agent.NewInvocationContext(agent.InvocationContextParams{
						Ctx:          ctx,
						Agent:        cfg.SubAgent,
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

					for event, err := range cfg.SubAgent.Run(childCtx) {
						if err != nil {
							yield(nil, err)
							return
						}

						// Check if any sub-agent escalated (ADK's exit_loop pattern)
						if event != nil && event.Actions.Escalate {
							shouldExit = true
						}

						if !yield(event, nil) {
							return
						}
					}
				}
			}
		},
	})
}

// --- ExitLoopTool ---

// ExitLoopTool is a tool that signals the loop agent to stop iterating.
// It uses the Escalate action (matching ADK's pattern) to signal termination.
type ExitLoopTool struct{}

func (t *ExitLoopTool) Name() string        { return "exit_loop" }
func (t *ExitLoopTool) Description() string { return "Call this tool when the task is complete and the loop should stop." }
func (t *ExitLoopTool) IsNative() bool      { return false }
func (t *ExitLoopTool) IsLongRunning() bool { return false }

func (t *ExitLoopTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type:       genai.TypeObject,
			Properties: map[string]*genai.Schema{},
		},
	}
}

func (t *ExitLoopTool) Execute(_ context.Context, _ map[string]any) (map[string]any, error) {
	// The actual escalation is handled by the runner/flow setting Actions.Escalate.
	// This tool returns a marker that the flow layer can detect.
	return map[string]any{"status": "loop_exit_requested", "_escalate": true}, nil
}

var _ tool.RunnableTool = (*ExitLoopTool)(nil)

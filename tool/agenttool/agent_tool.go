// Package agenttool provides a tool that wraps an agent, allowing one agent
// to invoke another as a tool. This enables composition of agents when different
// tool types cannot be used together.
//
// Usage:
//
//	subAgent, _ := llmagent.New(llmagent.Config{...})
//	agTool := agenttool.New(subAgent, nil)
//	parentAgent, _ := llmagent.New(llmagent.Config{
//	    Tools: []tool.Tool{agTool},
//	})
package agenttool

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/runner"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/tool"
)

// Config holds configuration for an agent tool.
type Config struct {
	// SkipSummarization prevents the parent agent from summarizing sub-agent output.
	SkipSummarization bool
}

// New creates a tool that wraps an agent.
func New(a agent.Agent, cfg *Config) tool.RunnableTool {
	skipSum := false
	if cfg != nil {
		skipSum = cfg.SkipSummarization
	}
	return &agentTool{
		agent:             a,
		skipSummarization: skipSum,
	}
}

// --- agentTool implementation ---

type agentTool struct {
	agent             agent.Agent
	skipSummarization bool
}

func (t *agentTool) Name() string        { return t.agent.Name() }
func (t *agentTool) Description() string { return t.agent.Description() }
func (t *agentTool) IsNative() bool      { return false }
func (t *agentTool) IsLongRunning() bool { return false }

func (t *agentTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type: "OBJECT",
			Properties: map[string]*genai.Schema{
				"request": {Type: "STRING", Description: "The request to send to the sub-agent"},
			},
			Required: []string{"request"},
		},
	}
}

func (t *agentTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	// Extract the request text
	reqText, _ := args["request"].(string)
	if reqText == "" {
		return nil, fmt.Errorf("agenttool: missing 'request' argument")
	}

	content := genai.NewContentFromText(reqText, genai.RoleUser)

	// Create a temporary session service + runner for the sub-agent
	sessionService := session.InMemoryService()

	r, err := runner.New(runner.Config{
		AppName:           t.agent.Name(),
		Agent:             t.agent,
		SessionService:    sessionService,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("agenttool: failed to create runner: %w", err)
	}

	// Run the sub-agent
	var lastEvent *session.Event
	for event, err := range r.Run(ctx, "agenttool-user", "", content, agent.RunConfig{
		StreamingMode: agent.StreamingModeNone,
	}) {
		if err != nil {
			return nil, fmt.Errorf("agenttool: error from sub-agent %q: %w", t.agent.Name(), err)
		}
		if event == nil {
			continue
		}
		if event.ErrorCode != "" || event.ErrorMessage != "" {
			return nil, fmt.Errorf("agenttool: sub-agent %q error (code: %q, msg: %q)",
				t.agent.Name(), event.ErrorCode, event.ErrorMessage)
		}
		if event.Content != nil && !event.Partial {
			lastEvent = event
		}
	}

	if lastEvent == nil {
		return map[string]any{}, nil
	}

	// Extract text from the last event
	var textParts []string
	for _, part := range lastEvent.Content.Parts {
		if part != nil && part.Text != "" {
			textParts = append(textParts, part.Text)
		}
	}
	outputText := strings.Join(textParts, "\n")

	if outputText == "" {
		return map[string]any{}, nil
	}

	return map[string]any{"result": outputText}, nil
}

// Ensure a sub-agent tool that set SkipSummarization affects the caller's actions
// when used with tool.Context.
func (t *agentTool) ShouldSkipSummarization() bool {
	return t.skipSummarization
}

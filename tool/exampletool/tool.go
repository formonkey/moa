// Package exampletool provides a tool that injects few-shot examples into the
// LLM system instruction. This helps the model understand expected input/output
// patterns including tool calls.
package exampletool

import (
	"fmt"
	"strings"

	"google.golang.org/genai"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/tool"
)

// Example represents a single few-shot example with an input and expected outputs.
type Example struct {
	Input  *genai.Content   `json:"input"`
	Output []*genai.Content `json:"output"`
}

// Config for the example tool.
type Config struct {
	Examples []*Example
}

type exampleTool struct {
	examples []*Example
}

// New creates a new few-shot example tool.
func New(cfg Config) tool.Tool {
	return &exampleTool{examples: cfg.Examples}
}

func (t *exampleTool) Name() string        { return "example_tool" }
func (t *exampleTool) Description() string { return "Provides few-shot examples to the LLM." }
func (t *exampleTool) IsNative() bool      { return false }
func (t *exampleTool) IsLongRunning() bool { return false }

// ProcessRequest builds the example instruction and appends it to the system prompt.
func (t *exampleTool) ProcessRequest(req *model.LLMRequest) {
	if len(t.examples) == 0 {
		return
	}
	instruction := buildExamplesInstruction(t.examples)
	appendInstruction(req, instruction)
}

const (
	examplesIntro      = "<EXAMPLES>\nBegin few-shot\nThe following are examples of user queries and model responses using the available tools.\n\n"
	examplesEnd        = "End few-shot\n</EXAMPLES>"
	exampleStartFmt    = "EXAMPLE %d:\nBegin example\n"
	exampleEndStr      = "End example\n\n"
	userPrefix         = "[user]\n"
	modelPrefix        = "[model]\n"
	functionCallFmt    = "```tool_code\n%s(%s)\n```\n"
	functionRespFmt    = "```tool_outputs\n%v\n```\n"
)

func buildExamplesInstruction(examples []*Example) string {
	var sb strings.Builder
	sb.WriteString(examplesIntro)

	for i, example := range examples {
		fmt.Fprintf(&sb, exampleStartFmt, i+1)

		// Input
		sb.WriteString(userPrefix)
		if example.Input != nil {
			for _, part := range example.Input.Parts {
				if part.Text != "" {
					sb.WriteString(strings.ReplaceAll(part.Text, "End few-shot", "[PROTECTED]"))
					sb.WriteString("\n")
				}
			}
		}

		// Outputs
		prevRole := ""
		for _, content := range example.Output {
			role := modelPrefix
			if content.Role != "model" {
				role = userPrefix
			}
			if role != prevRole {
				sb.WriteString(role)
			}
			prevRole = role

			for _, part := range content.Parts {
				if part.FunctionCall != nil {
					var args []string
					for k, v := range part.FunctionCall.Args {
						if s, ok := v.(string); ok {
							args = append(args, fmt.Sprintf("%s='%s'", k, s))
						} else {
							args = append(args, fmt.Sprintf("%s=%v", k, v))
						}
					}
					fmt.Fprintf(&sb, functionCallFmt, part.FunctionCall.Name, strings.Join(args, ", "))
				} else if part.FunctionResponse != nil {
					fmt.Fprintf(&sb, functionRespFmt, part.FunctionResponse)
				} else if part.Text != "" {
					sb.WriteString(strings.ReplaceAll(part.Text, "End few-shot", "[PROTECTED]"))
					sb.WriteString("\n")
				}
			}
		}
		sb.WriteString(exampleEndStr)
	}
	sb.WriteString(examplesEnd)
	return sb.String()
}

func appendInstruction(req *model.LLMRequest, text string) {
	if req.SystemInstruction == nil {
		req.SystemInstruction = &genai.Content{
			Role:  "system",
			Parts: []*genai.Part{{Text: text}},
		}
		return
	}
	existing := ""
	for _, p := range req.SystemInstruction.Parts {
		existing += p.Text
	}
	req.SystemInstruction = &genai.Content{
		Role:  "system",
		Parts: []*genai.Part{{Text: strings.TrimSpace(existing) + "\n\n" + text}},
	}
}

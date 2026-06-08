// Package guardrail provides input/output validation for moa agents.
//
// Guardrails protect against prompt injection, PII leaks, and unwanted content.
// They run as a plugin that intercepts user messages (input) and model
// responses (output) before they reach the next stage.
//
// Usage:
//
//	plug := guardrail.NewPlugin(
//	    guardrail.PromptInjection(),
//	    guardrail.PII(guardrail.PIIConfig{Sanitize: []guardrail.PIIType{guardrail.Email}}),
//	)
//	runner, _ := runner.New(runner.Config{Plugins: []*plugin.Plugin{plug}})
package guardrail

import "context"

// Guardrail validates input and/or output text.
type Guardrail interface {
	// Name returns the guardrail name (used in logging/errors).
	Name() string

	// ValidateInput checks user input before it reaches the model.
	// Return nil Result to skip (pass through).
	ValidateInput(ctx context.Context, text string) *Result

	// ValidateOutput checks model output before it reaches the user.
	// Return nil Result to skip (pass through).
	ValidateOutput(ctx context.Context, text string) *Result
}

// Result is the outcome of a guardrail check.
type Result struct {
	// Passed is true if the text passed validation.
	Passed bool
	// Action is what to do when validation fails.
	Action Action
	// Reason explains why the text was flagged.
	Reason string
	// Guardrail is the name of the guardrail that flagged the text.
	Guardrail string
	// Sanitized is the cleaned text (only when Action == ActionSanitize).
	Sanitized string
}

// Action determines what happens when a guardrail fails.
type Action string

const (
	// ActionBlock rejects the text entirely.
	ActionBlock Action = "block"
	// ActionWarn allows the text but logs a warning.
	ActionWarn Action = "warn"
	// ActionSanitize replaces the flagged content and continues.
	ActionSanitize Action = "sanitize"
)

// Validate runs a text through multiple guardrails.
// Returns the first non-passing result, or nil if all pass.
func Validate(ctx context.Context, text string, guardrails []Guardrail, isInput bool) *Result {
	for _, g := range guardrails {
		var r *Result
		if isInput {
			r = g.ValidateInput(ctx, text)
		} else {
			r = g.ValidateOutput(ctx, text)
		}
		if r != nil && !r.Passed {
			r.Guardrail = g.Name()
			return r
		}
	}
	return nil
}

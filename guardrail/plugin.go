package guardrail

import (
	"fmt"
	"log/slog"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/session"
)

// NewPlugin creates a plugin that applies guardrails to input and output.
//
// Input guardrails run on user messages before they reach the model.
// Output guardrails run on model responses before they reach the user.
//
// Usage:
//
//	plug := guardrail.NewPlugin(
//	    guardrail.PromptInjection(),
//	    guardrail.PII(guardrail.PIIConfig{Sanitize: []guardrail.PIIType{guardrail.Email}}),
//	    guardrail.ContentPolicy(guardrail.ContentConfig{BlockPatterns: []string{"API_KEY"}}),
//	)
func NewPlugin(guardrails ...Guardrail) *plugin.Plugin {
	return &plugin.Plugin{
		Name: "guardrails",

		// Validate user input
		OnUserMessageCallback: func(ctx agent.CallbackContext, event *session.Event) (*session.Event, error) {
			if event == nil || event.Content == nil {
				return nil, nil
			}

			text := extractText(event.Content.Parts)
			if text == "" {
				return nil, nil
			}

			result := Validate(nil, text, guardrails, true)
			if result == nil {
				return nil, nil
			}

			switch result.Action {
			case ActionBlock:
				return nil, fmt.Errorf("[guardrail:%s] input blocked: %s", result.Guardrail, result.Reason)
			case ActionWarn:
				slog.Warn("guardrail input warning", "guardrail", result.Guardrail, "reason", result.Reason)
				return nil, nil
			case ActionSanitize:
				slog.Info("guardrail input sanitized", "guardrail", result.Guardrail, "reason", result.Reason)
				replaceEventText(event, result.Sanitized)
				return event, nil
			}

			return nil, nil
		},

		// Validate model output
		AfterModelCallback: func(ctx agent.CallbackContext, resp *model.LLMResponse) (*model.LLMResponse, error) {
			if resp == nil || resp.Content == nil {
				return nil, nil
			}

			text := extractText(resp.Content.Parts)
			if text == "" {
				return nil, nil
			}

			result := Validate(nil, text, guardrails, false)
			if result == nil {
				return nil, nil
			}

			switch result.Action {
			case ActionBlock:
				return nil, fmt.Errorf("[guardrail:%s] output blocked: %s", result.Guardrail, result.Reason)
			case ActionWarn:
				slog.Warn("guardrail output warning", "guardrail", result.Guardrail, "reason", result.Reason)
				return nil, nil
			case ActionSanitize:
				slog.Info("guardrail output sanitized", "guardrail", result.Guardrail, "reason", result.Reason)
				replaceContentText(resp.Content, result.Sanitized)
				return resp, nil
			}

			return nil, nil
		},
	}
}

// extractText concatenates all text parts from a genai content.
func extractText(parts []*genai.Part) string {
	var text string
	for _, p := range parts {
		if p.Text != "" {
			text += p.Text
		}
	}
	return text
}

// replaceEventText replaces all text parts with sanitized text.
func replaceEventText(event *session.Event, sanitized string) {
	if event.Content != nil {
		replaceContentText(event.Content, sanitized)
	}
}

// replaceContentText replaces the text content with sanitized version.
func replaceContentText(content *genai.Content, sanitized string) {
	// Replace the first text part, clear others
	replaced := false
	for _, p := range content.Parts {
		if p.Text != "" {
			if !replaced {
				p.Text = sanitized
				replaced = true
			} else {
				p.Text = ""
			}
		}
	}
}

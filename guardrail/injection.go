package guardrail

import (
	"context"
	"regexp"
	"strings"
)

// injectionGuardrail detects prompt injection attempts in user input.
type injectionGuardrail struct {
	patterns []*regexp.Regexp
}

// PromptInjection creates a guardrail that detects prompt injection attempts.
//
// Detects patterns like:
//   - "ignore previous instructions"
//   - "system: you are now..."
//   - Role hijacking ("act as", "pretend to be")
//   - Delimiter injection (---, ===, ```)
//   - Encoded instructions (base64 directives)
func PromptInjection() Guardrail {
	patterns := []string{
		// Direct instruction override
		`(?i)ignore\s+(all\s+)?(previous|prior|above|earlier)\s+(instructions?|prompts?|rules?|guidelines?)`,
		`(?i)disregard\s+(all\s+)?(previous|prior|above)\s+(instructions?|prompts?|rules?)`,
		`(?i)forget\s+(all\s+)?(previous|prior|your)\s+(instructions?|prompts?|rules?)`,

		// Role hijacking
		`(?i)(you\s+are\s+now|from\s+now\s+on\s+you\s+are|act\s+as\s+if\s+you\s+are)\s+[a-z]`,
		`(?i)^system\s*:\s*`,
		`(?i)\[\s*system\s*\]`,
		`(?i)<\s*system\s*>`,

		// Prompt leaking
		`(?i)(show|reveal|display|output|repeat|print)\s+(me\s+)?(your|the|system)\s+(system\s+)?(prompt|instructions?|rules?)`,
		`(?i)what\s+(are|is)\s+your\s+(system\s+)?(prompt|instructions?|rules?)`,

		// Delimiter injection (trying to create a new system block)
		`(?i)---+\s*(system|instruction|prompt|role)\s*---+`,
		`(?i)===+\s*(system|instruction|prompt|role)\s*===+`,

		// Jailbreak patterns
		`(?i)(do\s+anything\s+now|DAN\s+mode|jailbreak|bypass\s+filter)`,
		`(?i)pretend\s+(that\s+)?(you\s+)?(are|have)\s+(no|removed)\s+(restrictions?|filters?|rules?)`,
	}

	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		compiled = append(compiled, regexp.MustCompile(p))
	}

	return &injectionGuardrail{patterns: compiled}
}

func (g *injectionGuardrail) Name() string { return "prompt_injection" }

func (g *injectionGuardrail) ValidateInput(_ context.Context, text string) *Result {
	for _, p := range g.patterns {
		if p.MatchString(text) {
			return &Result{
				Passed: false,
				Action: ActionBlock,
				Reason: "Potential prompt injection detected: " + p.String(),
			}
		}
	}

	// Check for suspicious encoded content (base64 blocks that decode to instructions)
	if hasEncodedInstructions(text) {
		return &Result{
			Passed: false,
			Action: ActionWarn,
			Reason: "Suspicious encoded content detected",
		}
	}

	return nil
}

func (g *injectionGuardrail) ValidateOutput(_ context.Context, _ string) *Result {
	// Prompt injection is input-only
	return nil
}

// hasEncodedInstructions checks for base64-like blocks that might contain encoded instructions.
func hasEncodedInstructions(text string) bool {
	// Look for base64 blocks with system/ignore/instruction keywords
	b64Pattern := regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
	matches := b64Pattern.FindAllString(text, 5)

	for _, m := range matches {
		lower := strings.ToLower(m)
		if strings.Contains(lower, "aWdub3Jl") || // "ignore" in base64
			strings.Contains(lower, "c3lzdGVt") || // "system" in base64
			strings.Contains(lower, "aW5zdHJ1Y3") { // "instruct" in base64
			return true
		}
	}
	return false
}

package guardrail

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// ContentConfig configures the content policy guardrail.
type ContentConfig struct {
	// BlockPatterns blocks messages containing these patterns (case-insensitive).
	BlockPatterns []string
	// WarnPatterns warns on messages containing these patterns.
	WarnPatterns []string
	// MaxOutputLength blocks outputs exceeding this length. 0 = unlimited.
	MaxOutputLength int
}

type contentGuardrail struct {
	blockPatterns []*regexp.Regexp
	warnPatterns  []*regexp.Regexp
	maxOutput     int
}

// ContentPolicy creates a guardrail that enforces content policies.
//
// Example:
//
//	guardrail.ContentPolicy(guardrail.ContentConfig{
//	    BlockPatterns:   []string{"API_KEY", "SECRET"},
//	    MaxOutputLength: 50000,
//	})
func ContentPolicy(cfg ContentConfig) Guardrail {
	g := &contentGuardrail{maxOutput: cfg.MaxOutputLength}
	for _, p := range cfg.BlockPatterns {
		g.blockPatterns = append(g.blockPatterns, regexp.MustCompile(`(?i)`+regexp.QuoteMeta(p)))
	}
	for _, p := range cfg.WarnPatterns {
		g.warnPatterns = append(g.warnPatterns, regexp.MustCompile(`(?i)`+regexp.QuoteMeta(p)))
	}
	return g
}

func (g *contentGuardrail) Name() string { return "content_policy" }

func (g *contentGuardrail) ValidateInput(_ context.Context, text string) *Result {
	return g.checkPatterns(text)
}

func (g *contentGuardrail) ValidateOutput(_ context.Context, text string) *Result {
	// Check length
	if g.maxOutput > 0 && len(text) > g.maxOutput {
		return &Result{
			Passed: false,
			Action: ActionBlock,
			Reason: fmt.Sprintf("Output exceeds max length (%d > %d)", len(text), g.maxOutput),
		}
	}
	return g.checkPatterns(text)
}

func (g *contentGuardrail) checkPatterns(text string) *Result {
	for _, p := range g.blockPatterns {
		if p.MatchString(text) {
			return &Result{
				Passed: false,
				Action: ActionBlock,
				Reason: "Content blocked: matches pattern " + p.String(),
			}
		}
	}
	for _, p := range g.warnPatterns {
		if p.MatchString(text) {
			return &Result{
				Passed: false,
				Action: ActionWarn,
				Reason: "Content warning: matches pattern " + p.String(),
			}
		}
	}
	return nil
}

// --- Keywords guardrail ---

// KeywordsConfig configures the keyword filter.
type KeywordsConfig struct {
	// Blocked keywords (case-insensitive, whole word match).
	Blocked []string
	// Action to take when a keyword is found. Default: Block.
	Action Action
	// Replacement string for sanitize mode. Default: "[REDACTED]".
	Replacement string
}

type keywordGuardrail struct {
	patterns    []*regexp.Regexp
	keywords    []string
	action      Action
	replacement string
}

// Keywords creates a guardrail that blocks or sanitizes specific keywords.
//
// Example:
//
//	guardrail.Keywords(guardrail.KeywordsConfig{
//	    Blocked: []string{"competitor_name", "internal_project"},
//	    Action:  guardrail.ActionSanitize,
//	})
func Keywords(cfg KeywordsConfig) Guardrail {
	g := &keywordGuardrail{
		action:      cfg.Action,
		replacement: cfg.Replacement,
	}
	if g.action == "" {
		g.action = ActionBlock
	}
	if g.replacement == "" {
		g.replacement = "[REDACTED]"
	}
	for _, kw := range cfg.Blocked {
		pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(kw) + `\b`)
		g.patterns = append(g.patterns, pattern)
		g.keywords = append(g.keywords, kw)
	}
	return g
}

func (g *keywordGuardrail) Name() string { return "keywords" }

func (g *keywordGuardrail) ValidateInput(ctx context.Context, text string) *Result {
	return g.check(text)
}

func (g *keywordGuardrail) ValidateOutput(ctx context.Context, text string) *Result {
	return g.check(text)
}

func (g *keywordGuardrail) check(text string) *Result {
	var matched []string
	sanitized := text

	for i, p := range g.patterns {
		if p.MatchString(text) {
			matched = append(matched, g.keywords[i])
			if g.action == ActionSanitize {
				sanitized = p.ReplaceAllString(sanitized, g.replacement)
			}
		}
	}

	if len(matched) == 0 {
		return nil
	}

	return &Result{
		Passed:    false,
		Action:    g.action,
		Reason:    "Keywords detected: " + strings.Join(matched, ", "),
		Sanitized: sanitized,
	}
}

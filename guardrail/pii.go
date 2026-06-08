package guardrail

import (
	"context"
	"regexp"
	"strings"
)

// PIIType identifies a category of personally identifiable information.
type PIIType string

const (
	PIIEmail      PIIType = "email"
	PIIPhone      PIIType = "phone"
	PIISSN        PIIType = "ssn"
	PIICreditCard PIIType = "credit_card"
	PIIPrivateIP  PIIType = "private_ip"
)

// PIIConfig configures the PII guardrail.
type PIIConfig struct {
	// Block lists PII types that should block the message entirely.
	Block []PIIType
	// Sanitize lists PII types that should be redacted and passed through.
	Sanitize []PIIType
}

var piiPatterns = map[PIIType]*regexp.Regexp{
	PIIEmail:      regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
	PIIPhone:      regexp.MustCompile(`(?:\+\d{1,3}[\s\-]?)?\(?\d{2,4}\)?[\s\-]?\d{3,4}[\s\-]?\d{3,4}`),
	PIISSN:        regexp.MustCompile(`\b\d{3}[\-\s]?\d{2}[\-\s]?\d{4}\b`),
	PIICreditCard: regexp.MustCompile(`\b(?:\d{4}[\s\-]?){3}\d{4}\b`),
	PIIPrivateIP:  regexp.MustCompile(`\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3})\b`),
}

var piiReplacements = map[PIIType]string{
	PIIEmail:      "[EMAIL_REDACTED]",
	PIIPhone:      "[PHONE_REDACTED]",
	PIISSN:        "[SSN_REDACTED]",
	PIICreditCard: "[CARD_REDACTED]",
	PIIPrivateIP:  "[IP_REDACTED]",
}

type piiGuardrail struct {
	block    map[PIIType]bool
	sanitize map[PIIType]bool
}

// PII creates a guardrail that detects and optionally sanitizes PII.
//
// Example:
//
//	guardrail.PII(guardrail.PIIConfig{
//	    Block:    []guardrail.PIIType{guardrail.PIISSN, guardrail.PIICreditCard},
//	    Sanitize: []guardrail.PIIType{guardrail.PIIEmail, guardrail.PIIPhone},
//	})
func PII(cfg PIIConfig) Guardrail {
	g := &piiGuardrail{
		block:    make(map[PIIType]bool),
		sanitize: make(map[PIIType]bool),
	}
	for _, t := range cfg.Block {
		g.block[t] = true
	}
	for _, t := range cfg.Sanitize {
		g.sanitize[t] = true
	}
	return g
}

func (g *piiGuardrail) Name() string { return "pii" }

func (g *piiGuardrail) ValidateInput(ctx context.Context, text string) *Result {
	return g.validate(text)
}

func (g *piiGuardrail) ValidateOutput(ctx context.Context, text string) *Result {
	return g.validate(text)
}

func (g *piiGuardrail) validate(text string) *Result {
	// Check blocked types first
	for piiType := range g.block {
		pattern, ok := piiPatterns[piiType]
		if !ok {
			continue
		}
		if pattern.MatchString(text) {
			return &Result{
				Passed: false,
				Action: ActionBlock,
				Reason: "Blocked PII detected: " + string(piiType),
			}
		}
	}

	// Check sanitize types
	sanitized := text
	found := false
	var reasons []string

	for piiType := range g.sanitize {
		pattern, ok := piiPatterns[piiType]
		if !ok {
			continue
		}
		replacement, ok := piiReplacements[piiType]
		if !ok {
			continue
		}
		if pattern.MatchString(sanitized) {
			found = true
			reasons = append(reasons, string(piiType))
			sanitized = pattern.ReplaceAllString(sanitized, replacement)
		}
	}

	if found {
		return &Result{
			Passed:    false,
			Action:    ActionSanitize,
			Reason:    "PII sanitized: " + strings.Join(reasons, ", "),
			Sanitized: sanitized,
		}
	}

	return nil
}

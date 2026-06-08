package teacherescalation

import (
	"regexp"
	"strings"
)

// FailureType categorizes the kind of failure detected.
type FailureType int

const (
	// ToolError indicates the model's tool usage resulted in an error pattern.
	ToolError FailureType = iota
	// ReplyGiveUp indicates the model gave up or expressed inability.
	ReplyGiveUp
)

// Failure represents a detected failure in the model's output.
type Failure struct {
	Type    FailureType
	Pattern string // the pattern that matched
	Match   string // the actual matched text
}

// FailureDetector evaluates model outputs for signs of failure.
type FailureDetector struct {
	toolErrorPatterns   []*regexp.Regexp
	replyGiveUpPatterns []*regexp.Regexp
}

// DefaultToolErrorPatterns are regex patterns for common tool error indicators.
var DefaultToolErrorPatterns = []string{
	`(?i)^Unknown action`,
	`(?i)^Failed to`,
	`(?i)\bnot found\b`,
	`(?i)^Invalid`,
	`(?i)\berror:\s`,
	`(?i)^Error executing`,
	`(?i)command not found`,
}

// DefaultReplyGiveUpPatterns are regex patterns for model "give up" responses.
var DefaultReplyGiveUpPatterns = []string{
	`(?i)I don'?t have a tool`,
	`(?i)I can'?t do`,
	`(?i)I'?m not (sure|able)`,
	`(?i)I don'?t know how`,
	`(?i)unable to (complete|perform|execute)`,
	`(?i)I cannot`,
	`(?i)beyond my (capabilities|ability)`,
	`(?i)not (possible|supported)`,
}

// NewFailureDetector creates a detector from regex pattern strings.
// If toolPatterns or replyPatterns are nil, defaults are used.
func NewFailureDetector(toolPatterns, replyPatterns []string) *FailureDetector {
	if toolPatterns == nil {
		toolPatterns = DefaultToolErrorPatterns
	}
	if replyPatterns == nil {
		replyPatterns = DefaultReplyGiveUpPatterns
	}

	d := &FailureDetector{
		toolErrorPatterns:   make([]*regexp.Regexp, 0, len(toolPatterns)),
		replyGiveUpPatterns: make([]*regexp.Regexp, 0, len(replyPatterns)),
	}

	for _, p := range toolPatterns {
		if re, err := regexp.Compile(p); err == nil {
			d.toolErrorPatterns = append(d.toolErrorPatterns, re)
		}
	}
	for _, p := range replyPatterns {
		if re, err := regexp.Compile(p); err == nil {
			d.replyGiveUpPatterns = append(d.replyGiveUpPatterns, re)
		}
	}

	return d
}

// EvaluateToolResult checks a tool's output for error patterns.
func (d *FailureDetector) EvaluateToolResult(result string) *Failure {
	if result == "" {
		return nil
	}
	for _, re := range d.toolErrorPatterns {
		if loc := re.FindString(result); loc != "" {
			return &Failure{
				Type:    ToolError,
				Pattern: re.String(),
				Match:   loc,
			}
		}
	}
	return nil
}

// EvaluateReply checks the model's text reply for "give up" patterns.
func (d *FailureDetector) EvaluateReply(reply string) *Failure {
	if reply == "" {
		return nil
	}
	for _, re := range d.replyGiveUpPatterns {
		if loc := re.FindString(reply); loc != "" {
			return &Failure{
				Type:    ReplyGiveUp,
				Pattern: re.String(),
				Match:   loc,
			}
		}
	}
	return nil
}

// extractTextFromParts extracts all text content from genai Content parts.
func extractTextFromParts(parts []interface{ MarshalJSON() ([]byte, error) }) string {
	// This is a simplified version — the actual implementation works with genai.Part.
	return ""
}

// ExtractReplyText extracts all text parts from a model response, joining them.
func ExtractReplyText(texts []string) string {
	return strings.Join(texts, "\n")
}

package teacherescalation

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed teacher_prompt.md
var teacherPromptTemplate string

// TeacherRequest contains the context sent to the teacher model.
type TeacherRequest struct {
	OriginalQuery string `json:"original_query"`
	FailureReason string `json:"failure_reason"`
	Trace         string `json:"trace"`
}

// TeacherResponse is the expected response from the teacher model.
type TeacherResponse struct {
	Answer     string     `json:"answer"`
	Skill      *SkillData `json:"skill,omitempty"`
	Confidence float64    `json:"confidence"`
}

// SkillData represents a skill extracted by the teacher.
type SkillData struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Directive   string   `json:"directive"`
	Tags        []string `json:"tags,omitempty"`
}

// BuildTeacherPrompt constructs the prompt sent to the teacher model.
// The student trace is wrapped in untrusted markers if guardEnabled is true.
func BuildTeacherPrompt(req TeacherRequest, guardEnabled bool) string {
	trace := req.Trace
	if guardEnabled {
		trace = "<<<UNTRUSTED_TRACE_START>>>\n" + trace + "\n<<<UNTRUSTED_TRACE_END>>>"
	}

	prompt := strings.ReplaceAll(teacherPromptTemplate, "{{ORIGINAL_QUERY}}", req.OriginalQuery)
	prompt = strings.ReplaceAll(prompt, "{{FAILURE_REASON}}", req.FailureReason)
	prompt = strings.ReplaceAll(prompt, "{{TRACE}}", trace)

	return prompt
}

// ParseTeacherResponse attempts to extract a TeacherResponse from the teacher's output.
func ParseTeacherResponse(raw string) (*TeacherResponse, error) {
	// Try to find JSON in the response
	raw = strings.TrimSpace(raw)

	// Try direct JSON parse
	var resp TeacherResponse
	if err := json.Unmarshal([]byte(raw), &resp); err == nil {
		return &resp, nil
	}

	// Try to extract JSON from markdown code block
	if idx := strings.Index(raw, "```json"); idx != -1 {
		start := idx + 7
		end := strings.Index(raw[start:], "```")
		if end != -1 {
			jsonStr := strings.TrimSpace(raw[start : start+end])
			if err := json.Unmarshal([]byte(jsonStr), &resp); err == nil {
				return &resp, nil
			}
		}
	}

	// Fallback: treat entire response as the answer
	return &TeacherResponse{
		Answer:     raw,
		Confidence: 0.5, // moderate confidence for unstructured responses
	}, nil
}

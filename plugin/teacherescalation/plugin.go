// Package teacherescalation provides a plugin that detects when a local model fails
// and escalates to a more capable "teacher" model.
//
// The teacher model answers the query and optionally generates a reusable skill
// that is persisted to the skill registry, so the local model can handle similar
// tasks in future sessions without escalation.
//
// Inspired by Odysseus's Teacher Escalation pattern.
//
// Usage:
//
//	escalation := teacherescalation.New(teacherescalation.Config{
//	    TeacherModel: teacherLLM,
//	    SkillRegistry: skillRegistry,
//	})
//	runner, _ := runner.New(runner.Config{
//	    Agent:   myAgent,
//	    Plugins: []*plugin.Plugin{escalation},
//	})
package teacherescalation

import (
	"context"
	"fmt"
	"strings"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/skill"

	"google.golang.org/genai"
)

// Config configures the teacher escalation plugin.
type Config struct {
	// TeacherModel is the SOTA cloud model used for escalation.
	TeacherModel model.LLM

	// Failure detection patterns. If nil, defaults are used.
	ReplyGiveUpPatterns []string

	// SkillRegistry for persisting learned skills. If nil, skills are not saved.
	SkillRegistry *skill.Registry

	// MinConfidence is the minimum confidence score for a skill to be persisted.
	// Default: 0.6.
	MinConfidence float64

	// UntrustedTraceGuard wraps the student trace in <<<UNTRUSTED_TRACE>>> markers
	// to prevent prompt injection from the student's output. Default: true.
	UntrustedTraceGuard *bool
}

// New creates a teacher escalation plugin.
func New(cfg Config) *plugin.Plugin {
	if cfg.MinConfidence <= 0 {
		cfg.MinConfidence = 0.6
	}
	guardEnabled := true
	if cfg.UntrustedTraceGuard != nil {
		guardEnabled = *cfg.UntrustedTraceGuard
	}

	detector := NewFailureDetector(nil, cfg.ReplyGiveUpPatterns)

	return &plugin.Plugin{
		Name: "teacher-escalation",

		AfterModelCallback: func(ctx agent.CallbackContext, resp *model.LLMResponse) (*model.LLMResponse, error) {
			return afterModel(ctx, resp, &cfg, detector, guardEnabled)
		},
	}
}

// afterModel evaluates the model's reply for signs of failure.
// If a failure is detected, escalates to the teacher model.
func afterModel(ctx agent.CallbackContext, resp *model.LLMResponse, cfg *Config, detector *FailureDetector, guardEnabled bool) (*model.LLMResponse, error) {
	if resp == nil || resp.Content == nil {
		return nil, nil
	}

	// Don't escalate on partial streaming responses.
	if resp.Partial {
		return nil, nil
	}

	// Extract text from the response.
	replyText := extractText(resp.Content)
	if replyText == "" {
		return nil, nil
	}

	// Check for failure patterns.
	failure := detector.EvaluateReply(replyText)
	if failure == nil {
		return nil, nil
	}

	// Teacher model is required for escalation.
	if cfg.TeacherModel == nil {
		return nil, nil
	}

	// Build the teacher prompt.
	originalQuery := extractUserQuery(ctx)
	teacherReq := TeacherRequest{
		OriginalQuery: originalQuery,
		FailureReason: fmt.Sprintf("Pattern: %s, Match: %q", failure.Pattern, failure.Match),
		Trace:         replyText,
	}
	prompt := BuildTeacherPrompt(teacherReq, guardEnabled)

	// Call the teacher model.
	teacherResp, err := callTeacher(ctx, cfg.TeacherModel, prompt)
	if err != nil {
		// If teacher also fails, let the original response through.
		return nil, nil
	}

	// Parse the teacher's response.
	parsed, err := ParseTeacherResponse(teacherResp)
	if err != nil || parsed.Answer == "" {
		return nil, nil
	}

	// Persist skill if applicable.
	if cfg.SkillRegistry != nil && parsed.Skill != nil && parsed.Confidence >= cfg.MinConfidence {
		s := &skill.SimpleSkill{
			SkillName:        parsed.Skill.Name,
			SkillDescription: parsed.Skill.Description,
			Directive:        parsed.Skill.Directive,
		}
		cfg.SkillRegistry.Register(s)
	}

	// Override the student's response with the teacher's answer.
	return &model.LLMResponse{
		Content: &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				genai.NewPartFromText(parsed.Answer),
			},
		},
		CustomMetadata: map[string]any{
			"escalated_to_teacher": true,
			"teacher_model":        cfg.TeacherModel.Name(),
			"failure_pattern":      failure.Pattern,
			"skill_generated":      parsed.Skill != nil,
		},
	}, nil
}

// callTeacher sends a prompt to the teacher model and returns the text response.
func callTeacher(ctx context.Context, teacherModel model.LLM, prompt string) (string, error) {
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role:  "user",
				Parts: []*genai.Part{genai.NewPartFromText(prompt)},
			},
		},
	}

	var result strings.Builder
	for resp, err := range teacherModel.GenerateContent(ctx, req, false) {
		if err != nil {
			return "", err
		}
		if resp != nil && resp.Content != nil {
			result.WriteString(extractText(resp.Content))
		}
	}

	if result.Len() == 0 {
		return "", fmt.Errorf("teacher model returned empty response")
	}

	return result.String(), nil
}

// extractText gets all text parts from a genai.Content.
func extractText(content *genai.Content) string {
	if content == nil || len(content.Parts) == 0 {
		return ""
	}
	var texts []string
	for _, part := range content.Parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// extractUserQuery tries to get the original user query from the callback context.
func extractUserQuery(ctx agent.CallbackContext) string {
	if ctx == nil {
		return ""
	}
	uc := ctx.UserContent()
	if uc == nil {
		return ""
	}
	return extractText(uc)
}

package teacherescalation

import (
	"context"
	"iter"
	"testing"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/skill"

	"google.golang.org/genai"
)

// --- Test helpers ---

type mockCallbackCtx struct {
	agent.CallbackContext
	userContent *genai.Content
}

func (m *mockCallbackCtx) AgentName() string                   { return "test-student" }
func (m *mockCallbackCtx) InvocationID() string                { return "inv-1" }
func (m *mockCallbackCtx) UserContent() *genai.Content         { return m.userContent }
func (m *mockCallbackCtx) ReadonlyState() session.ReadonlyState { return nil }

type mockTeacherLLM struct {
	name     string
	response string
	err      error
}

func (m *mockTeacherLLM) Name() string { return m.name }
func (m *mockTeacherLLM) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if m.err != nil {
			yield(nil, m.err)
			return
		}
		yield(&model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{genai.NewPartFromText(m.response)},
			},
		}, nil)
	}
}

func makeModelResp(text string) *model.LLMResponse {
	return &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{genai.NewPartFromText(text)},
		},
	}
}

// --- Detector Tests ---

func TestDetector_ToolErrors(t *testing.T) {
	d := NewFailureDetector(nil, nil)

	cases := []struct {
		input string
		match bool
	}{
		{"Unknown action: do_something", true},
		{"Failed to execute the command", true},
		{"file not found", true},
		{"Invalid argument: --flag", true},
		{"error: connection refused", true},
		{"Everything is fine", false},
		{"The result is 42", false},
		{"", false},
	}

	for _, tc := range cases {
		f := d.EvaluateToolResult(tc.input)
		if tc.match && f == nil {
			t.Errorf("expected match for %q", tc.input)
		}
		if !tc.match && f != nil {
			t.Errorf("unexpected match for %q: %v", tc.input, f)
		}
	}
}

func TestDetector_ReplyGiveUps(t *testing.T) {
	d := NewFailureDetector(nil, nil)

	cases := []struct {
		input string
		match bool
	}{
		{"I don't have a tool for that", true},
		{"I can't do this task", true},
		{"I'm not sure how to proceed", true},
		{"I'm not able to complete that", true},
		{"unable to complete the request", true},
		{"I cannot access that file", true},
		{"That is beyond my capabilities", true},
		{"Here is the answer: 42", false},
		{"I completed the task successfully", false},
		{"", false},
	}

	for _, tc := range cases {
		f := d.EvaluateReply(tc.input)
		if tc.match && f == nil {
			t.Errorf("expected match for %q", tc.input)
		}
		if !tc.match && f != nil {
			t.Errorf("unexpected match for %q: %v", tc.input, f)
		}
	}
}

func TestDetector_CustomPatterns(t *testing.T) {
	d := NewFailureDetector(
		[]string{`(?i)custom_error`},
		[]string{`(?i)I give up`},
	)

	// Custom tool error
	f := d.EvaluateToolResult("custom_error happened")
	if f == nil {
		t.Error("custom tool pattern should match")
	}

	// Default should not match
	f = d.EvaluateToolResult("Unknown action")
	if f != nil {
		t.Error("default tool pattern should not match with custom patterns")
	}

	// Custom reply
	f = d.EvaluateReply("I give up, this is too hard")
	if f == nil {
		t.Error("custom reply pattern should match")
	}
}

// --- Teacher Prompt Tests ---

func TestBuildTeacherPrompt_WithGuard(t *testing.T) {
	req := TeacherRequest{
		OriginalQuery: "What is 2+2?",
		FailureReason: "Model said I can't",
		Trace:         "Student: I can't do math",
	}

	prompt := BuildTeacherPrompt(req, true)

	if !contains(prompt, "<<<UNTRUSTED_TRACE_START>>>") {
		t.Error("guard markers should be present")
	}
	if !contains(prompt, "<<<UNTRUSTED_TRACE_END>>>") {
		t.Error("guard end marker should be present")
	}
	if !contains(prompt, "What is 2+2?") {
		t.Error("original query should be in prompt")
	}
}

func TestBuildTeacherPrompt_WithoutGuard(t *testing.T) {
	req := TeacherRequest{
		OriginalQuery: "test query",
		Trace:         "some trace",
	}

	prompt := BuildTeacherPrompt(req, false)

	if contains(prompt, "<<<UNTRUSTED_TRACE_START>>>") {
		t.Error("guard markers should NOT be present")
	}
	if !contains(prompt, "some trace") {
		t.Error("trace should be in prompt")
	}
}

func TestParseTeacherResponse_JSON(t *testing.T) {
	raw := `{"answer": "The answer is 4", "confidence": 0.95, "skill": {"name": "math_skill", "description": "Basic math", "directive": "You can do math"}}`

	resp, err := ParseTeacherResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Answer != "The answer is 4" {
		t.Errorf("wrong answer: %s", resp.Answer)
	}
	if resp.Confidence != 0.95 {
		t.Errorf("wrong confidence: %f", resp.Confidence)
	}
	if resp.Skill == nil {
		t.Fatal("skill should not be nil")
	}
	if resp.Skill.Name != "math_skill" {
		t.Errorf("wrong skill name: %s", resp.Skill.Name)
	}
}

func TestParseTeacherResponse_CodeBlock(t *testing.T) {
	raw := "Here is my response:\n```json\n{\"answer\": \"42\", \"confidence\": 0.8}\n```"

	resp, err := ParseTeacherResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Answer != "42" {
		t.Errorf("wrong answer: %s", resp.Answer)
	}
}

func TestParseTeacherResponse_PlainText(t *testing.T) {
	raw := "The answer is simply 42."

	resp, err := ParseTeacherResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Answer != "The answer is simply 42." {
		t.Errorf("wrong answer: %s", resp.Answer)
	}
	if resp.Confidence != 0.5 {
		t.Errorf("fallback confidence should be 0.5, got %f", resp.Confidence)
	}
}

// --- Plugin Integration Tests ---

func TestPlugin_NoEscalationOnSuccess(t *testing.T) {
	teacher := &mockTeacherLLM{name: "teacher", response: `{"answer": "should not see this"}`}
	p := New(Config{TeacherModel: teacher})

	ctx := &mockCallbackCtx{}
	resp := makeModelResp("Here is your answer: the capital of France is Paris.")

	overridden, err := p.AfterModelCallback(ctx, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overridden != nil {
		t.Error("successful response should not trigger escalation")
	}
}

func TestPlugin_EscalatesOnGiveUp(t *testing.T) {
	teacher := &mockTeacherLLM{
		name:     "gpt-4o",
		response: `{"answer": "The answer is 42", "confidence": 0.9}`,
	}
	p := New(Config{TeacherModel: teacher})

	ctx := &mockCallbackCtx{
		userContent: &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText("What is the meaning of life?")},
		},
	}
	resp := makeModelResp("I can't do this task, it's beyond my capabilities.")

	overridden, err := p.AfterModelCallback(ctx, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overridden == nil {
		t.Fatal("give-up response should trigger escalation")
	}

	text := extractText(overridden.Content)
	if text != "The answer is 42" {
		t.Errorf("expected teacher's answer, got: %s", text)
	}

	// Check metadata
	if overridden.CustomMetadata["escalated_to_teacher"] != true {
		t.Error("metadata should indicate escalation")
	}
	if overridden.CustomMetadata["teacher_model"] != "gpt-4o" {
		t.Errorf("wrong teacher model in metadata: %v", overridden.CustomMetadata["teacher_model"])
	}
}

func TestPlugin_SkillPersistence(t *testing.T) {
	teacher := &mockTeacherLLM{
		name: "teacher",
		response: `{
			"answer": "42",
			"confidence": 0.9,
			"skill": {
				"name": "math_basics",
				"description": "Basic arithmetic",
				"directive": "You can perform addition, subtraction, multiplication."
			}
		}`,
	}
	registry := skill.NewRegistry()
	p := New(Config{
		TeacherModel:  teacher,
		SkillRegistry: registry,
		MinConfidence: 0.6,
	})

	ctx := &mockCallbackCtx{}
	resp := makeModelResp("I'm not able to do math.")

	_, err := p.AfterModelCallback(ctx, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Skill should be registered
	s, ok := registry.Get("math_basics")
	if !ok {
		t.Fatal("skill should have been registered")
	}
	if s.Description() != "Basic arithmetic" {
		t.Errorf("wrong skill description: %s", s.Description())
	}
	if s.SystemDirective() != "You can perform addition, subtraction, multiplication." {
		t.Errorf("wrong skill directive: %s", s.SystemDirective())
	}
}

func TestPlugin_SkillNotPersistedBelowConfidence(t *testing.T) {
	teacher := &mockTeacherLLM{
		name: "teacher",
		response: `{
			"answer": "42",
			"confidence": 0.3,
			"skill": {"name": "low_conf_skill", "description": "x", "directive": "y"}
		}`,
	}
	registry := skill.NewRegistry()
	p := New(Config{
		TeacherModel:  teacher,
		SkillRegistry: registry,
		MinConfidence: 0.6,
	})

	ctx := &mockCallbackCtx{}
	resp := makeModelResp("I cannot do this.")

	p.AfterModelCallback(ctx, resp)

	_, ok := registry.Get("low_conf_skill")
	if ok {
		t.Error("low confidence skill should NOT be registered")
	}
}

func TestPlugin_NilTeacherSkipsEscalation(t *testing.T) {
	p := New(Config{TeacherModel: nil})

	ctx := &mockCallbackCtx{}
	resp := makeModelResp("I can't do this.")

	overridden, err := p.AfterModelCallback(ctx, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overridden != nil {
		t.Error("no teacher model = no escalation")
	}
}

func TestPlugin_PartialResponseSkipped(t *testing.T) {
	teacher := &mockTeacherLLM{name: "teacher", response: `{"answer": "x"}`}
	p := New(Config{TeacherModel: teacher})

	ctx := &mockCallbackCtx{}
	resp := makeModelResp("I can't do this")
	resp.Partial = true

	overridden, err := p.AfterModelCallback(ctx, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overridden != nil {
		t.Error("partial responses should not trigger escalation")
	}
}

func TestPlugin_NilResponseSafe(t *testing.T) {
	p := New(Config{TeacherModel: &mockTeacherLLM{name: "t"}})

	ctx := &mockCallbackCtx{}
	overridden, err := p.AfterModelCallback(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overridden != nil {
		t.Error("nil response should be safe")
	}
}

func TestExtractText(t *testing.T) {
	content := &genai.Content{
		Parts: []*genai.Part{
			genai.NewPartFromText("Hello "),
			genai.NewPartFromText("World"),
		},
	}
	text := extractText(content)
	if text != "Hello \nWorld" {
		t.Errorf("expected 'Hello \\nWorld', got %q", text)
	}
}

func TestExtractText_Nil(t *testing.T) {
	if extractText(nil) != "" {
		t.Error("nil content should return empty string")
	}
	if extractText(&genai.Content{}) != "" {
		t.Error("empty content should return empty string")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsHelper(s, substr)
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

var _ model.LLM = (*mockTeacherLLM)(nil)

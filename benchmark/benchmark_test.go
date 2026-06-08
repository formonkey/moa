package benchmark

import (
	"context"
	"iter"
	"sync"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/llmagent"
	"github.com/formonkey/moa/guardrail"
	"github.com/formonkey/moa/internal/testutil"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/runner"
	"github.com/formonkey/moa/security"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/toolbox"
)

// --- Mock model for benchmarks ---

type benchModel struct {
	functionCalls []*genai.FunctionCall
	response      string
	callCount     int
}

func (m *benchModel) Name() string { return "bench" }

func (m *benchModel) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.callCount++

		if m.callCount == 1 && len(m.functionCalls) > 0 {
			parts := make([]*genai.Part, len(m.functionCalls))
			for i, fc := range m.functionCalls {
				parts[i] = &genai.Part{FunctionCall: fc}
			}
			yield(&model.LLMResponse{
				Content:      &genai.Content{Role: "model", Parts: parts},
				ModelVersion: "bench",
			}, nil)
			return
		}

		yield(&model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{{Text: m.response}},
			},
			ModelVersion: "bench",
			FinishReason: "STOP",
		}, nil)
	}
}

// --- Mock tool ---

type benchTool struct {
	name string
}

func (t *benchTool) Name() string        { return t.name }
func (t *benchTool) Description() string { return t.name }
func (t *benchTool) IsNative() bool      { return false }
func (t *benchTool) IsLongRunning() bool { return false }
func (t *benchTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:       t.name,
		Parameters: &genai.Schema{Type: "OBJECT"},
	}
}
func (t *benchTool) Execute(_ context.Context, _ map[string]any) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

var _ tool.RunnableTool = (*benchTool)(nil)

// --- Benchmarks ---

// BenchmarkSimpleRun measures the overhead of a simple agent run with no tools.
func BenchmarkSimpleRun(b *testing.B) {
	for b.Loop() {
		m := &benchModel{response: "Hello!"}
		a, _ := llmagent.New(llmagent.Config{
			Name:  "bench",
			Model: m,
		})
		_, sess := testutil.NewTestSession(b)
		ctx := testutil.NewTestContext(a, sess)
		for range a.Run(ctx) {
		}
	}
}

// BenchmarkToolExecution measures overhead of 1 tool call cycle.
func BenchmarkToolExecution(b *testing.B) {
	for b.Loop() {
		m := &benchModel{
			functionCalls: []*genai.FunctionCall{{Name: "t1", Args: map[string]any{}}},
			response:      "done",
		}
		a, _ := llmagent.New(llmagent.Config{
			Name:  "bench",
			Model: m,
			Tools: []tool.Tool{&benchTool{name: "t1"}},
		})
		_, sess := testutil.NewTestSession(b)
		ctx := testutil.NewTestContext(a, sess)
		for range a.Run(ctx) {
		}
	}
}

// BenchmarkRunnerRun measures full runner overhead.
func BenchmarkRunnerRun(b *testing.B) {
	for b.Loop() {
		a := testutil.MockAgent("bench", "ok")
		svc := session.InMemoryService()
		r, _ := runner.New(runner.Config{
			AppName:           "bench",
			Agent:             a,
			SessionService:    svc,
			AutoCreateSession: true,
		})
		msg := genai.NewContentFromText("hi", "user")
		for range r.Run(context.Background(), "u", "s", msg, agent.RunConfig{}) {
		}
		r.Close()
	}
}

// BenchmarkToolbox measures toolbox discover + activate + execute.
func BenchmarkToolbox(b *testing.B) {
	for b.Loop() {
		tb := toolbox.New()
		tb.Register("cat", "Category", &benchTool{name: "t1"})

		m := &benchModel{
			functionCalls: []*genai.FunctionCall{{
				Name: "discover_tools",
				Args: map[string]any{"category": "cat"},
			}},
			response: "done",
		}
		a, _ := llmagent.New(llmagent.Config{
			Name:     "bench",
			Model:    m,
			Toolsets: []tool.Toolset{tb},
		})
		_, sess := testutil.NewTestSession(b)
		ctx := testutil.NewTestContext(a, sess)
		for range a.Run(ctx) {
		}
	}
}

// BenchmarkSecurityCheck measures security policy check overhead.
func BenchmarkSecurityCheck(b *testing.B) {
	policy := security.Standard("/tmp/project")
	for b.Loop() {
		policy.Enforce("run_command", map[string]any{"command": "go test ./..."})
	}
}

// BenchmarkGuardrailPII measures PII detection/sanitization overhead.
func BenchmarkGuardrailPII(b *testing.B) {
	g := guardrail.PII(guardrail.PIIConfig{
		Sanitize: []guardrail.PIIType{guardrail.PIIEmail, guardrail.PIIPhone, guardrail.PIISSN},
	})
	text := "Contact john@example.com or call 555-123-4567 for SSN 123-45-6789"
	for b.Loop() {
		g.ValidateOutput(nil, text)
	}
}

// BenchmarkGuardrailInjection measures injection detection overhead.
func BenchmarkGuardrailInjection(b *testing.B) {
	g := guardrail.PromptInjection()
	text := "Please help me write a function that processes user input safely"
	for b.Loop() {
		g.ValidateInput(nil, text)
	}
}

// BenchmarkConcurrent10 measures 10 concurrent session runs.
func BenchmarkConcurrent10(b *testing.B) {
	for b.Loop() {
		a := testutil.MockAgent("bench", "ok")
		svc := session.InMemoryService()
		r, _ := runner.New(runner.Config{
			AppName:           "bench",
			Agent:             a,
			SessionService:    svc,
			AutoCreateSession: true,
		})

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				msg := genai.NewContentFromText("hi", "user")
				for range r.Run(context.Background(), "u", string(rune('A'+id)), msg, agent.RunConfig{}) {
				}
			}(i)
		}
		wg.Wait()
		r.Close()
	}
}

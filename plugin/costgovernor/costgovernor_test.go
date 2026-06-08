package costgovernor

import (
	"testing"
	"time"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/tool"
	"google.golang.org/genai"
)

// --- Test helpers ---

type mockCallbackCtx struct{ agent.CallbackContext }

func (m *mockCallbackCtx) AgentName() string   { return "test-agent" }
func (m *mockCallbackCtx) InvocationID() string { return "inv-1" }

type mockTool struct{ name string }

func (m *mockTool) Name() string                { return m.name }
func (m *mockTool) Description() string         { return "" }
func (m *mockTool) Schema() *genai.Schema       { return nil }
func (m *mockTool) IsLongRunning() bool         { return false }
func (m *mockTool) IsNative() bool              { return false }

func makeResponse(input, output int32) *model.LLMResponse {
	return &model.LLMResponse{
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     input,
			CandidatesTokenCount: output,
		},
	}
}

// --- Tests ---

func TestTokenBudgetExhausted(t *testing.T) {
	p := New(Config{
		MaxTotalTokens:      1000,
		PricePerInputToken:  0.00000015,
		PricePerOutputToken: 0.0000006,
	})

	ctx := &mockCallbackCtx{}

	// Simulate usage: 600 input + 500 output = 1100 > 1000
	resp := makeResponse(600, 500)
	_, err := p.AfterModelCallback(ctx, resp)
	if err != nil {
		t.Fatalf("first call should not error: %v", err)
	}

	// Next call should be blocked by BeforeModel
	_, err = p.BeforeModelCallback(ctx, &model.LLMRequest{})
	if err == nil {
		t.Fatal("expected ErrBudgetExhausted, got nil")
	}
	if err != ErrBudgetExhausted {
		t.Fatalf("expected ErrBudgetExhausted, got %v", err)
	}
}

func TestDollarBudgetExhausted(t *testing.T) {
	p := New(Config{
		MaxDollarBudget:     0.001, // $0.001
		PricePerInputToken:  0.001, // $0.001 per token (exaggerated for testing)
		PricePerOutputToken: 0.001,
	})

	ctx := &mockCallbackCtx{}

	// 1 input token + 1 output token = $0.002 > $0.001
	resp := makeResponse(1, 1)
	_, err := p.AfterModelCallback(ctx, resp)
	if err != nil {
		t.Fatalf("first call should not error: %v", err)
	}

	// Next call should be blocked
	_, err = p.BeforeModelCallback(ctx, &model.LLMRequest{})
	if err == nil {
		t.Fatal("expected ErrDollarBudget, got nil")
	}
	if err != ErrDollarBudget {
		t.Fatalf("expected ErrDollarBudget, got %v", err)
	}
}

func TestToolCapExceeded(t *testing.T) {
	p := New(Config{
		ToolCallCaps: map[string]int{
			"run_command": 2,
		},
	})

	ctx := &mockCallbackCtx{}
	runCmd := &mockTool{name: "run_command"}
	otherTool := &mockTool{name: "read_file"}

	// First 2 calls OK
	for i := 0; i < 2; i++ {
		_, err := p.BeforeToolCallback(ctx, runCmd, nil)
		if err != nil {
			t.Fatalf("call %d should not error: %v", i+1, err)
		}
	}

	// Third call should fail
	_, err := p.BeforeToolCallback(ctx, runCmd, nil)
	if err == nil {
		t.Fatal("expected ErrToolCapExceeded, got nil")
	}

	// Other tool should still work
	_, err = p.BeforeToolCallback(ctx, otherTool, nil)
	if err != nil {
		t.Fatalf("other tool should not be capped: %v", err)
	}
}

func TestVelocityLimit(t *testing.T) {
	p := New(Config{
		MaxDollarsPerMinute: 0.003, // $0.003/min
		PricePerInputToken:  0.001,
		PricePerOutputToken: 0.001,
	})

	ctx := &mockCallbackCtx{}

	// First call: 1+1 = $0.002 (under $0.003 limit)
	resp := makeResponse(1, 1)
	_, err := p.AfterModelCallback(ctx, resp)
	if err != nil {
		t.Fatalf("first call should not error: %v", err)
	}

	// Second call: cumulative $0.004 > $0.003/min limit
	_, err = p.AfterModelCallback(ctx, resp)
	if err == nil {
		t.Fatal("expected ErrVelocityLimit, got nil")
	}
	if err != ErrVelocityLimit {
		t.Fatalf("expected ErrVelocityLimit, got %v", err)
	}
}

func TestHappyPath(t *testing.T) {
	p := New(Config{
		MaxTotalTokens:      10000,
		MaxDollarBudget:     1.00,
		PricePerInputToken:  0.00000015,
		PricePerOutputToken: 0.0000006,
		ToolCallCaps: map[string]int{
			"run_command": 50,
		},
	})

	ctx := &mockCallbackCtx{}
	runCmd := &mockTool{name: "run_command"}

	// 10 LLM calls with 100 tokens each = 2000 total (well under 10000)
	for i := 0; i < 10; i++ {
		_, err := p.BeforeModelCallback(ctx, &model.LLMRequest{})
		if err != nil {
			t.Fatalf("iteration %d BeforeModel error: %v", i, err)
		}
		_, err = p.AfterModelCallback(ctx, makeResponse(100, 100))
		if err != nil {
			t.Fatalf("iteration %d AfterModel error: %v", i, err)
		}
	}

	// 5 tool calls (well under 50 cap)
	for i := 0; i < 5; i++ {
		_, err := p.BeforeToolCallback(ctx, runCmd, nil)
		if err != nil {
			t.Fatalf("tool call %d error: %v", i, err)
		}
	}
}

func TestNilUsageMetadata(t *testing.T) {
	p := New(Config{
		MaxTotalTokens:      100,
		PricePerInputToken:  0.001,
		PricePerOutputToken: 0.001,
	})

	ctx := &mockCallbackCtx{}

	// Nil response
	_, err := p.AfterModelCallback(ctx, nil)
	if err != nil {
		t.Fatalf("nil response should not error: %v", err)
	}

	// Response with nil usage
	_, err = p.AfterModelCallback(ctx, &model.LLMResponse{})
	if err != nil {
		t.Fatalf("nil usage should not error: %v", err)
	}
}

func TestNoLimitsConfigured(t *testing.T) {
	p := New(Config{}) // All zeros = no limits

	ctx := &mockCallbackCtx{}
	runCmd := &mockTool{name: "anything"}

	_, err := p.BeforeModelCallback(ctx, &model.LLMRequest{})
	if err != nil {
		t.Fatalf("no limits should not block: %v", err)
	}

	_, err = p.AfterModelCallback(ctx, makeResponse(999999, 999999))
	if err != nil {
		t.Fatalf("no limits should not block: %v", err)
	}

	_, err = p.BeforeToolCallback(ctx, runCmd, nil)
	if err != nil {
		t.Fatalf("no limits should not block: %v", err)
	}
}

func TestVelocityPrunesOldEntries(t *testing.T) {
	cfg := Config{
		MaxDollarsPerMinute: 0.01,
		PricePerInputToken:  0.001,
		PricePerOutputToken: 0.001,
	}

	s := &state{
		toolCalls: make(map[string]int),
		spendLog: []spendEntry{
			{at: time.Now().Add(-2 * time.Minute), amount: 100.0}, // old, should be pruned
		},
	}

	velocity := s.velocityLastMinute()
	if velocity != 0 {
		t.Fatalf("old entries should be pruned, got velocity %.4f", velocity)
	}
	if len(s.spendLog) != 0 {
		t.Fatalf("expected spendLog pruned to 0, got %d", len(s.spendLog))
	}
	_ = cfg // used for context
}

func TestWallTimeExceeded(t *testing.T) {
	p := New(Config{
		MaxWallTime: 50 * time.Millisecond,
	})

	ctx := &mockCallbackCtx{}

	// First call starts the clock — should pass
	_, err := p.BeforeModelCallback(ctx, &model.LLMRequest{})
	if err != nil {
		t.Fatalf("first call should start the clock, not fail: %v", err)
	}

	// Second call within time — should pass
	_, err = p.BeforeModelCallback(ctx, &model.LLMRequest{})
	if err != nil {
		t.Fatalf("immediate second call should pass: %v", err)
	}

	// Wait past the wall time
	time.Sleep(60 * time.Millisecond)

	// Now should fail
	_, err = p.BeforeModelCallback(ctx, &model.LLMRequest{})
	if err == nil {
		t.Fatal("expected ErrWallTimeExceeded, got nil")
	}
	if err != ErrWallTimeExceeded {
		t.Fatalf("expected ErrWallTimeExceeded, got %v", err)
	}
}

func TestWallTimeDisabledByDefault(t *testing.T) {
	p := New(Config{}) // MaxWallTime = 0 → disabled

	ctx := &mockCallbackCtx{}

	for i := 0; i < 5; i++ {
		_, err := p.BeforeModelCallback(ctx, &model.LLMRequest{})
		if err != nil {
			t.Fatalf("call %d should pass with no wall time: %v", i, err)
		}
	}
}

// Verify mockTool implements tool.Tool.
var _ tool.Tool = (*mockTool)(nil)


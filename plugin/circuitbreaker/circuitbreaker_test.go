package circuitbreaker

import (
	"errors"
	"testing"
	"time"

	"github.com/formonkey/moa/agent"
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

var _ tool.Tool = (*mockTool)(nil)

// --- Tests ---

func TestIdenticalCallsTripsBreaker(t *testing.T) {
	p := New(Config{MaxIdenticalCalls: 3})
	ctx := &mockCallbackCtx{}
	cmd := &mockTool{name: "run_command"}
	args := map[string]any{"command": "ls -la"}

	// First 2 calls OK (threshold is 3)
	for i := 0; i < 2; i++ {
		_, err := p.BeforeToolCallback(ctx, cmd, args)
		if err != nil {
			t.Fatalf("call %d should pass: %v", i+1, err)
		}
		// Simulate success
		p.AfterToolCallback(ctx, cmd, args, nil)
	}

	// Third identical call should trip
	_, err := p.BeforeToolCallback(ctx, cmd, args)
	if err == nil {
		t.Fatal("expected ErrRepetitiveLoop, got nil")
	}
	if !errors.Is(err, ErrRepetitiveLoop) {
		t.Fatalf("expected ErrRepetitiveLoop, got %v", err)
	}
}

func TestDifferentArgsResetCounter(t *testing.T) {
	p := New(Config{MaxIdenticalCalls: 3})
	ctx := &mockCallbackCtx{}
	cmd := &mockTool{name: "run_command"}

	// 2 calls with same args
	for i := 0; i < 2; i++ {
		_, err := p.BeforeToolCallback(ctx, cmd, map[string]any{"command": "ls"})
		if err != nil {
			t.Fatalf("call %d should pass: %v", i+1, err)
		}
	}

	// Different args — resets counter
	_, err := p.BeforeToolCallback(ctx, cmd, map[string]any{"command": "pwd"})
	if err != nil {
		t.Fatalf("different args should not trip: %v", err)
	}

	// 2 more with same (new) args — still under threshold
	_, err = p.BeforeToolCallback(ctx, cmd, map[string]any{"command": "pwd"})
	if err != nil {
		t.Fatalf("should still be under threshold: %v", err)
	}
}

func TestConsecutiveFailsTripsBreaker(t *testing.T) {
	p := New(Config{MaxConsecutiveFails: 3})
	ctx := &mockCallbackCtx{}
	cmd := &mockTool{name: "run_command"}
	toolErr := errors.New("command not found")

	// 2 failures OK
	for i := 0; i < 2; i++ {
		_, err := p.OnToolErrorCallback(ctx, cmd, nil, toolErr)
		if err != nil {
			t.Fatalf("failure %d should pass through: %v", i+1, err)
		}
	}

	// Third failure should trip
	_, err := p.OnToolErrorCallback(ctx, cmd, nil, toolErr)
	if err == nil {
		t.Fatal("expected ErrConsecutiveFails, got nil")
	}
	if !errors.Is(err, ErrConsecutiveFails) {
		t.Fatalf("expected ErrConsecutiveFails, got %v", err)
	}
}

func TestSuccessResetsFailCounter(t *testing.T) {
	p := New(Config{MaxConsecutiveFails: 3})
	ctx := &mockCallbackCtx{}
	cmd := &mockTool{name: "run_command"}
	toolErr := errors.New("oops")

	// 2 failures
	for i := 0; i < 2; i++ {
		p.OnToolErrorCallback(ctx, cmd, nil, toolErr)
	}

	// Success resets counter
	p.AfterToolCallback(ctx, cmd, nil, nil)

	// 2 more failures — should not trip (counter was reset)
	for i := 0; i < 2; i++ {
		_, err := p.OnToolErrorCallback(ctx, cmd, nil, toolErr)
		if err != nil {
			t.Fatalf("after reset, failure %d should pass: %v", i+1, err)
		}
	}
}

func TestCooldownAndHalfOpen(t *testing.T) {
	p := New(Config{
		MaxIdenticalCalls: 2, // trip on 2nd identical call
		CooldownDuration:  50 * time.Millisecond,
		MaxProbes:         1,
	})
	ctx := &mockCallbackCtx{}
	cmd := &mockTool{name: "test"}
	args := map[string]any{"x": 1}

	// First call OK
	_, err := p.BeforeToolCallback(ctx, cmd, args)
	if err != nil {
		t.Fatalf("first call should pass: %v", err)
	}

	// Second identical call trips the breaker
	_, err = p.BeforeToolCallback(ctx, cmd, args)
	if err == nil {
		t.Fatal("should trip on second identical call")
	}

	// Immediately blocked
	_, err = p.BeforeToolCallback(ctx, cmd, map[string]any{"y": 2})
	if err == nil {
		t.Fatal("should be blocked while open")
	}
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	// Wait for cooldown
	time.Sleep(60 * time.Millisecond)

	// Now should allow a probe (half-open)
	_, err = p.BeforeToolCallback(ctx, cmd, map[string]any{"z": 3})
	if err != nil {
		t.Fatalf("probe should be allowed after cooldown: %v", err)
	}

	// Second probe should be blocked (MaxProbes=1)
	_, err = p.BeforeToolCallback(ctx, cmd, map[string]any{"w": 4})
	if err == nil {
		t.Fatal("second probe should be blocked")
	}
}

func TestHalfOpenSuccessCloses(t *testing.T) {
	p := New(Config{
		MaxIdenticalCalls: 2,
		CooldownDuration:  10 * time.Millisecond,
		MaxProbes:         1,
	})
	ctx := &mockCallbackCtx{}
	cmd := &mockTool{name: "test"}
	sameArgs := map[string]any{"a": 1}

	// First call OK, second trips
	p.BeforeToolCallback(ctx, cmd, sameArgs)
	p.BeforeToolCallback(ctx, cmd, sameArgs) // trips

	// Wait for cooldown
	time.Sleep(20 * time.Millisecond)

	// Probe call
	_, err := p.BeforeToolCallback(ctx, cmd, map[string]any{"b": 2})
	if err != nil {
		t.Fatalf("probe should be allowed: %v", err)
	}

	// Simulate success → closes circuit
	p.AfterToolCallback(ctx, cmd, nil, nil)

	// Should be closed now — allow normal calls
	_, err = p.BeforeToolCallback(ctx, cmd, map[string]any{"c": 3})
	if err != nil {
		t.Fatalf("circuit should be closed after successful probe: %v", err)
	}
}

func TestKillSwitch(t *testing.T) {
	ks := &AtomicKillSwitch{}
	p := New(Config{KillSwitch: ks})
	ctx := &mockCallbackCtx{}
	cmd := &mockTool{name: "test"}

	// Not tripped — should pass
	_, err := p.BeforeToolCallback(ctx, cmd, nil)
	if err != nil {
		t.Fatalf("should pass when kill switch is not tripped: %v", err)
	}

	// Trip it
	ks.Trip()

	// Now should be blocked
	_, err = p.BeforeToolCallback(ctx, cmd, nil)
	if err == nil {
		t.Fatal("expected ErrKillSwitchTripped, got nil")
	}
	if !errors.Is(err, ErrKillSwitchTripped) {
		t.Fatalf("expected ErrKillSwitchTripped, got %v", err)
	}

	// Reset
	ks.Reset()

	_, err = p.BeforeToolCallback(ctx, cmd, nil)
	if err != nil {
		t.Fatalf("should pass after reset: %v", err)
	}
}

func TestFileKillSwitch(t *testing.T) {
	exists := false
	fks := &FileKillSwitch{
		Path:    "/tmp/moa_kill_test",
		checkFn: func(_ string) bool { return exists },
	}

	if fks.IsTripped() {
		t.Fatal("should not be tripped initially")
	}

	exists = true
	if !fks.IsTripped() {
		t.Fatal("should be tripped when file exists")
	}
}

func TestDifferentToolFailsResetCounter(t *testing.T) {
	p := New(Config{MaxConsecutiveFails: 3})
	ctx := &mockCallbackCtx{}
	cmd1 := &mockTool{name: "tool_a"}
	cmd2 := &mockTool{name: "tool_b"}
	toolErr := errors.New("oops")

	// 2 failures on tool_a
	for i := 0; i < 2; i++ {
		p.OnToolErrorCallback(ctx, cmd1, nil, toolErr)
	}

	// Failure on tool_b — resets tool_a counter
	p.OnToolErrorCallback(ctx, cmd2, nil, toolErr)

	// 2 more failures on tool_a — should not trip (counter was reset by tool_b)
	for i := 0; i < 2; i++ {
		_, err := p.OnToolErrorCallback(ctx, cmd1, nil, toolErr)
		if err != nil {
			t.Fatalf("after tool switch, failure %d should pass: %v", i+1, err)
		}
	}
}

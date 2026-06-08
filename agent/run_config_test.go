package agent

import "testing"

func TestEffectiveInputBudget_AutoCompute8K(t *testing.T) {
	rc := &RunConfig{}
	// 8K model → 85% = 6963
	modelCtx := 8192
	got := rc.EffectiveInputBudget(modelCtx)
	want := int(float64(modelCtx) * 0.85)
	if got != want {
		t.Errorf("8K model: got %d, want %d", got, want)
	}
}

func TestEffectiveInputBudget_AutoCompute128K(t *testing.T) {
	rc := &RunConfig{}
	// 128K model → 85% = 108,800 (under 200K cap)
	modelCtx := 128_000
	got := rc.EffectiveInputBudget(modelCtx)
	want := int(float64(modelCtx) * 0.85)
	if got != want {
		t.Errorf("128K model: got %d, want %d", got, want)
	}
}

func TestEffectiveInputBudget_AutoComputeCappedAt200K(t *testing.T) {
	rc := &RunConfig{}
	// 1M model → 85% = 850K, capped at 200K
	got := rc.EffectiveInputBudget(1_000_000)
	if got != 200_000 {
		t.Errorf("1M model: got %d, want %d", got, 200_000)
	}
}

func TestEffectiveInputBudget_ExplicitOverride(t *testing.T) {
	rc := &RunConfig{
		InputTokenBudget:    50_000,
		InputTokenBudgetSet: true,
	}
	got := rc.EffectiveInputBudget(128_000)
	if got != 50_000 {
		t.Errorf("explicit 50K: got %d, want %d", got, 50_000)
	}
}

func TestEffectiveInputBudget_ExplicitClampedToModelWindow(t *testing.T) {
	rc := &RunConfig{
		InputTokenBudget:    100_000,
		InputTokenBudgetSet: true,
	}
	// User asked for 100K but model only supports 8K
	got := rc.EffectiveInputBudget(8192)
	if got != 8192 {
		t.Errorf("clamped to 8K: got %d, want %d", got, 8192)
	}
}

func TestEffectiveInputBudget_NilRunConfig(t *testing.T) {
	var rc *RunConfig
	modelCtx := 32_000
	got := rc.EffectiveInputBudget(modelCtx)
	want := int(float64(modelCtx) * 0.85)
	if got != want {
		t.Errorf("nil config 32K: got %d, want %d", got, want)
	}
}

func TestEffectiveInputBudget_ZeroModelContext(t *testing.T) {
	rc := &RunConfig{}
	// Unknown model context → return max budget
	got := rc.EffectiveInputBudget(0)
	if got != 200_000 {
		t.Errorf("zero context: got %d, want %d", got, 200_000)
	}
}

func TestEffectiveInputBudget_NegativeModelContext(t *testing.T) {
	rc := &RunConfig{}
	got := rc.EffectiveInputBudget(-1)
	if got != 200_000 {
		t.Errorf("negative context: got %d, want %d", got, 200_000)
	}
}

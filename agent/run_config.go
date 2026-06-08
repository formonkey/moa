package agent

// StreamingMode defines the streaming mode for agent execution.
type StreamingMode string

const (
	// StreamingModeNone indicates no streaming.
	StreamingModeNone StreamingMode = "none"
	// StreamingModeSSE enables server-sent events streaming.
	StreamingModeSSE StreamingMode = "sse"
)

// RunConfig controls runtime behavior of an agent.
type RunConfig struct {
	// StreamingMode defines the streaming mode.
	StreamingMode StreamingMode
	// If true, the runner will save each blob part of user input as an artifact.
	SaveInputBlobsAsArtifacts bool

	// InputTokenBudget is the maximum number of input tokens per model request.
	// When set to 0 and InputTokenBudgetSet is false, the budget is auto-computed
	// from the model's context window (85% of capacity, capped at 200K).
	// This is especially important for local models with small context windows
	// (e.g., 8K, 32K) where overloading the context degrades quality.
	InputTokenBudget int
	// InputTokenBudgetSet indicates whether InputTokenBudget was explicitly configured.
	// When false, EffectiveInputBudget auto-computes based on the model's context length.
	InputTokenBudgetSet bool
}

// EffectiveInputBudget returns the actual input token budget to use.
//
// If the user explicitly set InputTokenBudget, it is honored (clamped to modelContextLength).
// Otherwise, it is auto-computed as 85% of the model's context window, capped at 200,000 tokens.
//
// This enables automatic adaptation: a Qwen-8B with 8K context gets ~6.8K budget,
// while a Gemini with 1M context gets 200K (capped).
func (rc *RunConfig) EffectiveInputBudget(modelContextLength int) int {
	if rc == nil {
		return autoComputeBudget(modelContextLength)
	}
	if rc.InputTokenBudgetSet && rc.InputTokenBudget > 0 {
		if modelContextLength > 0 && rc.InputTokenBudget > modelContextLength {
			return modelContextLength
		}
		return rc.InputTokenBudget
	}
	return autoComputeBudget(modelContextLength)
}

const (
	defaultBudgetRatio = 0.85
	maxAutoBudget      = 200_000
)

func autoComputeBudget(modelContextLength int) int {
	if modelContextLength <= 0 {
		return maxAutoBudget
	}
	budget := int(float64(modelContextLength) * defaultBudgetRatio)
	if budget > maxAutoBudget {
		return maxAutoBudget
	}
	return budget
}

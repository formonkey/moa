package model

import (
	"context"
	"iter"

	"google.golang.org/genai"
)

// LLM defines the core language model interface for go-brain v2.
// We adopt an iterator-based response pattern to seamlessly support streaming,
// a pattern inspired by Google's ADK.
type LLM interface {
	Name() string
	GenerateContent(ctx context.Context, req *LLMRequest, stream bool) iter.Seq2[*LLMResponse, error]
}

// LLMRequest encapsulates a model generation request, including memory/context and tools.
type LLMRequest struct {
	Model             string
	Contents          []*genai.Content
	SystemInstruction *genai.Content
	Config            *genai.GenerateContentConfig

	// Tool definitions. Native tools will inject their schema here before generation.
	Tools []*genai.Tool
}

// LLMResponse represents a standardized response from any model provider.
type LLMResponse struct {
	Content           *genai.Content
	CitationMetadata  *genai.CitationMetadata
	GroundingMetadata *genai.GroundingMetadata
	UsageMetadata     *genai.GenerateContentResponseUsageMetadata
	CustomMetadata    map[string]any
	LogprobsResult    *genai.LogprobsResult
	ModelVersion      string
	FinishReason      genai.FinishReason
	AvgLogprobs       float64

	// Partial indicates whether the content is part of an unfinished stream.
	Partial bool
	// TurnComplete indicates the response from the model is complete (streaming only).
	TurnComplete bool
	// Interrupted indicates the LLM was interrupted during generation.
	Interrupted  bool
	ErrorCode    string
	ErrorMessage string
}

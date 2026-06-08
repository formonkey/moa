// Package groq provides a model adapter for Groq's ultra-fast inference API.
//
// Groq uses an OpenAI-compatible API format with the fastest inference speeds.
// Supported models: llama-3.3-70b-versatile, gemma2-9b-it, mixtral-8x7b-32768, etc.
package groq

import (
	"github.com/formonkey/moa/model/openai"
)

const defaultBaseURL = "https://api.groq.com/openai/v1"

// Config for the Groq adapter.
type Config struct {
	APIKey string
	Model  string // defaults to "llama-3.3-70b-versatile"
}

// NewClient creates a Groq adapter.
func NewClient(cfg Config) *openai.Client {
	m := cfg.Model
	if m == "" {
		m = "llama-3.3-70b-versatile"
	}
	return openai.NewClient(openai.Config{
		APIKey:  cfg.APIKey,
		BaseURL: defaultBaseURL,
		Model:   m,
	})
}

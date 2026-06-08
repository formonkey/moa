// Package deepseek provides a model adapter for DeepSeek's API.
//
// DeepSeek uses an OpenAI-compatible API format.
// Supported models: deepseek-chat, deepseek-reasoner.
package deepseek

import (
	"github.com/formonkey/moa/model/openai"
)

const defaultBaseURL = "https://api.deepseek.com/v1"

// Config for the DeepSeek adapter.
type Config struct {
	APIKey string
	Model  string // defaults to "deepseek-chat"
}

// NewClient creates a DeepSeek adapter.
func NewClient(cfg Config) *openai.Client {
	m := cfg.Model
	if m == "" {
		m = "deepseek-chat"
	}
	return openai.NewClient(openai.Config{
		APIKey:  cfg.APIKey,
		BaseURL: defaultBaseURL,
		Model:   m,
	})
}

// Package mistral provides a model adapter for Mistral AI's API.
//
// Mistral uses an OpenAI-compatible API format.
// Supported models: mistral-large-latest, mistral-small-latest, codestral-latest, etc.
package mistral

import (
	"github.com/formonkey/moa/model/openai"
)

const defaultBaseURL = "https://api.mistral.ai/v1"

// Config for the Mistral adapter.
type Config struct {
	APIKey string
	Model  string // defaults to "mistral-large-latest"
}

// NewClient creates a Mistral adapter.
func NewClient(cfg Config) *openai.Client {
	m := cfg.Model
	if m == "" {
		m = "mistral-large-latest"
	}
	return openai.NewClient(openai.Config{
		APIKey:  cfg.APIKey,
		BaseURL: defaultBaseURL,
		Model:   m,
	})
}

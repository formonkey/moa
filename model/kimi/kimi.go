// Package kimi provides a model adapter for Moonshot's Kimi API.
//
// Kimi uses an OpenAI-compatible API format.
// Supported models: moonshot-v1-8k, moonshot-v1-32k, moonshot-v1-128k.
package kimi

import (
	"github.com/formonkey/moa/model/openai"
)

const defaultBaseURL = "https://api.moonshot.cn/v1"

// Config for the Kimi adapter.
type Config struct {
	APIKey string
	Model  string // defaults to "moonshot-v1-128k"
}

// NewClient creates a Kimi (Moonshot) adapter.
func NewClient(cfg Config) *openai.Client {
	m := cfg.Model
	if m == "" {
		m = "moonshot-v1-128k"
	}
	return openai.NewClient(openai.Config{
		APIKey:  cfg.APIKey,
		BaseURL: defaultBaseURL,
		Model:   m,
	})
}

// Package openrouter provides a model adapter for OpenRouter's unified API.
//
// OpenRouter proxies requests to 200+ models from multiple providers.
// Use any model ID from https://openrouter.ai/models.
//
// Examples: "anthropic/claude-sonnet-4", "google/gemini-2.5-flash",
// "meta-llama/llama-3.3-70b-instruct", "qwen/qwen-2.5-72b-instruct".
package openrouter

import (
	"github.com/formonkey/moa/model/openai"
)

const defaultBaseURL = "https://openrouter.ai/api/v1"

// Config for the OpenRouter adapter.
type Config struct {
	APIKey string
	Model  string // any OpenRouter model ID
	// AppName for OpenRouter analytics (optional, recommended).
	AppName string
	// AppURL for OpenRouter referral tracking (optional).
	AppURL string
}

// NewClient creates an OpenRouter adapter.
func NewClient(cfg Config) *openai.Client {
	headers := make(map[string]string)
	if cfg.AppName != "" {
		headers["X-Title"] = cfg.AppName
	}
	if cfg.AppURL != "" {
		headers["HTTP-Referer"] = cfg.AppURL
	}

	return openai.NewClient(openai.Config{
		APIKey:  cfg.APIKey,
		BaseURL: defaultBaseURL,
		Model:   cfg.Model,
		Headers: headers,
	})
}

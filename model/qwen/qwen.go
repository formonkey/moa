// Package qwen provides a model adapter for Alibaba's Qwen (Tongyi Qianwen) API.
//
// Qwen uses an OpenAI-compatible API format via DashScope.
// Supported models: qwen-max, qwen-plus, qwen-turbo, qwen2.5-coder-32b, etc.
package qwen

import (
	"github.com/formonkey/moa/model/openai"
)

const defaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"

// Config for the Qwen adapter.
type Config struct {
	APIKey string
	Model  string // defaults to "qwen-max"
}

// NewClient creates a Qwen adapter.
func NewClient(cfg Config) *openai.Client {
	m := cfg.Model
	if m == "" {
		m = "qwen-max"
	}
	return openai.NewClient(openai.Config{
		APIKey:  cfg.APIKey,
		BaseURL: defaultBaseURL,
		Model:   m,
	})
}

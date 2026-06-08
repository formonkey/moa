// Package anthropic provides a model adapter for Anthropic's Claude API.
//
// Claude uses a different API format than OpenAI, so this adapter handles
// the conversion between genai types and Anthropic's Messages API format.
//
// Supported models: claude-sonnet-4-20250514, claude-3.5-haiku, etc.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"time"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/model/httputil"
	"google.golang.org/genai"
)

const defaultBaseURL = "https://api.anthropic.com/v1"
const defaultAPIVersion = "2023-06-01"
const defaultTimeout = 5 * time.Minute

// Config for the Anthropic adapter.
type Config struct {
	APIKey     string
	Model      string // e.g. "claude-sonnet-4-20250514"
	BaseURL    string // defaults to https://api.anthropic.com/v1
	APIVersion string // defaults to "2023-06-01"
	MaxTokens  int    // required by Anthropic, defaults to 4096
}

// NewClient creates an Anthropic Claude adapter.
func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = defaultAPIVersion
	}
	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-20250514"
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 4096
	}
	return &Client{
		apiKey:     cfg.APIKey,
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		model:      cfg.Model,
		apiVersion: cfg.APIVersion,
		maxTokens:  cfg.MaxTokens,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// Client implements model.LLM for Claude.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	apiVersion string
	maxTokens  int
	httpClient *http.Client
}

func (c *Client) Name() string { return c.model }

func (c *Client) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	targetModel := req.Model
	if targetModel == "" {
		targetModel = c.model
	}

	// Build Anthropic request
	payload := map[string]any{
		"model":      targetModel,
		"max_tokens": c.maxTokens,
		"stream":     stream,
	}

	// System message (Anthropic uses a top-level "system" field)
	if req.SystemInstruction != nil {
		sysText := extractText(req.SystemInstruction)
		if sysText != "" {
			payload["system"] = sysText
		}
	}

	// Messages
	messages := buildAnthropicMessages(req.Contents)
	payload["messages"] = messages

	// Tools
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			for _, fd := range t.FunctionDeclarations {
				tools = append(tools, map[string]any{
					"name":         fd.Name,
					"description":  fd.Description,
					"input_schema": schemaToJSON(fd.Parameters),
				})
			}
		}
		payload["tools"] = tools
	}

	// Config overrides
	if req.Config != nil {
		if req.Config.Temperature != nil {
			payload["temperature"] = *req.Config.Temperature
		}
		if req.Config.TopP != nil {
			payload["top_p"] = *req.Config.TopP
		}
		if req.Config.MaxOutputTokens != 0 {
			payload["max_tokens"] = req.Config.MaxOutputTokens
		}
		if req.Config.StopSequences != nil {
			payload["stop_sequences"] = req.Config.StopSequences
		}
	}

	return func(yield func(*model.LLMResponse, error) bool) {
		body, err := json.Marshal(payload)
		if err != nil {
			yield(nil, err)
			return
		}

		resp, err := httputil.DoWithRetry(c.httpClient, func() (*http.Request, error) {
			httpReq, err := http.NewRequestWithContext(ctx, "POST",
				fmt.Sprintf("%s/messages", c.baseURL), bytes.NewBuffer(body))
			if err != nil {
				return nil, err
			}
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("x-api-key", c.apiKey)
			httpReq.Header.Set("anthropic-version", c.apiVersion)
			return httpReq, nil
		})
		if err != nil {
			yield(nil, fmt.Errorf("anthropic: request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(resp.Body)
			yield(nil, fmt.Errorf("anthropic: API error %d: %s", resp.StatusCode, string(errBody)))
			return
		}

		if stream {
			c.handleStream(resp.Body, yield)
		} else {
			c.handleSync(resp.Body, yield)
		}
	}
}

// --- Sync ---

type anthropicResponse struct {
	ID         string             `json:"id"`
	Model      string             `json:"model"`
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      *anthropicUsage    `json:"usage"`
}

type anthropicContent struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (c *Client) handleSync(body io.Reader, yield func(*model.LLMResponse, error) bool) {
	var resp anthropicResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		yield(nil, fmt.Errorf("anthropic: decode error: %w", err))
		return
	}

	content := anthropicToGenai(resp.Content)

	yield(&model.LLMResponse{
		Content:      content,
		ModelVersion: resp.Model,
		Partial:      false,
	}, nil)
}

// --- Streaming ---

func (c *Client) handleStream(body io.Reader, yield func(*model.LLMResponse, error) bool) {
	scanner := bufio.NewScanner(body)

	// Track accumulated tool_use blocks
	var pendingToolUses []anthropicContent
	var currentToolUse *anthropicContent

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "event:") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == line {
			continue
		}

		var event struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type  string `json:"type"`
				ID    string `json:"id"`
				Name  string `json:"name"`
				Input any    `json:"input"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				currentToolUse = &anthropicContent{
					Type: "tool_use",
					ID:   event.ContentBlock.ID,
					Name: event.ContentBlock.Name,
				}
			}

		case "content_block_delta":
			if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				if !yield(&model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: event.Delta.Text}},
					},
					Partial: true,
				}, nil) {
					return
				}
			}
			if event.Delta.Type == "input_json_delta" && currentToolUse != nil {
				// Accumulate partial JSON for tool input
				if s, ok := currentToolUse.Input.(string); ok {
					currentToolUse.Input = s + event.Delta.PartialJSON
				} else {
					currentToolUse.Input = event.Delta.PartialJSON
				}
			}

		case "content_block_stop":
			if currentToolUse != nil {
				// Parse the accumulated JSON input
				if inputStr, ok := currentToolUse.Input.(string); ok && inputStr != "" {
					var parsed any
					if err := json.Unmarshal([]byte(inputStr), &parsed); err == nil {
						currentToolUse.Input = parsed
					}
				}
				pendingToolUses = append(pendingToolUses, *currentToolUse)
				currentToolUse = nil
			}

		case "message_stop":
			// Emit accumulated tool_use blocks as final response
			if len(pendingToolUses) > 0 {
				content := anthropicToGenai(pendingToolUses)
				yield(&model.LLMResponse{
					Content: content,
					Partial: false,
				}, nil)
			}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		yield(nil, err)
	}
}

// --- Helpers ---

func buildAnthropicMessages(contents []*genai.Content) []map[string]any {
	var messages []map[string]any
	for _, content := range contents {
		role := content.Role
		if role == "model" {
			role = "assistant"
		}

		var parts []map[string]any
		for _, p := range content.Parts {
			if p.Text != "" {
				parts = append(parts, map[string]any{"type": "text", "text": p.Text})
			}
			if p.FunctionCall != nil {
				inputJSON, _ := json.Marshal(p.FunctionCall.Args)
				parts = append(parts, map[string]any{
					"type":  "tool_use",
					"id":    p.FunctionCall.ID,
					"name":  p.FunctionCall.Name,
					"input": json.RawMessage(inputJSON),
				})
			}
			if p.FunctionResponse != nil {
				respJSON, _ := json.Marshal(p.FunctionResponse.Response)
				parts = append(parts, map[string]any{
					"type":        "tool_result",
					"tool_use_id": p.FunctionResponse.ID,
					"content":     string(respJSON),
				})
			}
		}

		if len(parts) == 1 && parts[0]["type"] == "text" {
			messages = append(messages, map[string]any{
				"role":    role,
				"content": parts[0]["text"],
			})
		} else if len(parts) > 0 {
			messages = append(messages, map[string]any{
				"role":    role,
				"content": parts,
			})
		}
	}
	return messages
}

func anthropicToGenai(content []anthropicContent) *genai.Content {
	c := &genai.Content{Role: "model"}
	for _, block := range content {
		switch block.Type {
		case "text":
			c.Parts = append(c.Parts, &genai.Part{Text: block.Text})
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			var argsMap map[string]any
			_ = json.Unmarshal(args, &argsMap)
			c.Parts = append(c.Parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					ID:   block.ID,
					Name: block.Name,
					Args: argsMap,
				},
			})
		}
	}
	return c
}

func extractText(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range c.Parts {
		sb.WriteString(p.Text)
	}
	return sb.String()
}

func schemaToJSON(s *genai.Schema) map[string]any {
	if s == nil {
		return map[string]any{"type": "object"}
	}
	result := map[string]any{"type": strings.ToLower(string(s.Type))}
	if s.Description != "" {
		result["description"] = s.Description
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any)
		for k, v := range s.Properties {
			props[k] = schemaToJSON(v)
		}
		result["properties"] = props
	}
	if len(s.Required) > 0 {
		result["required"] = s.Required
	}
	return result
}

var _ model.LLM = (*Client)(nil)

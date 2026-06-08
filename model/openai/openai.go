// Package openai provides an OpenAI-compatible model adapter for go-brain.
//
// This adapter works with any API that follows the OpenAI Chat Completions format,
// including: OpenAI (GPT-4o, o3), Azure OpenAI, Groq, Mistral, DeepSeek,
// Together AI, Fireworks AI, Perplexity, and any OpenRouter-proxied model.
//
// For providers with specific quirks, use the dedicated sub-packages
// (anthropic, qwen, kimi) which wrap this adapter with provider-specific logic.
package openai

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
	"google.golang.org/genai"
)

// Config for creating an OpenAI-compatible client.
type Config struct {
	// APIKey for authentication (Bearer token).
	APIKey string
	// BaseURL of the API (e.g. "https://api.openai.com/v1").
	BaseURL string
	// Model name (e.g. "gpt-4o", "llama-3.3-70b").
	Model string
	// Headers allows injecting custom HTTP headers (e.g. for OpenRouter).
	Headers map[string]string
}

// defaultTimeout for OpenAI-compatible API requests.
const defaultTimeout = 5 * time.Minute

// NewClient creates an OpenAI-compatible adapter.
func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	return &Client{
		apiKey:     cfg.APIKey,
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		model:      cfg.Model,
		headers:    cfg.Headers,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// Client implements model.LLM for OpenAI-compatible APIs.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	headers    map[string]string
	httpClient *http.Client
}

func (c *Client) Name() string { return c.model }

func (c *Client) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	targetModel := req.Model
	if targetModel == "" {
		targetModel = c.model
	}

	// Build OpenAI messages from genai.Content
	messages := buildMessages(req)

	// Build tools
	var tools []map[string]any
	for _, t := range req.Tools {
		for _, fd := range t.FunctionDeclarations {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        fd.Name,
					"description": fd.Description,
					"parameters":  schemaToJSON(fd.Parameters),
				},
			})
		}
	}

	payload := map[string]any{
		"model":    targetModel,
		"messages": messages,
		"stream":   stream,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}

	// Map genai config to OpenAI params
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
			payload["stop"] = req.Config.StopSequences
		}
		if req.Config.ResponseMIMEType == "application/json" {
			payload["response_format"] = map[string]string{"type": "json_object"}
		}
	}

	return func(yield func(*model.LLMResponse, error) bool) {
		body, err := json.Marshal(payload)
		if err != nil {
			yield(nil, fmt.Errorf("openai: failed to marshal request: %w", err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			fmt.Sprintf("%s/chat/completions", c.baseURL), bytes.NewBuffer(body))
		if err != nil {
			yield(nil, err)
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		for k, v := range c.headers {
			httpReq.Header.Set(k, v)
		}

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("openai: request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(resp.Body)
			yield(nil, fmt.Errorf("openai: API error %d: %s", resp.StatusCode, string(errBody)))
			return
		}

		if stream {
			c.handleStream(resp.Body, yield)
		} else {
			c.handleSync(resp.Body, yield)
		}
	}
}

// --- Sync response ---

type chatResponse struct {
	ID      string           `json:"id"`
	Model   string           `json:"model"`
	Choices []chatChoice     `json:"choices"`
	Usage   *chatUsage       `json:"usage"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	Delta        chatMessage `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
}

type chatToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (c *Client) handleSync(body io.Reader, yield func(*model.LLMResponse, error) bool) {
	var resp chatResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		yield(nil, fmt.Errorf("openai: failed to decode response: %w", err))
		return
	}

	if len(resp.Choices) == 0 {
		yield(nil, fmt.Errorf("openai: empty response"))
		return
	}

	choice := resp.Choices[0]
	content := messageToContent(choice.Message)

	yield(&model.LLMResponse{
		Content:      content,
		ModelVersion: resp.Model,
		Partial:      false,
	}, nil)
}

// --- Streaming ---

func (c *Client) handleStream(body io.Reader, yield func(*model.LLMResponse, error) bool) {
	scanner := bufio.NewScanner(body)
	var accumulatedToolCalls []chatToolCall

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line == "data: [DONE]" {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == line {
			continue // not a data line
		}

		var chunk chatResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Accumulate tool calls across chunks
		for _, tc := range choice.Delta.ToolCalls {
			if tc.ID != "" {
				accumulatedToolCalls = append(accumulatedToolCalls, tc)
			} else if len(accumulatedToolCalls) > 0 {
				// Append arguments to the last tool call
				last := &accumulatedToolCalls[len(accumulatedToolCalls)-1]
				last.Function.Arguments += tc.Function.Arguments
			}
		}

		// If there's text content, yield a partial event
		if choice.Delta.Content != "" {
			if !yield(&model.LLMResponse{
				Content: &genai.Content{
					Role:  "model",
					Parts: []*genai.Part{{Text: choice.Delta.Content}},
				},
				Partial: choice.FinishReason == "",
			}, nil) {
				return
			}
		}

		// On finish, yield the final event with any accumulated tool calls
		if choice.FinishReason != "" {
			if len(accumulatedToolCalls) > 0 {
				msg := chatMessage{
					Role:      "assistant",
					ToolCalls: accumulatedToolCalls,
				}
				yield(&model.LLMResponse{
					Content:      messageToContent(msg),
					ModelVersion: chunk.Model,
					Partial:      false,
				}, nil)
			}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		yield(nil, err)
	}
}

// --- Conversion helpers ---

func buildMessages(req *model.LLMRequest) []map[string]any {
	var messages []map[string]any

	if req.SystemInstruction != nil {
		sysText := extractTextFromContent(req.SystemInstruction)
		if sysText != "" {
			messages = append(messages, map[string]any{
				"role":    "system",
				"content": sysText,
			})
		}
	}

	for _, content := range req.Contents {
		role := content.Role
		switch role {
		case "model":
			role = "assistant"
		case "function":
			// Convert function responses to tool role
			for _, part := range content.Parts {
				if part.FunctionResponse != nil {
					respJSON, _ := json.Marshal(part.FunctionResponse.Response)
					messages = append(messages, map[string]any{
						"role":         "tool",
						"tool_call_id": part.FunctionResponse.ID,
						"content":      string(respJSON),
					})
				}
			}
			continue
		}

		msg := map[string]any{"role": role}

		// Check for tool calls
		var toolCalls []map[string]any
		var textParts []string

		for _, part := range content.Parts {
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
			if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, map[string]any{
					"id":   part.FunctionCall.ID,
					"type": "function",
					"function": map[string]any{
						"name":      part.FunctionCall.Name,
						"arguments": string(argsJSON),
					},
				})
			}
		}

		if len(textParts) > 0 {
			msg["content"] = strings.Join(textParts, "")
		}
		if len(toolCalls) > 0 {
			msg["tool_calls"] = toolCalls
		}

		messages = append(messages, msg)
	}

	return messages
}

func messageToContent(msg chatMessage) *genai.Content {
	content := &genai.Content{Role: "model"}

	if msg.Content != "" {
		content.Parts = append(content.Parts, &genai.Part{Text: msg.Content})
	}

	for _, tc := range msg.ToolCalls {
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		content.Parts = append(content.Parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: args,
			},
		})
	}

	return content
}

func extractTextFromContent(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range c.Parts {
		if p.Text != "" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

func schemaToJSON(s *genai.Schema) map[string]any {
	if s == nil {
		return map[string]any{"type": "object"}
	}

	result := map[string]any{
		"type": strings.ToLower(string(s.Type)),
	}
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
	if s.Items != nil {
		result["items"] = schemaToJSON(s.Items)
	}
	if len(s.Enum) > 0 {
		result["enum"] = s.Enum
	}
	return result
}

var _ model.LLM = (*Client)(nil)

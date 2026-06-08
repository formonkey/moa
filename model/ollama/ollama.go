package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"os"
	"time"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/model/httputil"
	"google.golang.org/genai"
)

// defaultTimeout is the HTTP timeout for Ollama requests.
// Ollama runs locally, so requests should be fast, but generation can be slow.
const defaultTimeout = 5 * time.Minute

type Client struct {
	endpoint   string
	model      string
	httpClient *http.Client
}

// NewClient initializes a native Ollama adapter.
//
// If endpoint is empty, it checks the OLLAMA_HOST environment variable
// (the standard Ollama config), then falls back to http://localhost:11434.
// This ensures compatibility with all Ollama installations: native, Docker,
// remote servers, and custom ports.
func NewClient(endpoint, defaultModel string) *Client {
	if endpoint == "" {
		endpoint = os.Getenv("OLLAMA_HOST")
	}
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	if defaultModel == "" {
		defaultModel = "llama3.2"
	}
	return &Client{
		endpoint:   endpoint,
		model:      defaultModel,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

func (c *Client) Name() string {
	return c.model
}

func (c *Client) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	targetModel := req.Model
	if targetModel == "" {
		targetModel = c.model
	}

	// Map genai.Content to Ollama messages
	var messages []map[string]any
	
	if req.SystemInstruction != nil {
		sysText := ""
		for _, p := range req.SystemInstruction.Parts {
			sysText += p.Text
		}
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": sysText,
		})
	}

	for _, content := range req.Contents {
		role := content.Role
		if role == "model" {
			role = "assistant"
		}
		
		text := ""
		for _, p := range content.Parts {
			if p.Text != "" {
				text += p.Text
			}
		}
		
		messages = append(messages, map[string]any{
			"role":    role,
			"content": text,
		})
	}

	payload := map[string]any{
		"model":    targetModel,
		"messages": messages,
		"stream":   stream,
	}

	// Map tools to Ollama's native tool format
	if len(req.Tools) > 0 {
		var ollamaTools []map[string]any
		for _, t := range req.Tools {
			for _, decl := range t.FunctionDeclarations {
				toolDef := map[string]any{
					"type": "function",
					"function": map[string]any{
						"name":        decl.Name,
						"description": decl.Description,
					},
				}
				if decl.Parameters != nil {
					params := map[string]any{
						"type": "object",
					}
					if len(decl.Parameters.Properties) > 0 {
						props := make(map[string]any)
						for name, schema := range decl.Parameters.Properties {
							prop := map[string]any{
								"type":        string(schema.Type),
								"description": schema.Description,
							}
							if len(schema.Enum) > 0 {
								prop["enum"] = schema.Enum
							}
							props[name] = prop
						}
						params["properties"] = props
					}
					if len(decl.Parameters.Required) > 0 {
						params["required"] = decl.Parameters.Required
					}
					toolDef["function"].(map[string]any)["parameters"] = params
				}
				ollamaTools = append(ollamaTools, toolDef)
			}
		}
		if len(ollamaTools) > 0 {
			payload["tools"] = ollamaTools
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
				fmt.Sprintf("%s/api/chat", c.endpoint), bytes.NewBuffer(body))
			if err != nil {
				return nil, err
			}
			httpReq.Header.Set("Content-Type", "application/json")
			return httpReq, nil
		})
		if err != nil {
			yield(nil, fmt.Errorf("ollama: request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			yield(nil, fmt.Errorf("ollama: API error status %d", resp.StatusCode))
			return
		}

		if stream {
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				var chunk struct {
					Message struct {
						Role      string `json:"role"`
						Content   string `json:"content"`
						ToolCalls []struct {
							Function struct {
								Name      string         `json:"name"`
								Arguments map[string]any `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"message"`
					Done bool `json:"done"`
				}
				if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
					continue
				}

				parts := buildOllamaParts(chunk.Message.Content, chunk.Message.ToolCalls)

				if !yield(&model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: parts,
					},
					Partial: !chunk.Done,
				}, nil) {
					return
				}
			}
			if err := scanner.Err(); err != nil {
				yield(nil, err)
			}
		} else {
			var result struct {
				Message struct {
					Role      string `json:"role"`
					Content   string `json:"content"`
					ToolCalls []struct {
						Function struct {
							Name      string         `json:"name"`
							Arguments map[string]any `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"message"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				yield(nil, err)
				return
			}

			parts := buildOllamaParts(result.Message.Content, result.Message.ToolCalls)

			yield(&model.LLMResponse{
				Content: &genai.Content{
					Role:  "model",
					Parts: parts,
				},
				Partial: false,
			}, nil)
		}
	}
}

// ollamaToolCall is used for type assertion in buildOllamaParts.
type ollamaToolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

// buildOllamaParts converts Ollama response content and tool_calls into genai.Part slice.
func buildOllamaParts(text string, toolCalls []struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}) []*genai.Part {
	var parts []*genai.Part

	if text != "" {
		parts = append(parts, &genai.Part{Text: text})
	}

	for _, tc := range toolCalls {
		parts = append(parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				Name: tc.Function.Name,
				Args: tc.Function.Arguments,
			},
		})
	}

	if len(parts) == 0 {
		parts = append(parts, &genai.Part{Text: ""})
	}

	return parts
}


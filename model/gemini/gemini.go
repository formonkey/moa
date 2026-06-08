package gemini

import (
	"context"
	"fmt"
	"iter"

	"github.com/formonkey/moa/model"
	"google.golang.org/genai"
)

type Client struct {
	client *genai.Client
	model  string
}

// NewClient initializes the Gemini native adapter for Go-Brain v2.
func NewClient(ctx context.Context, apiKey, defaultModel string) (*Client, error) {
	if defaultModel == "" {
		defaultModel = "gemini-2.5-flash"
	}
	
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, err
	}
	
	return &Client{
		client: client,
		model:  defaultModel,
	}, nil
}

func (c *Client) Name() string {
	return c.model
}

func (c *Client) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	targetModel := req.Model
	if targetModel == "" {
		targetModel = c.model
	}

	var config *genai.GenerateContentConfig
	if req.Config != nil {
		config = req.Config
	} else {
		config = &genai.GenerateContentConfig{}
	}

	if req.SystemInstruction != nil {
		config.SystemInstruction = req.SystemInstruction
	}
	if len(req.Tools) > 0 {
		config.Tools = req.Tools
	}

	if stream {
		return c.generateStream(ctx, targetModel, req.Contents, config)
	}

	return func(yield func(*model.LLMResponse, error) bool) {
		resp, err := c.client.Models.GenerateContent(ctx, targetModel, req.Contents, config)
		if err != nil {
			yield(nil, fmt.Errorf("failed to call gemini model: %w", err))
			return
		}
		if len(resp.Candidates) == 0 {
			yield(nil, fmt.Errorf("empty response candidates from gemini"))
			return
		}

		cand := resp.Candidates[0]
		yield(&model.LLMResponse{
			Content:      cand.Content,
			FinishReason: cand.FinishReason,
			UsageMetadata: resp.UsageMetadata,
			Partial:      false,
		}, nil)
	}
}

func (c *Client) generateStream(ctx context.Context, targetModel string, contents []*genai.Content, config *genai.GenerateContentConfig) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		iterator := c.client.Models.GenerateContentStream(ctx, targetModel, contents, config)
		for resp, err := range iterator {
			if err != nil {
				yield(nil, err)
				return
			}
			if len(resp.Candidates) == 0 {
				continue
			}

			cand := resp.Candidates[0]
			// FinishReason is set on the final chunk — mark it as non-partial
			isPartial := cand.FinishReason == ""
			if !yield(&model.LLMResponse{
				Content:       cand.Content,
				FinishReason:  cand.FinishReason,
				UsageMetadata: resp.UsageMetadata,
				Partial:       isPartial,
			}, nil) {
				return // Consumer stopped listening
			}
		}
	}
}

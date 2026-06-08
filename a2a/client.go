package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is an A2A client for communicating with remote A2A agents.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new A2A client for the given agent URL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// WithHTTPClient sets a custom HTTP client.
func (c *Client) WithHTTPClient(hc *http.Client) *Client {
	c.httpClient = hc
	return c
}

// GetAgentCard fetches the agent card from the remote agent.
func (c *Client) GetAgentCard(ctx context.Context) (*AgentCard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/.well-known/agent-card.json", nil)
	if err != nil {
		return nil, fmt.Errorf("a2a: create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a: fetch agent card: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("a2a: agent card returned %d", resp.StatusCode)
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("a2a: decode agent card: %w", err)
	}
	return &card, nil
}

// SendTask sends a new task to the remote agent.
func (c *Client) SendTask(ctx context.Context, req SendTaskRequest) (*Task, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("a2a: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tasks/send", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("a2a: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("a2a: send task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("a2a: send task returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var taskResp SendTaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return nil, fmt.Errorf("a2a: decode response: %w", err)
	}
	return &taskResp.Task, nil
}

// GetTask fetches the current state of a task.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/tasks/"+taskID, nil)
	if err != nil {
		return nil, fmt.Errorf("a2a: create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a: get task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("a2a: get task returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var taskResp SendTaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return nil, fmt.Errorf("a2a: decode response: %w", err)
	}
	return &taskResp.Task, nil
}

// CancelTask cancels a running task.
func (c *Client) CancelTask(ctx context.Context, taskID string) (*Task, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tasks/"+taskID+"/cancel", nil)
	if err != nil {
		return nil, fmt.Errorf("a2a: create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a: cancel task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("a2a: cancel task returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var taskResp SendTaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return nil, fmt.Errorf("a2a: decode response: %w", err)
	}
	return &taskResp.Task, nil
}

// WaitForCompletion polls the task until it reaches a terminal state.
func (c *Client) WaitForCompletion(ctx context.Context, taskID string, pollInterval time.Duration) (*Task, error) {
	if pollInterval == 0 {
		pollInterval = 500 * time.Millisecond
	}

	for {
		task, err := c.GetTask(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if task.State.IsTerminal() {
			return task, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

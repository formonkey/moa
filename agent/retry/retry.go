// Package retry provides LLM retry logic with exponential backoff.
//
// Wraps an agent's execution with automatic retry on transient failures
// (timeouts, rate limits, network errors).
package retry

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
)

// Config for the retry wrapper.
type Config struct {
	// MaxRetries is the maximum number of retry attempts. Default: 3.
	MaxRetries int
	// InitialBackoff is the base delay before the first retry. Default: 1s.
	InitialBackoff time.Duration
	// MaxBackoff caps the exponential growth. Default: 30s.
	MaxBackoff time.Duration
}

func (c *Config) defaults() {
	if c.MaxRetries <= 0 {
		c.MaxRetries = 3
	}
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = 1 * time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 30 * time.Second
	}
}

// Wrap creates a new agent that wraps the given agent with retry logic.
func Wrap(inner agent.Agent, cfg Config) agent.Agent {
	cfg.defaults()

	wrapped, err := agent.New(agent.Config{
		Name:        inner.Name(),
		Description: inner.Description(),
		SubAgents:   inner.SubAgents(),
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				backoff := cfg.InitialBackoff

				for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
					if attempt > 0 {
						fmt.Printf("[Retry] Agent %s: attempt %d/%d (backoff: %s)\n",
							inner.Name(), attempt+1, cfg.MaxRetries+1, backoff)

						select {
						case <-ctx.Done():
							yield(nil, ctx.Err())
							return
						case <-time.After(backoff):
						}

						// Exponential backoff with cap
						backoff *= 2
						if backoff > cfg.MaxBackoff {
							backoff = cfg.MaxBackoff
						}
					}

					var lastErr error
					success := true

					for event, err := range inner.Run(ctx) {
						if err != nil {
							if isRetryable(err) {
								lastErr = err
								success = false
								break
							}
							// Non-retryable error: propagate immediately
							yield(event, err)
							return
						}
						if !yield(event, nil) {
							return
						}
					}

					if success {
						return // completed successfully
					}

					if attempt == cfg.MaxRetries {
						yield(nil, fmt.Errorf("retry: agent %s failed after %d attempts: %w",
							inner.Name(), cfg.MaxRetries+1, lastErr))
						return
					}
				}
			}
		},
	})
	if err != nil {
		// This should never happen since we're using the same config as inner,
		// but propagate defensively.
		panic(fmt.Sprintf("retry.Wrap: failed to create wrapper agent: %v", err))
	}
	return wrapped
}

// isRetryable determines if an error is transient and worth retrying.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Use errors.Is for sentinel errors (handles wrapped errors correctly)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	msg := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"timeout",
		"deadline exceeded",
		"rate limit",
		"429",
		"503",
		"502",
		"connection refused",
		"connection reset",
		"eof",
		"temporary failure",
	}
	for _, pattern := range retryablePatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

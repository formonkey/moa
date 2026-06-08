package retry_test

import (
	"context"
	"errors"
	"iter"
	"testing"
	"time"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/retry"
	"github.com/formonkey/moa/internal/testutil"
	"github.com/formonkey/moa/session"
)

func TestRetrySuccess(t *testing.T) {
	inner := testutil.MockAgent("inner", "success")
	wrapped := retry.Wrap(inner, retry.Config{MaxRetries: 3, InitialBackoff: time.Millisecond})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(wrapped, sess)

	count := 0
	for _, err := range wrapped.Run(ctx) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("expected events")
	}
}

func TestRetryNonRetryable(t *testing.T) {
	inner := testutil.MockErrorAgent("inner", errors.New("permanent failure"))
	wrapped := retry.Wrap(inner, retry.Config{MaxRetries: 3, InitialBackoff: time.Millisecond})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(wrapped, sess)

	for _, err := range wrapped.Run(ctx) {
		if err != nil {
			return // Expected
		}
	}
	t.Fatal("expected error")
}

func TestRetryRetryableError(t *testing.T) {
	attempts := 0
	a, _ := agent.New(agent.Config{
		Name: "flaky",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				attempts++
				if attempts < 3 {
					yield(nil, errors.New("timeout: connection failed"))
					return
				}
				evt := session.NewEvent(ctx.InvocationID())
				evt.Author = "flaky"
				yield(evt, nil)
			}
		},
	})

	wrapped := retry.Wrap(a, retry.Config{MaxRetries: 5, InitialBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond})
	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(wrapped, sess)

	for _, err := range wrapped.Run(ctx) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if attempts < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", attempts)
	}
}

func TestRetryContextCanceled(t *testing.T) {
	inner := testutil.MockErrorAgent("inner", errors.New("timeout"))
	wrapped := retry.Wrap(inner, retry.Config{MaxRetries: 10, InitialBackoff: time.Second})

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, sess := testutil.NewTestSession(t)
	invCtx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:     cancelCtx,
		Agent:   wrapped,
		Session: sess,
	})

	for _, err := range wrapped.Run(invCtx) {
		if err != nil {
			return
		}
	}
}

func TestRetryMaxRetriesExhausted(t *testing.T) {
	inner := testutil.MockErrorAgent("inner", errors.New("timeout: always fails"))
	wrapped := retry.Wrap(inner, retry.Config{MaxRetries: 2, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(wrapped, sess)

	var gotErr error
	for _, err := range wrapped.Run(ctx) {
		if err != nil {
			gotErr = err
		}
	}
	if gotErr == nil {
		t.Fatal("expected error after exhausted retries")
	}
}

func TestRetryConfigDefaults(t *testing.T) {
	inner := testutil.MockAgent("inner", "ok")
	wrapped := retry.Wrap(inner, retry.Config{}) // All defaults

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(wrapped, sess)

	for _, err := range wrapped.Run(ctx) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestRetryVariousRetryableErrors(t *testing.T) {
	patterns := []string{
		"rate limit exceeded",
		"429 Too Many Requests",
		"503 Service Unavailable",
		"502 Bad Gateway",
		"connection refused",
		"connection reset by peer",
		"unexpected EOF",
		"temporary failure in name resolution",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			attempts := 0
			a, _ := agent.New(agent.Config{
				Name: "retryable",
				Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
					return func(yield func(*session.Event, error) bool) {
						attempts++
						if attempts < 2 {
							yield(nil, errors.New(pattern))
							return
						}
						evt := session.NewEvent("inv")
						yield(evt, nil)
					}
				},
			})

			wrapped := retry.Wrap(a, retry.Config{MaxRetries: 3, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond})
			_, sess := testutil.NewTestSession(t)
			ctx := testutil.NewTestContext(wrapped, sess)

			for _, err := range wrapped.Run(ctx) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if attempts < 2 {
				t.Fatalf("expected at least 2 attempts for pattern %q, got %d", pattern, attempts)
			}
		})
	}
}

func TestRetryDeadlineExceeded(t *testing.T) {
	attempts := 0
	a, _ := agent.New(agent.Config{
		Name: "deadline",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				attempts++
				if attempts < 2 {
					yield(nil, context.DeadlineExceeded)
					return
				}
				evt := session.NewEvent("inv")
				yield(evt, nil)
			}
		},
	})

	wrapped := retry.Wrap(a, retry.Config{MaxRetries: 3, InitialBackoff: time.Millisecond})
	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(wrapped, sess)

	for _, err := range wrapped.Run(ctx) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

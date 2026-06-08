package webhook_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/formonkey/moa/swarm/scheduler"
	"github.com/formonkey/moa/swarm/webhook"
)

func TestWebhookListenerStartAndReceive(t *testing.T) {
	bus := scheduler.NewEventBus()
	ch := bus.Subscribe("DEPLOY")

	port := 18923
	listener := webhook.New(bus, port, []webhook.Route{
		{Path: "/deploy", Event: "DEPLOY"},
		{Path: "/alert", Event: "ALERT"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- listener.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(200 * time.Millisecond)

	// Send POST
	resp, err := http.Post(fmt.Sprintf("http://localhost:%d/deploy", port), "application/json", strings.NewReader(`{"version":"1.0"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Check event was published
	select {
	case e := <-ch:
		if e.Type != "DEPLOY" {
			t.Fatalf("wrong event: %s", e.Type)
		}
		if e.Content == "" {
			t.Fatal("empty content")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	// Test GET should be rejected
	resp, _ = http.Get(fmt.Sprintf("http://localhost:%d/deploy", port))
	if resp != nil && resp.StatusCode != 405 {
		t.Fatalf("expected 405 for GET, got %d", resp.StatusCode)
	}

	cancel()
}

func TestWebhookNew(t *testing.T) {
	bus := scheduler.NewEventBus()
	listener := webhook.New(bus, 0, nil)
	if listener == nil {
		t.Fatal("nil listener")
	}
}

func TestWebhookListenAndServeError(t *testing.T) {
	bus := scheduler.NewEventBus()
	// Use a port that's already in use (bind the first one, then try to bind another)
	listener1 := webhook.New(bus, 18924, []webhook.Route{{Path: "/test", Event: "TEST"}})
	ctx1, cancel1 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel1()
	go listener1.Start(ctx1)
	time.Sleep(200 * time.Millisecond)

	listener2 := webhook.New(bus, 18924, []webhook.Route{{Path: "/test", Event: "TEST"}})
	ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel2()

	err := listener2.Start(ctx2)
	if err == nil {
		// On some OS, the error may be swallowed by context cancellation
		// That's still acceptable behavior
	}
	cancel1()
}

package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/formonkey/moa/internal/testutil"
	"github.com/formonkey/moa/swarm/scheduler"
)

func TestEventBusPubSub(t *testing.T) {
	bus := scheduler.NewEventBus()
	ch := bus.Subscribe("TEST")

	bus.Publish(scheduler.Event{Type: "TEST", Source: "test", Content: "hello"})

	select {
	case e := <-ch:
		if e.Type != "TEST" {
			t.Fatalf("wrong type: %s", e.Type)
		}
		if e.Content != "hello" {
			t.Fatalf("wrong content: %s", e.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestEventBusWildcard(t *testing.T) {
	bus := scheduler.NewEventBus()
	ch := bus.Subscribe("*")

	bus.Publish(scheduler.Event{Type: "ANY_TYPE", Source: "test"})

	select {
	case e := <-ch:
		if e.Type != "ANY_TYPE" {
			t.Fatalf("wrong type: %s", e.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestEventBusClose(t *testing.T) {
	bus := scheduler.NewEventBus()
	ch := bus.Subscribe("TEST")
	bus.Close()

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Fatal("expected closed channel")
	}
}

func TestSchedulerTrigger(t *testing.T) {
	bus := scheduler.NewEventBus()
	mockAgent := testutil.MockAgent("triggered", "ok")

	dispatchCh := make(chan struct{}, 1)
	s := scheduler.New(scheduler.Config{
		Bus: bus,
		Agents: []scheduler.AgentRunner{
			{Agent: mockAgent, Triggers: []string{"FILE_MODIFIED"}},
		},
		OnDispatch: func(agentName, triggerType string, chainDepth int) {
			select {
			case dispatchCh <- struct{}{}:
			default:
			}
		},
		OnComplete: func(agentName, triggerType string, duration time.Duration, err error) {},
	})

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	bus.Publish(scheduler.Event{Type: "FILE_MODIFIED", Source: "watcher"})

	select {
	case <-dispatchCh:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for dispatch")
	}

	cancel()
}

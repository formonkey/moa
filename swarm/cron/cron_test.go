package cron_test

import (
	"context"
	"testing"
	"time"

	"github.com/formonkey/moa/swarm/cron"
	"github.com/formonkey/moa/swarm/scheduler"
)

func TestCronEmitsEvents(t *testing.T) {
	bus := scheduler.NewEventBus()
	ch := bus.Subscribe("CRON_TICK")

	c := cron.New(bus, []cron.Job{
		{Event: "CRON_TICK", Interval: 50 * time.Millisecond},
	})

	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)

	select {
	case evt := <-ch:
		if evt.Type != "CRON_TICK" {
			t.Fatalf("wrong type: %s", evt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	cancel()
}

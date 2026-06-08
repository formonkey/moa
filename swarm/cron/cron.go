// Package cron provides time-based event emission for the swarm.
package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/formonkey/moa/swarm/scheduler"
)

// Job defines a periodic event emission.
type Job struct {
	Interval time.Duration // how often to fire
	Event    string        // event type to emit
}

// Scheduler runs cron jobs that publish events on a fixed interval.
type Scheduler struct {
	bus  *scheduler.EventBus
	jobs []Job
}

// New creates a cron scheduler.
func New(bus *scheduler.EventBus, jobs []Job) *Scheduler {
	return &Scheduler{bus: bus, jobs: jobs}
}

// Start begins all cron jobs. Cancel the context to stop.
func (s *Scheduler) Start(ctx context.Context) {
	for _, job := range s.jobs {
		j := job // capture
		go func() {
			ticker := time.NewTicker(j.Interval)
			defer ticker.Stop()
			fmt.Printf("[Cron] Scheduled %q every %s\n", j.Event, j.Interval)
			for {
				select {
				case <-ctx.Done():
					return
				case t := <-ticker.C:
					s.bus.Publish(scheduler.Event{
						Type:    j.Event,
						Source:  "Cron",
						Content: t.Format(time.RFC3339),
					})
				}
			}
		}()
	}
}

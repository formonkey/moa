// Package webhook provides an HTTP listener that converts incoming requests to swarm events.
package webhook

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/formonkey/moa/swarm/scheduler"
)

// Route maps an HTTP path to an event type.
type Route struct {
	Path  string // e.g., "/github"
	Event string // e.g., "WEBHOOK_GITHUB"
}

// Listener starts an HTTP server that publishes incoming POST requests as events.
type Listener struct {
	bus    *scheduler.EventBus
	routes []Route
	port   int
	server *http.Server
}

// New creates a webhook listener.
func New(bus *scheduler.EventBus, port int, routes []Route) *Listener {
	return &Listener{
		bus:    bus,
		routes: routes,
		port:   port,
	}
}

// Start begins listening. Cancel the context to stop.
func (l *Listener) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	for _, route := range l.routes {
		r := route // capture
		mux.HandleFunc(r.Path, func(w http.ResponseWriter, req *http.Request) {
			if req.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}

			body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20)) // 1MB max
			if err != nil {
				http.Error(w, "read error", http.StatusBadRequest)
				return
			}
			defer req.Body.Close()

			l.bus.Publish(scheduler.Event{
				Type:    r.Event,
				Source:  fmt.Sprintf("Webhook:%s", r.Path),
				Content: string(body),
			})

			fmt.Printf("[Webhook] %s %s → %s\n", req.Method, r.Path, r.Event)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		})
	}

	l.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", l.port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		l.server.Close()
	}()

	fmt.Printf("[Webhook] Listening on :%d (%d routes)\n", l.port, len(l.routes))
	if err := l.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("webhook: %w", err)
	}
	return nil
}

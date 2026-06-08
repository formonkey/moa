// Package streaming provides an HTTP adapter that streams agent execution
// events as Server-Sent Events (SSE). Zero external dependencies.
//
// It bridges the gap between moa's iter.Seq2 streaming model and
// browser/frontend consumption via standard SSE.
//
// Usage:
//
//	r, _ := runner.New(runner.Config{...})
//	http.Handle("/api/chat", streaming.SSEHandler(r, streaming.Config{}))
//	http.ListenAndServe(":8080", nil)
//
// Frontend:
//
//	const es = new EventSource('/api/chat?message=hello&session_id=abc');
//	es.onmessage = (e) => console.log(JSON.parse(e.data));
package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/runner"
	"github.com/formonkey/moa/session"
)

// Config for the SSE handler.
type Config struct {
	// DefaultUserID is used when no user ID is provided in the request.
	DefaultUserID string
	// EventBus is an optional event bus to forward events to.
	EventBus *agent.EventBus
}

// SSEEvent is the JSON payload sent to the client for each event.
type SSEEvent struct {
	Event   string `json:"event"`
	Agent   string `json:"agent,omitempty"`
	Content string `json:"content,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Payload any    `json:"payload,omitempty"`
	Error   string `json:"error,omitempty"`
	Done    bool   `json:"done,omitempty"`
}

// SSEHandler returns an http.Handler that runs an agent and streams events as SSE.
//
// Accepts GET (query params) or POST (JSON body / form):
//   - message: the user's message (required)
//   - session_id: session ID (optional, defaults to "default")
//   - user_id: user ID (optional)
//
// Response: Content-Type: text/event-stream
func SSEHandler(r *runner.Runner, cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		message, sessionID, userID := parseRequest(req)

		if message == "" {
			http.Error(w, `{"error":"message is required"}`, http.StatusBadRequest)
			return
		}
		if sessionID == "" {
			sessionID = "default"
		}
		if userID == "" {
			userID = cfg.DefaultUserID
		}
		if userID == "" {
			userID = "user"
		}

		// SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		userContent := &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: message}},
		}

		ctx := req.Context()

		writeSSE(w, flusher, SSEEvent{Event: "run.started"})

		for event, err := range r.Run(ctx, userID, sessionID, userContent, agent.RunConfig{}) {
			if ctx.Err() != nil {
				break
			}
			if err != nil {
				writeSSE(w, flusher, SSEEvent{Event: "error", Error: err.Error()})
				break
			}
			if event == nil {
				continue
			}

			sseEvent := sessionEventToSSE(event)
			writeSSE(w, flusher, sseEvent)

			if cfg.EventBus != nil {
				cfg.EventBus.Emit(agent.BusEvent{
					Type:    agent.EventType(sseEvent.Event),
					Agent:   sseEvent.Agent,
					Payload: sseEvent,
				})
			}
		}

		writeSSE(w, flusher, SSEEvent{Event: "run.completed", Done: true})
	})
}

// JSONHandler returns an http.Handler that runs an agent and returns
// the complete response as JSON (non-streaming).
func JSONHandler(r *runner.Runner, cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		message, sessionID, userID := parseRequest(req)

		if message == "" {
			http.Error(w, `{"error":"message is required"}`, http.StatusBadRequest)
			return
		}
		if sessionID == "" {
			sessionID = "default"
		}
		if userID == "" {
			userID = cfg.DefaultUserID
		}
		if userID == "" {
			userID = "user"
		}

		userContent := &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: message}},
		}

		var fullText string
		var lastErr error

		for event, err := range r.Run(req.Context(), userID, sessionID, userContent, agent.RunConfig{}) {
			if err != nil {
				lastErr = err
				break
			}
			if event != nil && event.Content != nil {
				for _, p := range event.Content.Parts {
					if p.Text != "" {
						fullText += p.Text
					}
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if lastErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": lastErr.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"response":   fullText,
			"session_id": sessionID,
		})
	})
}

// RunSSEServer starts a complete SSE server with chat and health endpoints.
//
//	streaming.RunSSEServer(r, ":8080", streaming.Config{})
func RunSSEServer(r *runner.Runner, addr string, cfg Config) error {
	mux := http.NewServeMux()
	mux.Handle("/api/chat", SSEHandler(r, cfg))
	mux.Handle("/api/chat/json", JSONHandler(r, cfg))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	fmt.Printf("[moa] SSE server listening on %s\n", addr)
	return http.ListenAndServe(addr, withCORS(mux))
}

// --- Helpers ---

func parseRequest(req *http.Request) (message, sessionID, userID string) {
	message = req.URL.Query().Get("message")
	sessionID = req.URL.Query().Get("session_id")
	userID = req.URL.Query().Get("user_id")

	if req.Method == http.MethodPost && message == "" {
		var body struct {
			Message   string `json:"message"`
			SessionID string `json:"session_id"`
			UserID    string `json:"user_id"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err == nil {
			if body.Message != "" {
				message = body.Message
			}
			if body.SessionID != "" {
				sessionID = body.SessionID
			}
			if body.UserID != "" {
				userID = body.UserID
			}
		}
	}
	return
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, evt SSEEvent) {
	data, _ := json.Marshal(evt)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func sessionEventToSSE(event *session.Event) SSEEvent {
	sse := SSEEvent{
		Event: "chunk",
		Agent: event.Author,
	}

	if event.Content != nil {
		for _, p := range event.Content.Parts {
			if p.Text != "" {
				sse.Content += p.Text
			}
			if p.FunctionCall != nil {
				sse.Event = "tool.call"
				sse.Tool = p.FunctionCall.Name
				sse.Payload = p.FunctionCall.Args
			}
			if p.FunctionResponse != nil {
				sse.Event = "tool.result"
				sse.Tool = p.FunctionResponse.Name
				sse.Payload = p.FunctionResponse.Response
			}
		}
	}

	return sse
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type contextKey string

const ctxKeyUserID contextKey = "user_id"

// WithUserID stores a user ID in the context.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxKeyUserID, userID)
}

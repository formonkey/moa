# Streaming — SSE for UIs

moa's streaming package provides a zero-dependency HTTP adapter that streams agent execution events as Server-Sent Events (SSE). Connect any frontend to a moa agent.

## Quick Start

### Server (Go)

```go
r, _ := runner.New(runner.Config{
    Agent:             myAgent,
    SessionService:    inmemory.New(),
    AutoCreateSession: true,
})

// Option 1: Full server with health check and CORS
streaming.RunSSEServer(r, ":8080", streaming.Config{})

// Option 2: Mount as handler
http.Handle("/api/chat", streaming.SSEHandler(r, streaming.Config{}))
http.ListenAndServe(":8080", nil)
```

### Frontend (JavaScript)

```javascript
async function chat(message) {
    const response = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            message: message,
            session_id: 'my-session'
        })
    });

    const reader = response.body.getReader();
    const decoder = new TextDecoder();

    while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        const lines = decoder.decode(value).split('\n');
        for (const line of lines) {
            if (!line.startsWith('data: ')) continue;
            const event = JSON.parse(line.slice(6));

            switch (event.event) {
                case 'chunk':
                    appendText(event.content);
                    break;
                case 'tool.call':
                    showToolCall(event.tool, event.payload);
                    break;
                case 'tool.result':
                    showToolResult(event.tool, event.payload);
                    break;
                case 'error':
                    showError(event.error);
                    break;
                case 'run.completed':
                    markDone();
                    break;
            }
        }
    }
}
```

## Event Types

| Event | Description | Payload |
|-------|-------------|---------|
| `run.started` | Agent execution began | — |
| `chunk` | Text content from the agent | `content` |
| `tool.call` | Agent is calling a tool | `tool`, `payload` (args) |
| `tool.result` | Tool returned a result | `tool`, `payload` (result) |
| `error` | An error occurred | `error` |
| `run.completed` | Agent execution finished | `done: true` |

## SSEEvent JSON Format

```json
{
    "event": "chunk",
    "agent": "developer",
    "content": "Here's what I found...",
    "tool": "",
    "payload": null,
    "done": false
}
```

```json
{
    "event": "tool.call",
    "agent": "developer",
    "tool": "codegraph_search",
    "payload": {"query": "router"}
}
```

## Request Format

### GET

```
GET /api/chat?message=hello&session_id=abc&user_id=bob
```

### POST (JSON)

```
POST /api/chat
Content-Type: application/json

{"message": "hello", "session_id": "abc", "user_id": "bob"}
```

## Non-Streaming (JSON) Endpoint

For APIs that don't need real-time updates:

```
POST /api/chat/json
Content-Type: application/json

{"message": "hello"}
```

Response:
```json
{"response": "Hello! How can I help?", "session_id": "default"}
```

## Event Bus Integration

Forward SSE events to the internal event bus for logging or custom handlers:

```go
bus := agent.NewEventBus()
bus.On(func(evt agent.BusEvent) {
    log.Printf("[%s] %s: %v", evt.Type, evt.Agent, evt.Payload)
})

streaming.RunSSEServer(r, ":8080", streaming.Config{
    EventBus: bus,
})
```

## CORS

CORS headers are automatically added:
- `Access-Control-Allow-Origin: *`
- `Access-Control-Allow-Methods: GET, POST, OPTIONS`
- `Access-Control-Allow-Headers: Content-Type`

OPTIONS preflight requests are handled automatically.

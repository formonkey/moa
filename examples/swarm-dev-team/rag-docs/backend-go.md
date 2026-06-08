# Go Backend — Architecture & Best Practices

## Project Structure

```
backend/
├── cmd/
│   └── server/
│       └── main.go          # Entry point
├── internal/
│   ├── handler/             # HTTP handlers (controllers)
│   ├── service/             # Business logic
│   ├── repository/          # Data access layer
│   ├── model/               # Domain models
│   └── middleware/           # Auth, logging, CORS
├── pkg/                     # Shared utilities
├── migrations/              # SQL migrations
├── go.mod
└── go.sum
```

## Key Patterns

### Clean Architecture
Strict dependency flow: handler → service → repository. Never import handler from service.

### HTTP Router (Chi)
```go
r := chi.NewRouter()
r.Use(middleware.Logger)
r.Use(middleware.Recoverer)
r.Route("/api/v1", func(r chi.Router) {
    r.Get("/tasks", h.ListTasks)
    r.Post("/tasks", h.CreateTask)
    r.Get("/tasks/{id}", h.GetTask)
})
```

### Error Handling
Always wrap errors with context:
```go
if err != nil {
    return fmt.Errorf("taskService.Create: %w", err)
}
```

### Testing
Table-driven tests with `t.Run`:
```go
func TestCreateTask(t *testing.T) {
    tests := []struct{
        name    string
        input   CreateTaskInput
        wantErr bool
    }{
        {"valid task", CreateTaskInput{Title: "Test"}, false},
        {"empty title", CreateTaskInput{Title: ""}, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

## Concurrency
- Use `errgroup` for parallel operations
- Protect shared state with `sync.Mutex`
- Use `context.Context` for cancellation
- Never use `go func()` without error handling

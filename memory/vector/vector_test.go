package vector_test

import (
	"context"
	"errors"
	"iter"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/formonkey/moa/memory"
	"github.com/formonkey/moa/memory/vector"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
)

// --- Mock Store ---

type mockStore struct {
	saved     []vector.Document
	results   []vector.Document
	saveErr   error
	searchErr error
	deleteErr error
}

func (m *mockStore) Save(ctx context.Context, doc vector.Document) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = append(m.saved, doc)
	return nil
}

func (m *mockStore) Search(ctx context.Context, query string, k int) ([]vector.Document, error) {
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	if len(m.results) > k {
		return m.results[:k], nil
	}
	return m.results, nil
}

func (m *mockStore) DeleteBySource(ctx context.Context, source string) error {
	return m.deleteErr
}

// --- Mock Session ---

type mockEvents struct {
	events []*session.Event
}

func (e *mockEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, evt := range e.events {
			if !yield(evt) {
				return
			}
		}
	}
}
func (e *mockEvents) Len() int               { return len(e.events) }
func (e *mockEvents) At(i int) *session.Event { return e.events[i] }

type mockSession struct {
	id     string
	events *mockEvents
	state  session.State
}

func (s *mockSession) ID() string                { return s.id }
func (s *mockSession) AppName() string           { return "test" }
func (s *mockSession) UserID() string            { return "u1" }
func (s *mockSession) State() session.State      { return s.state }
func (s *mockSession) Events() session.Events    { return s.events }
func (s *mockSession) LastUpdateTime() time.Time { return time.Now() }

// --- Tests ---

func TestNew(t *testing.T) {
	store := &mockStore{}
	vm := vector.New(store, 5)
	if vm == nil {
		t.Fatal("expected non-nil")
	}
}

func TestNewDefaultTopK(t *testing.T) {
	store := &mockStore{}
	vm := vector.New(store, 0) // should default to 10
	if vm == nil {
		t.Fatal("expected non-nil")
	}
}

func TestNewNegativeTopK(t *testing.T) {
	store := &mockStore{}
	vm := vector.New(store, -5) // should default to 10
	if vm == nil {
		t.Fatal("expected non-nil")
	}
}

func TestSearchMemory(t *testing.T) {
	store := &mockStore{
		results: []vector.Document{
			{ID: "1", Content: "Go is great", Source: "session:s1"},
			{ID: "2", Content: "Rust is fast", Source: "session:s1"},
		},
	}
	vm := vector.New(store, 10)

	resp, err := vm.SearchMemory(context.Background(), &memory.SearchRequest{
		AppName: "test",
		UserID:  "u1",
		Query:   "programming",
	})
	if err != nil {
		t.Fatalf("SearchMemory: %v", err)
	}
	if len(resp.Memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(resp.Memories))
	}
	if resp.Memories[0].ID != "1" {
		t.Fatal("wrong ID")
	}
	if resp.Memories[0].Content.Parts[0].Text != "Go is great" {
		t.Fatal("wrong content")
	}
	if resp.Memories[0].Author != "session:s1" {
		t.Fatalf("wrong author: %s", resp.Memories[0].Author)
	}
}

func TestSearchMemoryError(t *testing.T) {
	store := &mockStore{searchErr: errors.New("search failed")}
	vm := vector.New(store, 5)

	_, err := vm.SearchMemory(context.Background(), &memory.SearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSearchMemoryEmpty(t *testing.T) {
	store := &mockStore{results: nil}
	vm := vector.New(store, 5)

	resp, err := vm.SearchMemory(context.Background(), &memory.SearchRequest{Query: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Memories) != 0 {
		t.Fatalf("expected 0 memories, got %d", len(resp.Memories))
	}
}

func makeEvent(id, author, text string) *session.Event {
	evt := &session.Event{
		ID:     id,
		Author: author,
	}
	evt.LLMResponse = model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{{Text: text}},
		},
	}
	return evt
}

func TestAddSessionToMemory(t *testing.T) {
	store := &mockStore{}
	vm := vector.New(store, 5)

	// We need a real session with State for the mock
	svc := session.InMemoryService()
	createResp, _ := svc.Create(context.Background(), &session.CreateRequest{
		AppName: "test", UserID: "u1", SessionID: "s1",
	})
	realSess := createResp.Session

	sess := &mockSession{
		id: "s1",
		events: &mockEvents{
			events: []*session.Event{
				makeEvent("e1", "agent1", "Hello world"),
				makeEvent("e2", "user", "Hi there"),
			},
		},
		state: realSess.State(),
	}

	err := vm.AddSessionToMemory(context.Background(), sess)
	if err != nil {
		t.Fatalf("AddSessionToMemory: %v", err)
	}
	if len(store.saved) != 2 {
		t.Fatalf("expected 2 saved docs, got %d", len(store.saved))
	}
	if store.saved[0].Source != "session:s1" {
		t.Fatalf("wrong source: %s", store.saved[0].Source)
	}
	if store.saved[0].Metadata["author"] != "agent1" {
		t.Fatal("wrong metadata author")
	}
}

func TestAddSessionToMemorySkipsNilContent(t *testing.T) {
	store := &mockStore{}
	vm := vector.New(store, 5)

	svc := session.InMemoryService()
	createResp, _ := svc.Create(context.Background(), &session.CreateRequest{
		AppName: "test", UserID: "u1", SessionID: "s2",
	})

	sess := &mockSession{
		id: "s2",
		events: &mockEvents{
			events: []*session.Event{
				{ID: "e1"}, // nil content
			},
		},
		state: createResp.Session.State(),
	}

	err := vm.AddSessionToMemory(context.Background(), sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.saved) != 0 {
		t.Fatalf("expected 0 saved docs for nil content, got %d", len(store.saved))
	}
}

func TestAddSessionToMemorySkipsEmptyText(t *testing.T) {
	store := &mockStore{}
	vm := vector.New(store, 5)

	svc := session.InMemoryService()
	createResp, _ := svc.Create(context.Background(), &session.CreateRequest{
		AppName: "test", UserID: "u1", SessionID: "s3",
	})

	emptyEvt := &session.Event{ID: "e1"}
	emptyEvt.LLMResponse = model.LLMResponse{
		Content: &genai.Content{
			Parts: []*genai.Part{{Text: ""}}, // empty text
		},
	}

	sess := &mockSession{
		id: "s3",
		events: &mockEvents{
			events: []*session.Event{emptyEvt},
		},
		state: createResp.Session.State(),
	}

	err := vm.AddSessionToMemory(context.Background(), sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.saved) != 0 {
		t.Fatal("expected 0 saved docs for empty text")
	}
}

func TestAddSessionToMemorySaveError(t *testing.T) {
	store := &mockStore{saveErr: errors.New("save failed")}
	vm := vector.New(store, 5)

	svc := session.InMemoryService()
	createResp, _ := svc.Create(context.Background(), &session.CreateRequest{
		AppName: "test", UserID: "u1", SessionID: "s4",
	})

	sess := &mockSession{
		id: "s4",
		events: &mockEvents{
			events: []*session.Event{
				makeEvent("e1", "agent", "some content"),
			},
		},
		state: createResp.Session.State(),
	}

	err := vm.AddSessionToMemory(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error from Save")
	}
}

func TestIngestFile(t *testing.T) {
	store := &mockStore{}
	vm := vector.New(store, 5)

	err := vm.IngestFile(context.Background(), "main.go", "package main\nfunc main() {}")
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if len(store.saved) != 1 {
		t.Fatal("expected 1 saved doc")
	}
	if store.saved[0].Source != "main.go" {
		t.Fatalf("wrong source: %s", store.saved[0].Source)
	}
	if store.saved[0].Metadata["type"] != "file" {
		t.Fatal("wrong metadata type")
	}
}

func TestIngestFileError(t *testing.T) {
	store := &mockStore{saveErr: errors.New("disk full")}
	vm := vector.New(store, 5)

	err := vm.IngestFile(context.Background(), "bad.go", "content")
	if err == nil {
		t.Fatal("expected error")
	}
}

// Verify interface compliance
var _ memory.Service = (*vector.Memory)(nil)

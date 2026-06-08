package instructionutil_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/util/instructionutil"
)

func createSession(t *testing.T) session.Session {
	t.Helper()
	svc := session.InMemoryService()
	resp, _ := svc.Create(context.Background(), &session.CreateRequest{
		AppName: "app", UserID: "u1", SessionID: "s1",
	})
	return resp.Session
}

func TestInjectSessionState(t *testing.T) {
	sess := createSession(t)
	sess.State().Set("name", "Alice")
	sess.State().Set("role", "engineer")

	result, err := instructionutil.InjectSessionState(context.Background(), "Hello {name}, you are {role}", sess.State(), nil)
	if err != nil {
		t.Fatalf("InjectSessionState: %v", err)
	}
	if result != "Hello Alice, you are engineer" {
		t.Fatalf("unexpected: %q", result)
	}
}

func TestInjectSessionStateNoPlaceholders(t *testing.T) {
	sess := createSession(t)
	result, _ := instructionutil.InjectSessionState(context.Background(), "No placeholders", sess.State(), nil)
	if result != "No placeholders" {
		t.Fatalf("unexpected: %q", result)
	}
}

func TestInjectOptionalPlaceholder(t *testing.T) {
	sess := createSession(t)
	result, err := instructionutil.InjectSessionState(context.Background(), "Name: {name?}", sess.State(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// name? is optional and not set — should be empty
	if result != "Name: " {
		t.Fatalf("unexpected: %q", result)
	}
}

func TestInjectMissingRequired(t *testing.T) {
	sess := createSession(t)
	_, err := instructionutil.InjectSessionState(context.Background(), "Hello {missing_key}", sess.State(), nil)
	if err == nil {
		t.Fatal("expected error for missing required key")
	}
}

func TestInjectArtifactWithoutLoader(t *testing.T) {
	sess := createSession(t)
	_, err := instructionutil.InjectSessionState(context.Background(), "Content: {artifact.doc}", sess.State(), nil)
	if err == nil {
		t.Fatal("expected error for artifact without loader")
	}
}

func TestInjectOptionalArtifactWithoutLoader(t *testing.T) {
	sess := createSession(t)
	result, err := instructionutil.InjectSessionState(context.Background(), "Content: {artifact.doc?}", sess.State(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Content: " {
		t.Fatalf("unexpected: %q", result)
	}
}

type mockArtifactTextLoader struct{}

func (m *mockArtifactTextLoader) LoadArtifactText(ctx context.Context, name string) (string, error) {
	return "loaded:" + name, nil
}

type mockErrorArtifactLoader struct{}

func (m *mockErrorArtifactLoader) LoadArtifactText(ctx context.Context, name string) (string, error) {
	return "", fmt.Errorf("artifact %q not found", name)
}

func TestInjectArtifactWithLoader(t *testing.T) {
	sess := createSession(t)
	result, err := instructionutil.InjectSessionState(context.Background(), "Content: {artifact.readme}", sess.State(), &mockArtifactTextLoader{})
	if err != nil {
		t.Fatalf("InjectSessionState: %v", err)
	}
	if result != "Content: loaded:readme" {
		t.Fatalf("unexpected: %q", result)
	}
}

func TestInjectArtifactLoaderError(t *testing.T) {
	sess := createSession(t)
	_, err := instructionutil.InjectSessionState(context.Background(), "Content: {artifact.missing}", sess.State(), &mockErrorArtifactLoader{})
	if err == nil {
		t.Fatal("expected error for failed artifact load")
	}
}

func TestInjectOptionalArtifactLoaderError(t *testing.T) {
	sess := createSession(t)
	result, err := instructionutil.InjectSessionState(context.Background(), "Content: {artifact.missing?}", sess.State(), &mockErrorArtifactLoader{})
	if err != nil {
		t.Fatalf("unexpected error for optional artifact: %v", err)
	}
	if result != "Content: " {
		t.Fatalf("unexpected: %q", result)
	}
}

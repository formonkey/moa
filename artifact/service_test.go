package artifact_test

import (
	"context"
	"sync"
	"testing"

	"github.com/formonkey/moa/artifact"
	"google.golang.org/genai"
)

func TestSaveAndLoad(t *testing.T) {
	svc := artifact.InMemoryService()
	ctx := context.Background()

	data := &genai.Part{Text: "hello artifact"}
	version, err := svc.Save(ctx, &artifact.SaveRequest{
		AppName:   "app",
		UserID:    "u1",
		SessionID: "s1",
		Filename:  "test.txt",
		Data:      data,
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if version != 1 {
		t.Fatalf("expected version 1, got %d", version)
	}

	// Save a second version
	data2 := &genai.Part{Text: "version 2"}
	version2, _ := svc.Save(ctx, &artifact.SaveRequest{
		AppName:   "app",
		UserID:    "u1",
		SessionID: "s1",
		Filename:  "test.txt",
		Data:      data2,
	})
	if version2 != 2 {
		t.Fatalf("expected version 2, got %d", version2)
	}

	// Load latest
	resp, err := svc.Load(ctx, &artifact.LoadRequest{
		AppName:   "app",
		UserID:    "u1",
		SessionID: "s1",
		Filename:  "test.txt",
	})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if resp.Data.Text != "version 2" {
		t.Fatalf("expected 'version 2', got %q", resp.Data.Text)
	}
	if resp.Version != 2 {
		t.Fatalf("expected version 2, got %d", resp.Version)
	}

	// Load specific version
	resp1, _ := svc.Load(ctx, &artifact.LoadRequest{
		AppName:   "app",
		UserID:    "u1",
		SessionID: "s1",
		Filename:  "test.txt",
		Version:   1,
	})
	if resp1.Data.Text != "hello artifact" {
		t.Fatalf("expected 'hello artifact', got %q", resp1.Data.Text)
	}
}

func TestListArtifacts(t *testing.T) {
	svc := artifact.InMemoryService()
	ctx := context.Background()

	svc.Save(ctx, &artifact.SaveRequest{AppName: "app", UserID: "u1", SessionID: "s1", Filename: "a.txt", Data: &genai.Part{Text: "a"}})
	svc.Save(ctx, &artifact.SaveRequest{AppName: "app", UserID: "u1", SessionID: "s1", Filename: "b.txt", Data: &genai.Part{Text: "b"}})

	names, err := svc.List(ctx, &artifact.ListRequest{AppName: "app", UserID: "u1", SessionID: "s1"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2, got %d", len(names))
	}
}

func TestDeleteArtifact(t *testing.T) {
	svc := artifact.InMemoryService()
	ctx := context.Background()

	svc.Save(ctx, &artifact.SaveRequest{AppName: "app", UserID: "u1", SessionID: "s1", Filename: "del.txt", Data: &genai.Part{Text: "x"}})

	err := svc.Delete(ctx, &artifact.DeleteRequest{AppName: "app", UserID: "u1", SessionID: "s1", Filename: "del.txt"})
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = svc.Load(ctx, &artifact.LoadRequest{AppName: "app", UserID: "u1", SessionID: "s1", Filename: "del.txt"})
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestVersions(t *testing.T) {
	svc := artifact.InMemoryService()
	ctx := context.Background()

	svc.Save(ctx, &artifact.SaveRequest{AppName: "app", UserID: "u1", SessionID: "s1", Filename: "v.txt", Data: &genai.Part{Text: "1"}})
	svc.Save(ctx, &artifact.SaveRequest{AppName: "app", UserID: "u1", SessionID: "s1", Filename: "v.txt", Data: &genai.Part{Text: "2"}})
	svc.Save(ctx, &artifact.SaveRequest{AppName: "app", UserID: "u1", SessionID: "s1", Filename: "v.txt", Data: &genai.Part{Text: "3"}})

	count, err := svc.Versions(ctx, &artifact.VersionsRequest{AppName: "app", UserID: "u1", SessionID: "s1", Filename: "v.txt"})
	if err != nil {
		t.Fatalf("Versions failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3, got %d", count)
	}
}

func TestEmptyFilename(t *testing.T) {
	svc := artifact.InMemoryService()
	ctx := context.Background()

	_, err := svc.Save(ctx, &artifact.SaveRequest{AppName: "app", UserID: "u1", SessionID: "s1", Filename: "", Data: &genai.Part{Text: "x"}})
	if err == nil {
		t.Fatal("expected error for empty filename")
	}
}

func TestConcurrentArtifactAccess(t *testing.T) {
	svc := artifact.InMemoryService()
	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			svc.Save(ctx, &artifact.SaveRequest{
				AppName: "app", UserID: "u1", SessionID: "s1",
				Filename: "concurrent.txt", Data: &genai.Part{Text: "data"},
			})
		}(i)
	}
	wg.Wait()

	count, _ := svc.Versions(ctx, &artifact.VersionsRequest{AppName: "app", UserID: "u1", SessionID: "s1", Filename: "concurrent.txt"})
	if count != 50 {
		t.Fatalf("expected 50 versions, got %d", count)
	}
}

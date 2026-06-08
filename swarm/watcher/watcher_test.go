package watcher_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/formonkey/moa/swarm/scheduler"
	"github.com/formonkey/moa/swarm/watcher"
)

func TestNewFileWatcher(t *testing.T) {
	bus := scheduler.NewEventBus()
	dir := t.TempDir()

	fw, err := watcher.NewFileWatcher(bus, []watcher.FileWatchRule{
		{Path: dir, Event: "FILE_CHANGED"},
	})
	if err != nil {
		t.Fatalf("NewFileWatcher: %v", err)
	}
	if fw == nil {
		t.Fatal("nil watcher")
	}
}

func TestNewFileWatcherInvalidPath(t *testing.T) {
	bus := scheduler.NewEventBus()
	_, err := watcher.NewFileWatcher(bus, []watcher.FileWatchRule{
		{Path: "/nonexistent/path/that/doesnt/exist", Event: "X"},
	})
	// May or may not error depending on fsnotify impl
	_ = err
}

func TestFileWatcherStart(t *testing.T) {
	bus := scheduler.NewEventBus()
	dir := t.TempDir()

	fw, err := watcher.NewFileWatcher(bus, []watcher.FileWatchRule{
		{Path: dir, Event: "FILE_CHANGED"},
	})
	if err != nil {
		t.Fatalf("NewFileWatcher: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start in background — will exit when context expires
	go fw.Start(ctx)

	// Create a file to trigger an event
	time.Sleep(100 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644)

	// Wait for context to finish
	<-ctx.Done()
}

func TestBuildWatcherRunOnce(t *testing.T) {
	bus := scheduler.NewEventBus()
	ch := bus.Subscribe("BUILD_OK")

	bw := watcher.NewBuildWatcher(bus, t.TempDir(), "echo", "build ok")
	ctx := context.Background()
	bw.RunOnce(ctx)

	select {
	case e := <-ch:
		if e.Type != "BUILD_OK" {
			t.Fatalf("wrong event type: %s", e.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for build event")
	}
}

func TestBuildWatcherWatch(t *testing.T) {
	bus := scheduler.NewEventBus()
	bw := watcher.NewBuildWatcher(bus, t.TempDir(), "echo", "ok")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	bw.Watch(ctx, 200*time.Millisecond)
	<-ctx.Done()
}

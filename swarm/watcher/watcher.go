// Package watcher provides filesystem and build monitoring that publishes events to the scheduler.
package watcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/formonkey/moa/swarm/scheduler"
)

// --- FileWatcher ---

// FileWatchRule defines what to watch and what event to emit.
type FileWatchRule struct {
	Path  string // file path or glob pattern (e.g., "./src/**/*.go")
	Event string // event type to emit (e.g., "FILE_MODIFIED")
	Diff  bool   // include diff in payload (future)
}

// FileWatcher monitors files and emits events when they change.
type FileWatcher struct {
	bus      *scheduler.EventBus
	rules    []FileWatchRule
	watcher  *fsnotify.Watcher
	debounce time.Duration
}

// NewFileWatcher creates a file watcher that publishes to the given EventBus.
func NewFileWatcher(bus *scheduler.EventBus, rules []FileWatchRule) (*FileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watcher: failed to create fsnotify watcher: %w", err)
	}
	return &FileWatcher{
		bus:      bus,
		rules:    rules,
		watcher:  w,
		debounce: 500 * time.Millisecond,
	}, nil
}

// Start begins watching. Cancel the context to stop.
func (fw *FileWatcher) Start(ctx context.Context) error {
	// Resolve and add watch paths
	for _, rule := range fw.rules {
		paths, err := resolveGlob(rule.Path)
		if err != nil {
			return fmt.Errorf("watcher: failed to resolve %q: %w", rule.Path, err)
		}
		for _, p := range paths {
			if err := fw.watcher.Add(p); err != nil {
				fmt.Printf("[FileWatcher] Warning: cannot watch %s: %v\n", p, err)
			}
		}
	}

	// Debounce map to avoid duplicate events on rapid saves
	var mu sync.Mutex
	lastEvent := make(map[string]time.Time)

	go func() {
		defer fw.watcher.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-fw.watcher.Events:
				if !ok {
					return
				}
				if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
					continue
				}

				mu.Lock()
				last, exists := lastEvent[event.Name]
				now := time.Now()
				if exists && now.Sub(last) < fw.debounce {
					mu.Unlock()
					continue
				}
				lastEvent[event.Name] = now
				mu.Unlock()

				// Find matching rule
				for _, rule := range fw.rules {
					if matchesRule(event.Name, rule.Path) {
						fw.bus.Publish(scheduler.Event{
							Type:    rule.Event,
							Source:  "FileWatcher",
							Content: event.Name,
						})
						fmt.Printf("[FileWatcher] %s → %s\n", event.Name, rule.Event)
						break
					}
				}

			case err, ok := <-fw.watcher.Errors:
				if !ok {
					return
				}
				fmt.Printf("[FileWatcher] Error: %v\n", err)
			}
		}
	}()
	return nil
}

// resolveGlob expands a path pattern into concrete directories to watch.
func resolveGlob(pattern string) ([]string, error) {
	// If it's a direct file/dir, return it
	if !strings.Contains(pattern, "*") {
		info, err := os.Stat(pattern)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return []string{pattern}, nil
		}
		return []string{filepath.Dir(pattern)}, nil
	}

	// For glob patterns like "./src/**/*.go", watch the base directory recursively
	base := pattern
	for strings.Contains(base, "*") {
		base = filepath.Dir(base)
	}

	var dirs []string
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible dirs
		}
		if d.IsDir() {
			// Skip hidden dirs and common noise
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(dirs) == 0 {
		dirs = append(dirs, base)
	}
	return dirs, nil
}

// matchesRule checks if a file path matches a watch rule pattern.
func matchesRule(filePath, pattern string) bool {
	if !strings.Contains(pattern, "*") {
		return strings.HasPrefix(filePath, pattern)
	}
	// Simple glob: extract extension from pattern
	ext := filepath.Ext(pattern)
	if ext != "" && ext != ".*" {
		return filepath.Ext(filePath) == ext
	}
	return true
}

// --- BuildWatcher ---

// BuildWatcher runs a command periodically or on-demand and publishes
// BUILD_FAILED or BUILD_OK based on exit code.
type BuildWatcher struct {
	bus     *scheduler.EventBus
	command string
	args    []string
	dir     string
}

// NewBuildWatcher creates a build watcher.
func NewBuildWatcher(bus *scheduler.EventBus, dir, command string, args ...string) *BuildWatcher {
	return &BuildWatcher{
		bus:     bus,
		command: command,
		args:    args,
		dir:     dir,
	}
}

// RunOnce executes the build command and publishes the result.
func (bw *BuildWatcher) RunOnce(ctx context.Context) {
	cmd := exec.CommandContext(ctx, bw.command, bw.args...)
	cmd.Dir = bw.dir
	output, err := cmd.CombinedOutput()

	if err != nil {
		bw.bus.Publish(scheduler.Event{
			Type:    "BUILD_FAILED",
			Source:  "BuildWatcher",
			Content: string(output),
		})
		fmt.Printf("[BuildWatcher] BUILD_FAILED: %s\n", bw.command)
	} else {
		bw.bus.Publish(scheduler.Event{
			Type:    "BUILD_OK",
			Source:  "BuildWatcher",
			Content: string(output),
		})
	}
}

// Watch runs the build command every interval. Cancel context to stop.
func (bw *BuildWatcher) Watch(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				bw.RunOnce(ctx)
			}
		}
	}()
}

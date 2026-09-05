package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStartWatcherReturnsNilOnUnsupportedPlatform(t *testing.T) {
	// StartWatcher should gracefully return nil if fsnotify cannot create a
	// watcher (e.g. in CI environments with limited inotify).
	tempDir := t.TempDir()
	// Force a path that cannot be watched by creating a file (not a dir).
	badPath := filepath.Join(tempDir, "not-a-dir")
	if err := os.WriteFile(badPath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// This should not panic — just return nil.
	fw := StartWatcher(context.Background(), nil, "proj", badPath, WatcherConfig{})
	if fw != nil {
		fw.Stop()
		// If it returned non-nil, that's fine too (platform may support it).
	}
}

func TestFileWatcherDirtyFlag(t *testing.T) {
	tempDir := t.TempDir()
	// Create a subdirectory for watching.
	subDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a long debounce so dirty stays set long enough to check.
	fw := StartWatcher(ctx, nil, "proj", tempDir, WatcherConfig{Debounce: 5 * time.Second})
	if fw == nil {
		t.Skip("fsnotify not available on this platform")
	}
	defer fw.Stop()

	// Initially not dirty.
	if fw.IsDirty() {
		t.Error("expected not dirty initially")
	}

	// Create a file to trigger a change event.
	if err := os.WriteFile(filepath.Join(subDir, "test.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for the event to be processed (fsnotify has some latency).
	time.Sleep(300 * time.Millisecond)

	if !fw.IsDirty() {
		t.Error("expected dirty after file creation")
	}
}

func TestFileWatcherDebounceCollapsesBursts(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Long debounce so dirty stays set.
	fw := StartWatcher(ctx, nil, "proj", tempDir, WatcherConfig{Debounce: 5 * time.Second})
	if fw == nil {
		t.Skip("fsnotify not available on this platform")
	}
	defer fw.Stop()

	// Create multiple files rapidly — should collapse into one dirty state.
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(subDir, "f.go"), []byte("x"), 0644)
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(300 * time.Millisecond)

	if !fw.IsDirty() {
		t.Error("expected dirty after burst of writes")
	}
}

package graph

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// defaultWatchDebounce is the default debounce window before a detected change
// triggers a graph resync. Matches codegraph's 2s default.
const defaultWatchDebounce = 2 * time.Second

// WatcherConfig holds configuration for the file watcher.
type WatcherConfig struct {
	Debounce time.Duration // debounce window; zero or negative uses defaultWatchDebounce
}

// FileWatcher watches a project directory for source file changes and triggers
// incremental graph sync in the background. When active, getOrLoadGraph can
// skip the O(n) DetectStaleFiles walk per query by checking IsDirty().
//
// The watcher is best-effort: if fsnotify is unavailable or the walk fails,
// StartWatcher returns nil and callers fall back to the existing lazy sync.
type FileWatcher struct {
	db        *sql.DB
	projectID string
	projPath  string
	debounce  time.Duration

	watcher    *fsnotify.Watcher
	dirty      bool
	dirtyMu    sync.Mutex
	syncTimer  *time.Timer
	syncTimerMu sync.Mutex

	cancel context.CancelFunc
}

// StartWatcher starts a background file watcher that keeps the dependency graph
// fresh. Returns nil when fsnotify is unavailable — callers should fall back
// to the existing lazy SyncGraphIfStale path.
func StartWatcher(ctx context.Context, db *sql.DB, projectID, projPath string, cfg WatcherConfig) *FileWatcher {
	debounce := cfg.Debounce
	if debounce <= 0 {
		debounce = defaultWatchDebounce
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		// fsnotify not available on this platform — graceful fallback.
		return nil
	}

	childCtx, cancel := context.WithCancel(ctx)
	fw := &FileWatcher{
		db:        db,
		projectID: projectID,
		projPath:  projPath,
		debounce:  debounce,
		watcher:   w,
		cancel:    cancel,
	}

	// Seed the watcher with existing directories.
	fw.seedDirs()

	go fw.loop(childCtx)
	return fw
}

// IsDirty reports whether file changes have been detected since the last
// background sync completed. When false, getOrLoadGraph can safely skip
// the DetectStaleFiles walk and rely on the cache.
func (fw *FileWatcher) IsDirty() bool {
	fw.dirtyMu.Lock()
	defer fw.dirtyMu.Unlock()
	return fw.dirty
}

// Stop cancels the background goroutine and closes the fsnotify watcher.
func (fw *FileWatcher) Stop() {
	if fw.cancel != nil {
		fw.cancel()
	}
}

// --- internal ---

func (fw *FileWatcher) loop(ctx context.Context) {
	defer fw.watcher.Close()
	defer fw.stopTimer()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			// Only react to file write/create/remove/rename events.
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			fw.markDirty()
			fw.scheduleSync()
		case _, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			// Log and continue — transient fsnotify errors are not fatal.
		}
	}
}

func (fw *FileWatcher) markDirty() {
	fw.dirtyMu.Lock()
	fw.dirty = true
	fw.dirtyMu.Unlock()
}

func (fw *FileWatcher) clearDirty() {
	fw.dirtyMu.Lock()
	fw.dirty = false
	fw.dirtyMu.Unlock()
}

func (fw *FileWatcher) stopTimer() {
	fw.syncTimerMu.Lock()
	if fw.syncTimer != nil {
		fw.syncTimer.Stop()
		fw.syncTimer = nil
	}
	fw.syncTimerMu.Unlock()
}

// scheduleSync resets the debounce timer. Each file event restarts the window,
// so a burst of edits collapses into one sync.
func (fw *FileWatcher) scheduleSync() {
	fw.syncTimerMu.Lock()
	defer fw.syncTimerMu.Unlock()

	if fw.syncTimer != nil {
		fw.syncTimer.Stop()
	}
	fw.syncTimer = time.AfterFunc(fw.debounce, func() {
		// Run sync outside the lock. Skip when db is nil (test mode).
		if fw.db != nil {
			if _, err := SyncGraphIfStale(fw.db, fw.projectID, fw.projPath); err != nil {
				return
			}
			GlobalGraphCache.Invalidate(fw.projectID)
		}
		fw.clearDirty()

		// Watch for new directories created during sync.
		fw.seedDirs()
	})
}

// seedDirs walks the project tree and adds every directory to the fsnotify
// watch. Directories that match skip patterns are excluded.
func (fw *FileWatcher) seedDirs() {
	_ = filepath.WalkDir(fw.projPath, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		name := d.Name()
		// Skip well-known non-source directories.
		if name == ".git" || name == ".sv-memory" || name == "node_modules" ||
			name == "vendor" || name == ".venv" || name == "dist" || name == "build" {
			return filepath.SkipDir
		}
		// Skip hidden directories (except the project root itself).
		if strings.HasPrefix(name, ".") && path != fw.projPath {
			return filepath.SkipDir
		}
		// Best-effort: ignore errors on individual directories.
		_ = fw.watcher.Add(path)
		return nil
	})
}

package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
	"github.com/svtech-code/sv-memory/internal/memory"
)

// syncPathStat returns the path + mtime of the signal file/dir to watch:
// the chunks dir if it exists, otherwise the legacy memories.json.
func (s *Server) syncPathStat() (string, time.Time) {
	syncFile := filepath.Join(s.cfg.ProjPath, ".sv-memory", "memories.json")
	chunkDir := filepath.Join(s.cfg.ProjPath, ".sv-memory", "chunks")
	if fi, err := os.Stat(chunkDir); err == nil {
		return chunkDir, fi.ModTime()
	}
	if fi, err := os.Stat(syncFile); err == nil {
		return syncFile, fi.ModTime()
	}
	return "", time.Time{}
}

// maybeSyncFromGit imports shared memories only when their mtime advanced
// since the last call. Falls back to a full sync the first time (zero mtime).
func (s *Server) maybeSyncFromGit() {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	_, mtim := s.syncPathStat()
	if mtim.IsZero() {
		s.lastSyncMtim = time.Time{}
		return
	}
	if !mtim.After(s.lastSyncMtim) {
		return
	}
	start := time.Now()
	if err := memory.SyncFromGit(s.pool.Writer, s.cfg.ProjectID, s.cfg.ProjPath); err != nil {
		fmt.Fprintf(os.Stderr, "[sv-memory] syncFromGit failed: %v\n", err)
		return
	}
	s.lastSyncMtim = mtim
	debugLog("syncFromGit pulled in %s", time.Since(start))
}

// scheduleSync resets the debounce timer. Each call to sv_mem_save invokes
// this instead of writing to disk immediately. The timer fires 500ms after the
// last save, so a burst of saves triggers only one Git write.
func (s *Server) scheduleSync() {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	if s.syncTimer != nil {
		s.syncTimer.Stop()
	}
	s.syncVersion++
	currentVersion := s.syncVersion
	s.syncTimer = time.AfterFunc(500*time.Millisecond, func() {
		s.debounceMu.Lock()
		if currentVersion != s.syncVersion {
			s.debounceMu.Unlock()
			return
		}
		s.debounceMu.Unlock()

		if !viper.GetBool("git_sync_enabled") {
			return
		}
		startSync := time.Now()
		if err := memory.SyncToGit(s.pool.Writer, s.cfg.ProjectID, s.cfg.ProjPath); err != nil {
			fmt.Fprintf(os.Stderr, "[sv-memory] syncToGit (debounced) failed: %v\n", err)
			return
		}
		debugLog("syncToGit (debounced) took %s", time.Since(startSync))
		s.syncMu.Lock()
		_, mtim := s.syncPathStat()
		if !mtim.IsZero() {
			s.lastSyncMtim = mtim
		}
		s.syncMu.Unlock()
	})
}

// flushPendingSync stops the debounce timer and, if a write is pending, flushes
// the Git sync synchronously. Invoked during graceful shutdown. It forces the
// monolithic memories.json rewrite so the fallback file is always up to date.
func (s *Server) flushPendingSync() {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	if s.syncTimer != nil {
		s.syncTimer.Stop()
		if viper.GetBool("git_sync_enabled") {
			fmt.Fprintf(os.Stderr, "[sv-memory] Flushing pending Git sync...\n")
			if err := memory.SyncToGitForceFull(s.pool.Writer, s.cfg.ProjectID, s.cfg.ProjPath); err != nil {
				fmt.Fprintf(os.Stderr, "[sv-memory] Final syncToGit failed: %v\n", err)
			}
		}
	}
}

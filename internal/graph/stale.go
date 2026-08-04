package graph

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// StaleReport describes which project files changed on disk compared to the
// graph's stored file metadata. It is produced by a cheap mtime/size probe
// that never reads file contents, so the agent can decide whether a graph
// refresh is needed without paying for a full rescan.
type StaleReport struct {
	Changed    []string // files added or modified (mtime/size differ)
	Deleted    []string // files tracked in the DB but missing on disk
	NeedsFull  bool     // true when a full rebuild is required (no prior meta or excessive churn)
	HasChanges bool     // true when Changed/Deleted is non-empty
}

// DetectStaleFiles walks the project doing only os.Stat per file (no content
// reads) and compares mtime/size against graph_files_meta. It returns the set
// of files that changed, were deleted, or whether a full rebuild is needed.
func DetectStaleFiles(db *sql.DB, projectID string, projPath string) (*StaleReport, error) {
	report := &StaleReport{}

	oldMeta, err := loadFileMeta(db, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed loading file meta: %w", err)
	}

	// First run: no prior metadata — a full rebuild is required.
	if len(oldMeta) == 0 {
		report.NeedsFull = true
		return report, nil
	}

	gi, _ := loadGitignore(projPath)
	current := make(map[string]fileMetaEntry)

	err = filepath.WalkDir(projPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, relErr := filepath.Rel(projPath, path)
		if relErr != nil {
			return nil
		}
		if d.IsDir() {
			if fallbackIgnoreDirs[d.Name()] {
				return filepath.SkipDir
			}
			if gi != nil && gi.match(relPath, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if gi != nil && gi.match(relPath, false) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(relPath))
		if !supportedScanExts[ext] {
			return nil
		}

		fi, fiErr := os.Stat(path)
		mtimeMs := int64(0)
		size := int64(0)
		if fiErr == nil {
			mtimeMs = fi.ModTime().UnixMilli()
			size = fi.Size()
		}
		current[relPath] = fileMetaEntry{mtimeMs: mtimeMs, size: size}

		prev, tracked := oldMeta[relPath]
		if !tracked {
			report.Changed = append(report.Changed, relPath)
		} else if prev.mtimeMs != mtimeMs || prev.size != size {
			report.Changed = append(report.Changed, relPath)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed walking directory for staleness: %w", err)
	}

	for p := range oldMeta {
		if _, stillExists := current[p]; !stillExists {
			report.Deleted = append(report.Deleted, p)
		}
	}

	// Excessive churn (>30% of tracked files) → full rebuild is cheaper.
	totalTracked := len(oldMeta)
	churn := len(report.Changed) + len(report.Deleted)
	if totalTracked > 0 && float64(churn)/float64(totalTracked) > 0.30 {
		report.NeedsFull = true
	}

	report.HasChanges = len(report.Changed) > 0 || len(report.Deleted) > 0
	return report, nil
}

// SyncGraphIfStale refreshes the dependency graph only when project files
// actually changed on disk. It uses a cheap mtime/size probe (DetectStaleFiles)
// and, when changes are found, runs the incremental sync restricted to the
// changed files. Returns true when a sync was performed. This is the lazy
// freshness path used before serving graph queries.
func SyncGraphIfStale(db *sql.DB, projectID string, projPath string) (bool, error) {
	stale, err := DetectStaleFiles(db, projectID, projPath)
	if err != nil {
		return false, err
	}
	if !stale.HasChanges && !stale.NeedsFull {
		return false, nil
	}

	readOnly := make(map[string]bool, len(stale.Changed))
	for _, p := range stale.Changed {
		readOnly[p] = true
	}

	if stale.NeedsFull {
		if syncErr := syncGraphFull(db, projectID, projPath); syncErr != nil {
			return true, syncErr
		}
		return true, nil
	}

	ok, err := trySyncGraphIncrementalFiltered(db, projectID, projPath, readOnly)
	if err != nil {
		return true, err
	}
	if ok {
		return true, nil
	}
	// Incremental path unavailable (should not happen given the probe already
	// determined a partial update is viable) — fall back to a full rebuild.
	if err := syncGraphFull(db, projectID, projPath); err != nil {
		return true, err
	}
	return true, nil
}

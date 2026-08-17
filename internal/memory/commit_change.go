package memory

import (
	"database/sql"
	"fmt"
	"time"
)

// CommitChangeAtomic persists the authoritative state of a spec commit in a
// single transaction: the decision memory (via the normal save logic), the
// memory->change link, the capability delta merge, and the change status stamp.
// Either all of them land or none do — a partial failure (e.g. an ADDED delta
// that already exists) leaves the change untouched so the agent can fix the
// delta and retry without a half-committed decision memory or capability state.
//
// The graph wiring (rationale_for/implements edges) and the conflicts_with
// judgments are derived, best-effort concerns and are intentionally performed
// by the caller after the transaction commits, so a graph hiccup never rolls
// back the authoritative commit.
func CommitChangeAtomic(db *sql.DB, mem *Memory, changeID, capabilityPath string, deltas []Delta, status string) (*Memory, error) {
	now := time.Now()
	if err := prepareMemoryForSave(mem, now); err != nil {
		return nil, err
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin commit transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Decision memory (topic-key upsert on decision/<slug>).
	if err := saveMemoryInTx(tx, mem, now); err != nil {
		return nil, fmt.Errorf("failed to save decision memory: %w", err)
	}

	// 2. Memory -> change link.
	if err := execMemoryChangeLink(tx, mem.ProjectID, mem.ID, changeID); err != nil {
		return nil, err
	}

	// 3. Capability delta merge (best-effort in isolation, but inside the tx it
	// must succeed for the commit to land).
	if err := mergeDeltasInTx(tx, mem.ProjectID, capabilityPath, deltas); err != nil {
		return nil, fmt.Errorf("failed to merge requirements: %w", err)
	}

	// 4. Lifecycle stamp.
	if err := execChangeStatusUpdate(tx, mem.ProjectID, changeID, status); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit spec transaction: %w", err)
	}
	return mem, nil
}

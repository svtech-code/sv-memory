package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Pool struct {
	Writer *sql.DB
	Reader *sql.DB
}

func (p *Pool) Close() error {
	var wErr, rErr error
	if p.Writer != nil {
		wErr = p.Writer.Close()
	}
	if p.Reader != nil {
		rErr = p.Reader.Close()
	}
	if wErr != nil {
		return wErr
	}
	return rErr
}

func NewDBPool(dbPath string) (*Pool, error) {
	w, err := openDBWithTuning(dbPath, true)
	if err != nil {
		return nil, fmt.Errorf("failed to open writer at %s: %w", dbPath, err)
	}
	if err := applyMigrations(w); err != nil {
		w.Close()
		return nil, err
	}

	r, rerr := openDBWithTuning(dbPath, false)
	if rerr != nil {
		w.Close()
		return nil, fmt.Errorf("failed to open reader at %s: %w", dbPath, rerr)
	}
	return &Pool{Writer: w, Reader: r}, nil
}

func openDBWithTuning(dbPath string, isWriter bool) (*sql.DB, error) {
	pragmaParams := "_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-20000)&_pragma=mmap_size(268435456)&_pragma=busy_timeout(5000)"
	var dsn string
	if !isWriter {
		dsn = "file:" + dbPath + "?mode=ro&" + pragmaParams
	} else {
		dsn = "file:" + dbPath + "?" + pragmaParams
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	if isWriter {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	} else {
		// Readers outnumber writers: WAL allows many concurrent readers while
		// the single writer serializes commits. 16 open readers comfortably
		// covers bursts of parallel tool calls without saturating SQLite.
		maxReaders := 16
		db.SetMaxOpenConns(maxReaders)
		db.SetMaxIdleConns(maxReaders)
		// Readers are long-lived: recycling WAL read connections adds churn
		// and can transiently hit busy_timeout. Keep them warm (idle pruning
		// via ConnMaxIdleTime still applies).
		db.SetConnMaxLifetime(0)
	}
	db.SetConnMaxIdleTime(30 * time.Minute)

	// Apply foreign_keys pragma once via Exec to ensure correct value per-session
	// (modernc's _pragma may not set it correctly for some configurations).
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign_keys: %w", err)
	}

	return db, nil
}

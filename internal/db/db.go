package db

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
)

func debugEnabled() bool {
	v := os.Getenv("SV_MEMORY_DEBUG")
	if v == "" {
		return false
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	return true
}

func InitDB(dbPath string) (*sql.DB, error) {
	db, err := openDBWithTuning(dbPath, true)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}

	if err := applyMigrations(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func RegisterProject(db *sql.DB, id, name, path string) error {
	query := `
	INSERT INTO projects (id, name, path)
	VALUES (?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name = excluded.name,
		path = excluded.path;
	`
	_, err := db.Exec(query, id, name, path)
	if err != nil {
		return fmt.Errorf("failed to register project %s: %w", name, err)
	}
	return nil
}

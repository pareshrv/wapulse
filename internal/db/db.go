package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// InitDB initializes and opens an embedded SQLite database connection.
// It configures WAL mode, foreign keys, a busy timeout, and caps the
// connection pool to a single writer to prevent SQLITE_BUSY races across
// the watcher, scheduler, and queue-worker goroutines.
func InitDB(dbPath string) (*sql.DB, error) {
	database, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Cap the pool to one connection so concurrent goroutines never race on
	// writes — SQLite supports only one writer at a time.
	database.SetMaxOpenConns(1)

	// Enable WAL (Write-Ahead Logging) for performance and concurrent reads.
	var journalMode string
	err = database.QueryRow("PRAGMA journal_mode=WAL;").Scan(&journalMode)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Enable foreign key constraints.
	_, err = database.Exec("PRAGMA foreign_keys = ON;")
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Wait up to 5 s before returning SQLITE_BUSY, giving concurrent writers
	// a chance to finish rather than failing immediately.
	_, err = database.Exec("PRAGMA busy_timeout = 5000;")
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	return database, nil
}

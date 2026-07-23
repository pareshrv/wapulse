package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// InitDB initializes and opens an embedded SQLite database connection with WAL mode enabled.
func InitDB(dbPath string) (*sql.DB, error) {
	database, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL (Write-Ahead Logging) mode for performance and concurrent reads/writes
	var journalMode string
	err = database.QueryRow("PRAGMA journal_mode=WAL;").Scan(&journalMode)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Enable foreign key constraints
	_, err = database.Exec("PRAGMA foreign_keys = ON;")
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	return database, nil
}

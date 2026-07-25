package db

import (
	"database/sql"
	"fmt"
	"time"
)

// ProcessedFile represents a row in the processed_files deduplication ledger.
type ProcessedFile struct {
	FileHash    string
	FilePath    string
	EventType   string
	ProcessedAt time.Time
}

// IngestionDAO manages the processed_files deduplication ledger.
// MarkProcessed intentionally accepts a *sql.Tx so the ingestion gate
// (task 4.1.3) can atomically combine the outbox insert, the processed_files
// insert, and the watermark update in a single transaction.
type IngestionDAO struct {
	db *sql.DB
}

// NewIngestionDAO creates a new IngestionDAO instance.
func NewIngestionDAO(db *sql.DB) *IngestionDAO {
	return &IngestionDAO{db: db}
}

// HasProcessedHash returns true if the given SHA-256 hash already exists in
// the processed_files table, indicating the file has already been handled.
func (dao *IngestionDAO) HasProcessedHash(hash string) (bool, error) {
	var count int
	err := dao.db.QueryRow(
		`SELECT COUNT(1) FROM processed_files WHERE file_hash = ?`, hash,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("HasProcessedHash: %w", err)
	}
	return count > 0, nil
}

// MarkProcessed records a file hash inside the provided transaction.
// Callers are responsible for beginning and committing (or rolling back) the
// transaction — this method participates in an externally-managed transaction
// so the ingestion gate can keep plugin execution, outbox insert, and this
// insert all atomic.
func (dao *IngestionDAO) MarkProcessed(tx *sql.Tx, f ProcessedFile) error {
	_, err := tx.Exec(`
		INSERT INTO processed_files (file_hash, file_path, event_type, processed_at)
		VALUES (?, ?, ?, ?)`,
		f.FileHash, f.FilePath, f.EventType,
		f.ProcessedAt.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return fmt.Errorf("MarkProcessed: %w", err)
	}
	return nil
}

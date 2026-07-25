package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestIngestionDAO(t *testing.T) {
	tempDir := t.TempDir()
	database, err := InitDB(filepath.Join(tempDir, "ingestion_test.db"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer database.Close()

	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	dao := NewIngestionDAO(database)
	const hash = "abc123def456"

	// 1. Hash not present yet.
	found, err := dao.HasProcessedHash(hash)
	if err != nil {
		t.Fatalf("HasProcessedHash: %v", err)
	}
	if found {
		t.Fatal("expected hash to be absent before MarkProcessed")
	}

	// 2. MarkProcessed inside a transaction, then commit.
	tx, err := database.Begin()
	if err != nil {
		t.Fatalf("Begin tx: %v", err)
	}
	err = dao.MarkProcessed(tx, ProcessedFile{
		FileHash:    hash,
		FilePath:    "/data/bills/invoice_001.pdf",
		EventType:   "WHOLESALER_BILL",
		ProcessedAt: time.Now().UTC(),
	})
	if err != nil {
		tx.Rollback()
		t.Fatalf("MarkProcessed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// 3. Hash now present.
	found, err = dao.HasProcessedHash(hash)
	if err != nil {
		t.Fatalf("HasProcessedHash after insert: %v", err)
	}
	if !found {
		t.Fatal("expected hash to be present after MarkProcessed")
	}

	// 4. Rolling back the transaction must not persist the record.
	const hash2 = "rollback999"
	tx2, err := database.Begin()
	if err != nil {
		t.Fatalf("Begin tx2: %v", err)
	}
	if err := dao.MarkProcessed(tx2, ProcessedFile{
		FileHash:    hash2,
		FilePath:    "/data/bills/invoice_002.pdf",
		EventType:   "WHOLESALER_BILL",
		ProcessedAt: time.Now().UTC(),
	}); err != nil {
		tx2.Rollback()
		t.Fatalf("MarkProcessed tx2: %v", err)
	}
	tx2.Rollback()

	found, err = dao.HasProcessedHash(hash2)
	if err != nil {
		t.Fatalf("HasProcessedHash after rollback: %v", err)
	}
	if found {
		t.Fatal("hash should be absent after transaction rollback")
	}
}

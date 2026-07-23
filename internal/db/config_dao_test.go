package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigDAO(t *testing.T) {
	tempDir := t.TempDir()
	testDBPath := filepath.Join(tempDir, "test_config.db")

	database, err := InitDB(testDBPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	configDAO := NewConfigDAO(database)

	// 1. Test missing key returns sql.ErrNoRows
	_, err = configDAO.GetKV("missing_key")
	if err != sql.ErrNoRows {
		t.Fatalf("Expected sql.ErrNoRows for missing key, got: %v", err)
	}

	// 2. Test SetKV & GetKV (UPSERT)
	err = configDAO.SetKV("system_mode", "PROD")
	if err != nil {
		t.Fatalf("SetKV failed: %v", err)
	}

	val, err := configDAO.GetKV("system_mode")
	if err != nil || val != "PROD" {
		t.Fatalf("GetKV failed. Expected 'PROD', got '%s', err: %v", val, err)
	}

	// Update existing key
	err = configDAO.SetKV("system_mode", "MAINTENANCE")
	if err != nil {
		t.Fatalf("SetKV update failed: %v", err)
	}

	valUpdated, err := configDAO.GetKV("system_mode")
	if err != nil || valUpdated != "MAINTENANCE" {
		t.Fatalf("GetKV update failed. Expected 'MAINTENANCE', got '%s'", valUpdated)
	}

	// 3. Test High Watermark Persistence
	watermarkKey := "last_scanned_pdf_ts"
	nowUTC := time.Now().UTC().Truncate(time.Second)

	err = configDAO.SetWatermark(watermarkKey, nowUTC)
	if err != nil {
		t.Fatalf("SetWatermark failed: %v", err)
	}

	retrievedTime, err := configDAO.GetWatermark(watermarkKey)
	if err != nil {
		t.Fatalf("GetWatermark failed: %v", err)
	}

	if !retrievedTime.Equal(nowUTC) {
		t.Errorf("Watermark mismatch. Expected %v, got %v", nowUTC, retrievedTime)
	}
}

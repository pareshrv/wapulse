package db

import (
	"path/filepath"
	"testing"
)

func TestInitAndMigrateDB(t *testing.T) {
	tempDir := t.TempDir()
	testDBPath := filepath.Join(tempDir, "test_wapulse.db")

	database, err := InitDB(testDBPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	// Execute migration
	err = Migrate(database)
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Verify core tables exist
	tables := []string{"app_config", "message_outbox", "scheduled_notifications"}
	for _, table := range tables {
		var name string
		query := `SELECT name FROM sqlite_master WHERE type='table' AND name=?;`
		err := database.QueryRow(query, table).Scan(&name)
		if err != nil {
			t.Errorf("Expected table '%s' to exist, but it was not found: %v", table, err)
		}
	}
}

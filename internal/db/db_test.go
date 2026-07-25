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

	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// All 5 v3 tables must exist.
	tables := []string{
		"app_config",
		"message_outbox",
		"scheduled_notifications",
		"processed_files",
		"message_templates",
	}
	for _, table := range tables {
		var name string
		err := database.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist: %v", table, err)
		}
	}

	// message_outbox must have the v3 template columns.
	outboxColumns := []string{"template_name", "template_vars_json"}
	for _, col := range outboxColumns {
		var found bool
		rows, err := database.Query(`PRAGMA table_info(message_outbox)`)
		if err != nil {
			t.Fatalf("PRAGMA table_info(message_outbox) failed: %v", err)
		}
		for rows.Next() {
			var cid int
			var cname, ctype string
			var notNull, pk int
			var dflt interface{}
			if err := rows.Scan(&cid, &cname, &ctype, &notNull, &dflt, &pk); err != nil {
				rows.Close()
				t.Fatalf("scan error: %v", err)
			}
			if cname == col {
				found = true
			}
		}
		rows.Close()
		if !found {
			t.Errorf("expected column %q in message_outbox", col)
		}
	}

	// message_templates must have the approval_status column.
	var found bool
	rows, err := database.Query(`PRAGMA table_info(message_templates)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(message_templates) failed: %v", err)
	}
	for rows.Next() {
		var cid int
		var cname, ctype string
		var notNull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &cname, &ctype, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			t.Fatalf("scan error: %v", err)
		}
		if cname == "approval_status" {
			found = true
		}
	}
	rows.Close()
	if !found {
		t.Error("expected column \"approval_status\" in message_templates")
	}

	// Migrate must be idempotent — running it twice must not error.
	if err := Migrate(database); err != nil {
		t.Errorf("second Migrate call failed (not idempotent): %v", err)
	}
}

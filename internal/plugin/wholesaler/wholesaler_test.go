//go:build wholesaler

package wholesaler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wapulse/internal/db"
)

func setupTestDB(t *testing.T) *db.ConfigDAO {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db.NewConfigDAO(database)
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "invoice_*.pdf")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

func TestOnFileDiscovered_NoPhoneConfigured(t *testing.T) {
	configDAO := setupTestDB(t)

	p := New()
	// Inject configDAO directly for testing without a full sql.DB Init call.
	p.configDAO = configDAO

	_, err := p.OnFileDiscovered(writeTempFile(t, "dummy pdf content"))
	if err == nil {
		t.Fatal("expected error when default phone not configured")
	}
	if !strings.Contains(err.Error(), cfgDefaultPhone) {
		t.Errorf("error should mention config key, got: %v", err)
	}
}

func TestOnFileDiscovered_ReturnsEvent(t *testing.T) {
	configDAO := setupTestDB(t)

	if err := configDAO.SetKV(cfgDefaultPhone, "+919876543210"); err != nil {
		t.Fatalf("SetKV phone: %v", err)
	}
	if err := configDAO.SetKV(cfgDefaultCustomerName, "Ravi Sharma"); err != nil {
		t.Fatalf("SetKV name: %v", err)
	}

	p := New()
	p.configDAO = configDAO

	filePath := writeTempFile(t, "fake invoice data 12345")
	events, err := p.OnFileDiscovered(filePath)
	if err != nil {
		t.Fatalf("OnFileDiscovered: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	ev := events[0]

	if ev.EventType != "WHOLESALER_BILL" {
		t.Errorf("EventType: expected WHOLESALER_BILL, got %q", ev.EventType)
	}
	if ev.CustomerPhone != "+919876543210" {
		t.Errorf("CustomerPhone: expected +919876543210, got %q", ev.CustomerPhone)
	}
	if ev.AttachmentPath != filePath {
		t.Errorf("AttachmentPath: expected %q, got %q", filePath, ev.AttachmentPath)
	}
	if ev.Data["CustomerName"] != "Ravi Sharma" {
		t.Errorf("CustomerName: expected Ravi Sharma, got %q", ev.Data["CustomerName"])
	}
	if ev.Data["FileName"] != filepath.Base(filePath) {
		t.Errorf("FileName mismatch: got %q", ev.Data["FileName"])
	}
	if ev.IdempotencyKey == "" || !strings.HasPrefix(ev.IdempotencyKey, "WHOLESALER_BILL:") {
		t.Errorf("IdempotencyKey malformed: %q", ev.IdempotencyKey)
	}
}

func TestOnFileDiscovered_Idempotency(t *testing.T) {
	// Same file content must always produce the same idempotency key.
	configDAO := setupTestDB(t)
	if err := configDAO.SetKV(cfgDefaultPhone, "+919876543210"); err != nil {
		t.Fatalf("SetKV: %v", err)
	}

	p := New()
	p.configDAO = configDAO

	content := "identical invoice content"
	events1, _ := p.OnFileDiscovered(writeTempFile(t, content))
	events2, _ := p.OnFileDiscovered(writeTempFile(t, content))

	if events1[0].IdempotencyKey != events2[0].IdempotencyKey {
		t.Errorf("same content must produce same idempotency key\ngot1: %s\ngot2: %s",
			events1[0].IdempotencyKey, events2[0].IdempotencyKey)
	}
}

func TestOnFileDiscovered_DifferentContentDifferentKey(t *testing.T) {
	configDAO := setupTestDB(t)
	if err := configDAO.SetKV(cfgDefaultPhone, "+919876543210"); err != nil {
		t.Fatalf("SetKV: %v", err)
	}

	p := New()
	p.configDAO = configDAO

	events1, _ := p.OnFileDiscovered(writeTempFile(t, "invoice A"))
	events2, _ := p.OnFileDiscovered(writeTempFile(t, "invoice B"))

	if events1[0].IdempotencyKey == events2[0].IdempotencyKey {
		t.Error("different content must produce different idempotency keys")
	}
}

func TestOnScheduledTrigger_ReturnsEmpty(t *testing.T) {
	p := New()
	p.configDAO = setupTestDB(t)

	events, err := p.OnScheduledTrigger(db.ScheduledRule{RuleKey: "ANY"})
	if err != nil {
		t.Fatalf("OnScheduledTrigger: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

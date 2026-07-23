package db

import (
	"path/filepath"
	"testing"
)

func TestOutboxDAO(t *testing.T) {
	tempDir := t.TempDir()
	testDBPath := filepath.Join(tempDir, "test_outbox.db")

	database, err := InitDB(testDBPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	outbox := NewOutboxDAO(database)

	// 1. Test EnqueueAction
	item := &ActionItem{
		IdempotencyKey: "hash_123456",
		ActionType:     "WHATSAPP",
		CustomerPhone:  "+919876543210",
		MessageBody:    "Your bill of Rs. 500 is ready.",
		PayloadPath:    "/path/to/bill.pdf",
	}

	inserted, err := outbox.EnqueueAction(item)
	if err != nil || !inserted {
		t.Fatalf("Failed to enqueue action: %v", err)
	}

	// 2. Test Idempotency (Duplicate insert should be ignored)
	insertedAgain, err := outbox.EnqueueAction(item)
	if err != nil {
		t.Fatalf("Enqueue duplicate check failed: %v", err)
	}
	if insertedAgain {
		t.Errorf("Expected duplicate enqueue to return false, got true")
	}

	// 3. Test FetchPendingActions
	items, err := outbox.FetchPendingActions(10)
	if err != nil || len(items) != 1 {
		t.Fatalf("Expected 1 pending action, got %d: %v", len(items), err)
	}

	// 4. Test MarkActionSent
	err = outbox.MarkActionSent(items[0].ID)
	if err != nil {
		t.Fatalf("Failed to mark action as sent: %v", err)
	}

	// Verify queue is now empty
	itemsAfterSent, err := outbox.FetchPendingActions(10)
	if err != nil {
		t.Fatalf("Fetch pending after sent failed: %v", err)
	}
	if len(itemsAfterSent) != 0 {
		t.Errorf("Expected 0 pending actions, got %d", len(itemsAfterSent))
	}
}

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

	// 1. Missing key returns sql.ErrNoRows.
	_, err = configDAO.GetKV("missing_key")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows for missing key, got: %v", err)
	}

	// 2. SetKV / GetKV upsert.
	if err := configDAO.SetKV("system_mode", "PROD"); err != nil {
		t.Fatalf("SetKV failed: %v", err)
	}
	val, err := configDAO.GetKV("system_mode")
	if err != nil || val != "PROD" {
		t.Fatalf("GetKV: expected PROD, got %q err %v", val, err)
	}

	if err := configDAO.SetKV("system_mode", "MAINTENANCE"); err != nil {
		t.Fatalf("SetKV update failed: %v", err)
	}
	val, err = configDAO.GetKV("system_mode")
	if err != nil || val != "MAINTENANCE" {
		t.Fatalf("GetKV update: expected MAINTENANCE, got %q err %v", val, err)
	}

	// 3. Watermark round-trip.
	nowUTC := time.Now().UTC().Truncate(time.Second)
	if err := configDAO.SetWatermark("last_scanned_pdf_ts", nowUTC); err != nil {
		t.Fatalf("SetWatermark failed: %v", err)
	}
	got, err := configDAO.GetWatermark("last_scanned_pdf_ts")
	if err != nil {
		t.Fatalf("GetWatermark failed: %v", err)
	}
	if !got.Equal(nowUTC) {
		t.Errorf("watermark mismatch: expected %v, got %v", nowUTC, got)
	}

	// 4. Missing watermark returns zero time (not an error).
	zero, err := configDAO.GetWatermark("nonexistent_watermark")
	if err != nil {
		t.Fatalf("GetWatermark for missing key should not error, got: %v", err)
	}
	if !zero.IsZero() {
		t.Errorf("expected zero time for missing watermark, got %v", zero)
	}

	// 5. WABA credentials — plain text (no encryption key).
	t.Run("WABACredentials_NoEncryption", func(t *testing.T) {
		creds := WABACredentials{
			WABAID:        "waba-123",
			PhoneNumberID: "phone-456",
			AccessToken:   "token-abc",
		}
		if err := configDAO.SetWABACredentials(creds); err != nil {
			t.Fatalf("SetWABACredentials failed: %v", err)
		}

		got, err := configDAO.GetWABACredentials()
		if err != nil {
			t.Fatalf("GetWABACredentials failed: %v", err)
		}
		if got != creds {
			t.Errorf("credentials mismatch: expected %+v, got %+v", creds, got)
		}
	})

	// 6. ClearWABACredentials removes all three keys.
	t.Run("ClearWABACredentials", func(t *testing.T) {
		if err := configDAO.ClearWABACredentials(); err != nil {
			t.Fatalf("ClearWABACredentials failed: %v", err)
		}
		_, err := configDAO.GetWABACredentials()
		if err != ErrCredentialsNotFound {
			t.Errorf("expected ErrCredentialsNotFound after clear, got: %v", err)
		}
	})

	// 7. WABA credentials — AES-256-GCM encryption round-trip.
	t.Run("WABACredentials_WithEncryption", func(t *testing.T) {
		// 32-byte key (in production this comes from a machine-derived or
		// securely stored key, not a hard-coded value).
		key := make([]byte, 32)
		for i := range key {
			key[i] = byte(i + 1)
		}

		encDAO, err := NewConfigDAOWithKey(database, key)
		if err != nil {
			t.Fatalf("NewConfigDAOWithKey failed: %v", err)
		}

		creds := WABACredentials{
			WABAID:        "waba-encrypted-789",
			PhoneNumberID: "phone-encrypted-012",
			AccessToken:   "super-secret-token",
		}
		if err := encDAO.SetWABACredentials(creds); err != nil {
			t.Fatalf("SetWABACredentials (encrypted) failed: %v", err)
		}

		// Verify the raw stored value is NOT the plain-text token.
		raw, err := encDAO.GetKV(keyWABAAccessToken)
		if err != nil {
			t.Fatalf("GetKV for raw token failed: %v", err)
		}
		if raw == creds.AccessToken {
			t.Error("access token is stored as plain text — encryption did not apply")
		}

		// Decrypt and verify round-trip.
		got, err := encDAO.GetWABACredentials()
		if err != nil {
			t.Fatalf("GetWABACredentials (encrypted) failed: %v", err)
		}
		if got != creds {
			t.Errorf("encrypted credentials mismatch: expected %+v, got %+v", creds, got)
		}
	})

	// 8. SetWABACredentials rejects incomplete credentials.
	t.Run("WABACredentials_Validation", func(t *testing.T) {
		if err := configDAO.SetWABACredentials(WABACredentials{
			WABAID: "only-waba-id",
		}); err == nil {
			t.Error("expected error for incomplete credentials, got nil")
		}
	})

	// 9. Invalid encryption key length is rejected.
	t.Run("NewConfigDAOWithKey_InvalidKeyLen", func(t *testing.T) {
		_, err := NewConfigDAOWithKey(database, []byte("too-short"))
		if err == nil {
			t.Error("expected error for short key, got nil")
		}
	})
}

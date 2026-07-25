package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"
)

// credentialKeys are the config_key names used to store per-client WABA credentials.
const (
	keyWABAID          = "waba_id"
	keyPhoneNumberID   = "phone_number_id"
	keyWABAAccessToken = "waba_access_token"
)

// ErrCredentialsNotFound is returned when one or more WABA credentials are absent.
var ErrCredentialsNotFound = errors.New("WABA credentials not configured")

// WABACredentials holds the three per-client identifiers needed to call the
// WhatsApp Cloud API on behalf of a client.
type WABACredentials struct {
	WABAID        string
	PhoneNumberID string
	AccessToken   string
}

// ConfigDAO manages key-value configurations, watermarks, and encrypted
// per-client WABA credentials in the app_config table.
type ConfigDAO struct {
	db         *sql.DB
	encryptKey []byte // 32-byte AES-256 key; nil disables encryption (tests)
}

// NewConfigDAO creates a ConfigDAO with no encryption key.
// Use NewConfigDAOWithKey for production paths that store credentials.
func NewConfigDAO(db *sql.DB) *ConfigDAO {
	return &ConfigDAO{db: db}
}

// NewConfigDAOWithKey creates a ConfigDAO that encrypts credential values
// with AES-256-GCM before writing to SQLite.
// encryptionKey must be exactly 32 bytes.
func NewConfigDAOWithKey(db *sql.DB, encryptionKey []byte) (*ConfigDAO, error) {
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("encryptionKey must be 32 bytes, got %d", len(encryptionKey))
	}
	key := make([]byte, 32)
	copy(key, encryptionKey)
	return &ConfigDAO{db: db, encryptKey: key}, nil
}

// ---------------------------------------------------------------------------
// Generic key-value helpers
// ---------------------------------------------------------------------------

// SetKV inserts or updates a plain key-value pair in app_config.
func (dao *ConfigDAO) SetKV(key, value string) error {
	_, err := dao.db.Exec(`
		INSERT INTO app_config (config_key, config_value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(config_key) DO UPDATE SET
			config_value = excluded.config_value,
			updated_at   = CURRENT_TIMESTAMP`,
		key, value)
	if err != nil {
		return fmt.Errorf("SetKV %q: %w", key, err)
	}
	return nil
}

// GetKV retrieves a plain config value by key.
// Returns sql.ErrNoRows if the key does not exist.
func (dao *ConfigDAO) GetKV(key string) (string, error) {
	var value string
	err := dao.db.QueryRow(
		`SELECT config_value FROM app_config WHERE config_key = ?`, key,
	).Scan(&value)
	return value, err
}

// ---------------------------------------------------------------------------
// Watermark helpers
// ---------------------------------------------------------------------------

// GetWatermark retrieves a timestamp watermark. Returns zero time if missing.
func (dao *ConfigDAO) GetWatermark(key string) (time.Time, error) {
	val, err := dao.GetKV(key)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse("2006-01-02 15:04:05", val)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid watermark format for %q: %w", key, err)
	}
	return t.UTC(), nil
}

// SetWatermark persists a UTC timestamp watermark.
func (dao *ConfigDAO) SetWatermark(key string, t time.Time) error {
	return dao.SetKV(key, t.UTC().Format("2006-01-02 15:04:05"))
}

// ---------------------------------------------------------------------------
// Encrypted WABA credential helpers
// ---------------------------------------------------------------------------

// SetWABACredentials encrypts and persists the three WABA credential fields.
// All three values must be non-empty. When an encryption key is configured
// each value is independently encrypted with AES-256-GCM; otherwise the
// values are stored as plain text (intended for tests only).
func (dao *ConfigDAO) SetWABACredentials(creds WABACredentials) error {
	if creds.WABAID == "" || creds.PhoneNumberID == "" || creds.AccessToken == "" {
		return errors.New("SetWABACredentials: all credential fields are required")
	}

	pairs := map[string]string{
		keyWABAID:          creds.WABAID,
		keyPhoneNumberID:   creds.PhoneNumberID,
		keyWABAAccessToken: creds.AccessToken,
	}

	for k, v := range pairs {
		stored, err := dao.encrypt(v)
		if err != nil {
			return fmt.Errorf("SetWABACredentials: encrypting %q: %w", k, err)
		}
		if err := dao.SetKV(k, stored); err != nil {
			return err
		}
	}
	return nil
}

// GetWABACredentials retrieves and decrypts the stored WABA credentials.
// Returns ErrCredentialsNotFound if any field has not been set yet.
func (dao *ConfigDAO) GetWABACredentials() (WABACredentials, error) {
	get := func(key string) (string, error) {
		raw, err := dao.GetKV(key)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrCredentialsNotFound
		}
		if err != nil {
			return "", err
		}
		return dao.decrypt(raw)
	}

	wabaID, err := get(keyWABAID)
	if err != nil {
		return WABACredentials{}, err
	}
	phoneID, err := get(keyPhoneNumberID)
	if err != nil {
		return WABACredentials{}, err
	}
	token, err := get(keyWABAAccessToken)
	if err != nil {
		return WABACredentials{}, err
	}

	return WABACredentials{
		WABAID:        wabaID,
		PhoneNumberID: phoneID,
		AccessToken:   token,
	}, nil
}

// ClearWABACredentials removes all stored WABA credential keys.
func (dao *ConfigDAO) ClearWABACredentials() error {
	for _, key := range []string{keyWABAID, keyPhoneNumberID, keyWABAAccessToken} {
		if _, err := dao.db.Exec(
			`DELETE FROM app_config WHERE config_key = ?`, key,
		); err != nil {
			return fmt.Errorf("ClearWABACredentials: removing %q: %w", key, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// AES-256-GCM helpers (internal)
// ---------------------------------------------------------------------------

// encrypt returns the plaintext unchanged when no key is set, or an
// AES-256-GCM ciphertext encoded as base64 when a key is configured.
func (dao *ConfigDAO) encrypt(plaintext string) (string, error) {
	if len(dao.encryptKey) == 0 {
		return plaintext, nil
	}

	block, err := aes.NewCipher(dao.encryptKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// Seal appends ciphertext+tag to nonce; we store nonce||ciphertext as base64.
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt reverses encrypt. When no key is set the value is returned as-is.
func (dao *ConfigDAO) decrypt(encoded string) (string, error) {
	if len(dao.encryptKey) == 0 {
		return encoded, nil
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decrypt: base64 decode: %w", err)
	}

	block, err := aes.NewCipher(dao.encryptKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("decrypt: ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: GCM open: %w", err)
	}
	return string(plaintext), nil
}

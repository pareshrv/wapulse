//go:build wholesaler

// Package wholesaler implements the DomainPlugin interface for the wholesaler
// client build.
//
// Current behaviour (v1 — attachment-only):
//   - Any file dropped into a watched directory is forwarded as a WhatsApp
//     message with the file attached in full. No parsing is performed.
//   - CustomerPhone and CustomerName are read from app_config so the
//     wholesaler can configure a default recipient during onboarding.
//     Later versions will parse the file to extract per-customer details.
//
// To compile this driver:
//
//	wails build -tags wholesaler
package wholesaler

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"wapulse/internal/db"
	"wapulse/internal/plugin"
)

// config keys used by this plugin.
const (
	cfgDefaultPhone        = "wholesaler.default_phone"
	cfgDefaultCustomerName = "wholesaler.default_customer_name"
)

// Plugin is the wholesaler DomainPlugin implementation.
type Plugin struct {
	configDAO *db.ConfigDAO
}

// New returns an uninitialised Plugin. Init must be called before use.
func New() *Plugin {
	return &Plugin{}
}

// Init stores the config DAO reference. Called once at app startup.
func (p *Plugin) Init(database *sql.DB) error {
	p.configDAO = db.NewConfigDAO(database)
	return nil
}

// OnFileDiscovered is called by the ingestion gate when a new, stable file
// appears in a watched directory. It returns a single ExtractedEvent that
// carries the full file as an attachment — no content parsing yet.
//
// The idempotency key is derived from the SHA-256 hash of the file contents
// combined with the event type, so the same file re-appearing after a copy
// or restart is safely deduplicated.
func (p *Plugin) OnFileDiscovered(filePath string) ([]plugin.ExtractedEvent, error) {
	// Read the file to compute a content hash for idempotency.
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("wholesaler: open %q: %w", filePath, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("wholesaler: hashing %q: %w", filePath, err)
	}
	contentHash := fmt.Sprintf("%x", h.Sum(nil))

	// Look up the configured default recipient.
	phone, err := p.configDAO.GetKV(cfgDefaultPhone)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("wholesaler: reading default phone: %w", err)
	}
	if phone == "" {
		// Fail loudly — the wholesaler must configure a recipient before the
		// pipeline can send anything.
		return nil, fmt.Errorf("wholesaler: default recipient phone not configured; " +
			"set config key %q via the setup screen", cfgDefaultPhone)
	}

	customerName, err := p.configDAO.GetKV(cfgDefaultCustomerName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("wholesaler: reading default customer name: %w", err)
	}
	if customerName == "" {
		customerName = "Customer" // safe fallback
	}

	event := plugin.ExtractedEvent{
		// Idempotency key: content hash + event type ensures the same physical
		// file is never processed twice, even across restarts.
		IdempotencyKey: fmt.Sprintf("WHOLESALER_BILL:%s", contentHash),
		EventType:      "WHOLESALER_BILL",
		CustomerPhone:  phone,

		// Attach the file in full — no parsing in this version.
		AttachmentPath: filePath,

		Data: map[string]string{
			"CustomerName": customerName,
			"FileName":     filepath.Base(filePath),
			// ContentHash is carried in Data so the template or audit log can
			// reference it; it is not used as a template variable by default.
			"ContentHash": contentHash,
		},
	}

	return []plugin.ExtractedEvent{event}, nil
}

// OnScheduledTrigger handles recurring reminder rules for the wholesaler.
// Not used in the attachment-only v1 — returns empty slice gracefully.
func (p *Plugin) OnScheduledTrigger(rule db.ScheduledRule) ([]plugin.ExtractedEvent, error) {
	// No scheduled rules defined yet for the wholesaler plugin.
	return nil, nil
}

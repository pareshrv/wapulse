package db

import (
	"database/sql"
	"fmt"
)

// Migrate initializes the v3 database schema.
// Changes from v2:
//   - message_outbox: added template_name, template_vars_json columns and
//     AWAITING_TEMPLATE status; message_body is now nullable (not needed for
//     template sends).
//   - processed_files: new table — SHA-256 deduplication ledger for the
//     ingestion gate.
//   - message_templates: new table — Meta approval lifecycle for all
//     outbound message templates (DRAFT → PENDING_REVIEW → APPROVED | REJECTED).
func Migrate(db *sql.DB) error {
	schema := `
	-- 1. Persistent app configuration, watermarks, and per-client WABA credentials.
	--    Credentials (waba_id, phone_number_id, access token) are stored here
	--    encrypted at rest (see task 7.1.6 / Design Doc v3 §8).
	CREATE TABLE IF NOT EXISTS app_config (
		config_key   TEXT PRIMARY KEY,
		config_value TEXT NOT NULL,
		updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 2. Unified action outbox queue (v3: template-based WhatsApp sends).
	--    action_type values : 'WHATSAPP_TEMPLATE' | 'DESKTOP_NOTIFICATION'
	--    status values      : 'PENDING' | 'SENT' | 'FAILED_PERMANENT' | 'AWAITING_TEMPLATE'
	--    template_name / template_vars_json are populated for WHATSAPP_TEMPLATE rows.
	--    message_body is retained for DESKTOP_NOTIFICATION rows and local audit log.
	CREATE TABLE IF NOT EXISTS message_outbox (
		id                 INTEGER PRIMARY KEY AUTOINCREMENT,
		idempotency_key    TEXT UNIQUE NOT NULL,
		action_type        TEXT NOT NULL DEFAULT 'WHATSAPP_TEMPLATE',
		customer_phone     TEXT,
		template_name      TEXT,        -- Meta-approved template name (WHATSAPP_TEMPLATE only)
		template_vars_json TEXT,        -- JSON array of ordered variable values for the template
		message_body       TEXT,        -- Rendered text for DESKTOP_NOTIFICATION / local log
		payload_path       TEXT,        -- PDF/media path for media attachment sends
		status             TEXT NOT NULL DEFAULT 'PENDING',
		retry_count        INTEGER NOT NULL DEFAULT 0,
		next_retry_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		error_log          TEXT,
		created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		sent_at            DATETIME
	);

	CREATE INDEX IF NOT EXISTS idx_outbox_queue
		ON message_outbox(status, next_retry_at);

	-- 3. Scheduled and recurring notification rules engine (unchanged from v2).
	--    status values: 'SCHEDULED' | 'EXECUTED' | 'CANCELLED'
	CREATE TABLE IF NOT EXISTS scheduled_notifications (
		id                   INTEGER PRIMARY KEY AUTOINCREMENT,
		rule_key             TEXT NOT NULL,
		metadata_json        TEXT DEFAULT '{}',
		scheduled_for        DATETIME NOT NULL,
		repeat_interval_days INTEGER NOT NULL DEFAULT 0,
		status               TEXT NOT NULL DEFAULT 'SCHEDULED',
		created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_pending_schedules
		ON scheduled_notifications(status, scheduled_for);

	-- 4. Idempotent file ingestion ledger (new in v3).
	--    Each processed file is recorded by its SHA-256 content hash so that
	--    duplicate file events (restarts, re-copies) are rejected before the
	--    plugin runs.
	CREATE TABLE IF NOT EXISTS processed_files (
		file_hash    TEXT PRIMARY KEY,
		file_path    TEXT NOT NULL,
		event_type   TEXT,
		processed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- 5. Message templates with Meta approval lifecycle (new in v3).
	--    approval_status values: 'DRAFT' | 'PENDING_REVIEW' | 'APPROVED' | 'REJECTED'
	--    Only APPROVED templates are used by the outbox worker; events whose
	--    template is not yet APPROVED are held as AWAITING_TEMPLATE in the outbox.
	CREATE TABLE IF NOT EXISTS message_templates (
		id                 INTEGER PRIMARY KEY AUTOINCREMENT,
		event_type         TEXT NOT NULL UNIQUE,   -- matches ExtractedEvent.EventType
		meta_template_name TEXT,                   -- name registered with Meta
		language_code      TEXT NOT NULL DEFAULT 'en',
		category           TEXT,                   -- 'UTILITY' | 'MARKETING'
		body_text          TEXT NOT NULL,           -- template body with {{1}}, {{2}} placeholders
		variable_order     TEXT,                    -- JSON array mapping {{n}} -> ExtractedEvent.Data key
		approval_status    TEXT NOT NULL DEFAULT 'DRAFT',
		rejection_reason   TEXT,
		updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to execute database migrations: %w", err)
	}

	return nil
}

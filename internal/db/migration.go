package db

import (
	"database/sql"
	"fmt"
)

// Migrate initializes the core database schema required for the 3 pipelines.
func Migrate(db *sql.DB) error {
	schema := `
	-- 1. Persistent App Configuration & Watermarks
	CREATE TABLE IF NOT EXISTS app_config (
		config_key TEXT PRIMARY KEY,
		config_value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 2. Unified Action Outbox Queue (WhatsApp & Desktop Notifications)
	CREATE TABLE IF NOT EXISTS message_outbox (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		idempotency_key TEXT UNIQUE NOT NULL,
		action_type TEXT NOT NULL DEFAULT 'WHATSAPP', -- 'WHATSAPP' or 'DESKTOP_NOTIFICATION'
		customer_phone TEXT NOT NULL,                 -- Phone number or 'LOCAL_DESKTOP'
		message_body TEXT NOT NULL,
		payload_path TEXT,                            -- PDF/Media path if applicable
		status TEXT DEFAULT 'PENDING',                -- 'PENDING', 'SENT', 'FAILED_PERMANENT'
		retry_count INTEGER DEFAULT 0,
		next_retry_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		error_log TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		sent_at DATETIME
	);

	CREATE INDEX IF NOT EXISTS idx_outbox_queue ON message_outbox(status, next_retry_at);

	-- 3. Scheduled & Recurring Rules Engine
	CREATE TABLE IF NOT EXISTS scheduled_notifications (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		rule_key TEXT NOT NULL,                        -- Identified by Plugin
		metadata_json TEXT DEFAULT '{}',               -- Dynamic parameters for Plugin
		scheduled_for DATETIME NOT NULL,               -- Next trigger execution timestamp
		repeat_interval_days INTEGER DEFAULT 0,       -- 0 = One-time, 15 = Every 15 days
		status TEXT DEFAULT 'SCHEDULED',               -- 'SCHEDULED', 'EXECUTED', 'CANCELLED'
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_pending_schedules ON scheduled_notifications(status, scheduled_for);
	`

	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to execute database migrations: %w", err)
	}

	return nil
}

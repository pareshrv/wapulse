// Package plugin defines the contract between WAPulse's core pipeline and
// any client-specific domain driver.
//
// The interface is intentionally narrow: plugins extract structured data from
// files or scheduled triggers and return it as ExtractedEvents. They never
// build message text — that responsibility belongs to the template engine
// (internal/template). This separation means changing what a message says
// only requires editing a Meta-approved template in the UI, not a rebuild.
package plugin

import (
	"database/sql"

	"wapulse/internal/db"
)

// ExtractedEvent is the data contract between a domain plugin and the rest
// of the pipeline. It carries enough structured information for the template
// engine to render a Cloud API send call, and for the ingestion gate to
// deduplicate and atomically enqueue the resulting outbox action.
type ExtractedEvent struct {
	// IdempotencyKey is a stable, content-derived identifier for this event
	// (e.g. SHA-256 of the source file + event type). The outbox uses it to
	// guarantee exactly-once insertion.
	IdempotencyKey string

	// EventType identifies which message template to look up in
	// message_templates (e.g. "WHOLESALER_BILL", "DOCTOR_VISIT_REMINDER").
	// Must match a row's event_type column exactly.
	EventType string

	// CustomerPhone is the recipient's phone number in E.164 format
	// (e.g. "+919876543210"). Required for WHATSAPP_TEMPLATE actions.
	CustomerPhone string

	// AttachmentPath is the local file path of a PDF or image to upload as
	// a media attachment. Empty string means no attachment.
	AttachmentPath string

	// Data maps logical field names to their string values for this event.
	// The template engine uses variable_order from message_templates to map
	// these fields to the {{1}}, {{2}}, ... placeholders Meta expects.
	// Example: {"CustomerName": "Ravi Sharma", "Balance": "₹4,200"}
	Data map[string]string
}

// DomainPlugin is the interface every client driver must implement.
// Drivers are compiled in via Go build tags (see internal/plugin/registry_*.go)
// so only one driver is ever present in a given binary.
type DomainPlugin interface {
	// Init is called once at application startup. The plugin receives the
	// shared database connection so it can read config, seed default
	// templates, or set up any plugin-specific state it needs.
	Init(database *sql.DB) error

	// OnFileDiscovered is called by the ingestion gate (internal/watcher)
	// when a new, stable, not-yet-processed file is found in a watched
	// directory. The plugin parses the file and returns one or more
	// ExtractedEvents — one per distinct action to take (e.g. a bill file
	// might produce one WhatsApp send + one desktop notification).
	// Returning an empty slice with no error is valid (file was irrelevant).
	OnFileDiscovered(filePath string) ([]ExtractedEvent, error)

	// OnScheduledTrigger is called by the 30-second scheduler ticker
	// (internal/scheduler) when a due rule is fetched from
	// scheduled_notifications. The plugin uses the rule's metadata to
	// produce the appropriate reminder events.
	OnScheduledTrigger(rule db.ScheduledRule) ([]ExtractedEvent, error)
}

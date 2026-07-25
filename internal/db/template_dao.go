package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Template approval lifecycle statuses.
const (
	TemplateStatusDraft         = "DRAFT"
	TemplateStatusPendingReview = "PENDING_REVIEW"
	TemplateStatusApproved      = "APPROVED"
	TemplateStatusRejected      = "REJECTED"
)

// MessageTemplate represents a row in the message_templates table.
type MessageTemplate struct {
	ID               int64
	EventType        string // matches ExtractedEvent.EventType
	MetaTemplateName string // name registered with Meta
	LanguageCode     string
	Category         string // 'UTILITY' | 'MARKETING'
	BodyText         string // template body with {{1}}, {{2}} placeholders
	VariableOrder    string // JSON array mapping {{n}} -> ExtractedEvent.Data key
	ApprovalStatus   string // DRAFT | PENDING_REVIEW | APPROVED | REJECTED
	RejectionReason  string
	UpdatedAt        time.Time
}

// TemplateDAO manages queries for the message_templates table.
type TemplateDAO struct {
	db *sql.DB
}

// NewTemplateDAO creates a new TemplateDAO instance.
func NewTemplateDAO(db *sql.DB) *TemplateDAO {
	return &TemplateDAO{db: db}
}

// GetTemplate retrieves a template by its event type.
// Returns sql.ErrNoRows if no template exists for that event type.
func (dao *TemplateDAO) GetTemplate(eventType string) (*MessageTemplate, error) {
	row := dao.db.QueryRow(`
		SELECT id, event_type,
		       COALESCE(meta_template_name, ''),
		       language_code,
		       COALESCE(category, ''),
		       body_text,
		       COALESCE(variable_order, ''),
		       approval_status,
		       COALESCE(rejection_reason, ''),
		       updated_at
		FROM message_templates
		WHERE event_type = ?`, eventType)

	var t MessageTemplate
	var updatedAtStr string
	err := row.Scan(
		&t.ID, &t.EventType, &t.MetaTemplateName, &t.LanguageCode,
		&t.Category, &t.BodyText, &t.VariableOrder,
		&t.ApprovalStatus, &t.RejectionReason, &updatedAtStr,
	)
	if err != nil {
		return nil, err
	}
	t.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAtStr)
	return &t, nil
}

// UpsertTemplate inserts or replaces a template row keyed on event_type.
// Use this when seeding default templates (task 3.2.2) or when the user
// edits a template body in the UI before resubmitting to Meta.
func (dao *TemplateDAO) UpsertTemplate(t *MessageTemplate) error {
	if t.EventType == "" {
		return fmt.Errorf("UpsertTemplate: EventType is required")
	}
	if t.BodyText == "" {
		return fmt.Errorf("UpsertTemplate: BodyText is required")
	}
	if t.ApprovalStatus == "" {
		t.ApprovalStatus = TemplateStatusDraft
	}
	if t.LanguageCode == "" {
		t.LanguageCode = "en"
	}

	_, err := dao.db.Exec(`
		INSERT INTO message_templates
			(event_type, meta_template_name, language_code, category,
			 body_text, variable_order, approval_status, rejection_reason, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(event_type) DO UPDATE SET
			meta_template_name = excluded.meta_template_name,
			language_code      = excluded.language_code,
			category           = excluded.category,
			body_text          = excluded.body_text,
			variable_order     = excluded.variable_order,
			approval_status    = excluded.approval_status,
			rejection_reason   = excluded.rejection_reason,
			updated_at         = CURRENT_TIMESTAMP`,
		t.EventType, t.MetaTemplateName, t.LanguageCode, t.Category,
		t.BodyText, t.VariableOrder, t.ApprovalStatus, t.RejectionReason,
	)
	if err != nil {
		return fmt.Errorf("UpsertTemplate %q: %w", t.EventType, err)
	}
	return nil
}

// UpdateApprovalStatus updates the approval_status (and optional rejection
// reason) for a template. Called when Meta's approval poll or webhook
// returns a result for a PENDING_REVIEW template.
func (dao *TemplateDAO) UpdateApprovalStatus(eventType, status, rejectionReason string) error {
	validStatuses := map[string]bool{
		TemplateStatusDraft:         true,
		TemplateStatusPendingReview: true,
		TemplateStatusApproved:      true,
		TemplateStatusRejected:      true,
	}
	if !validStatuses[status] {
		return fmt.Errorf("UpdateApprovalStatus: invalid status %q", status)
	}

	_, err := dao.db.Exec(`
		UPDATE message_templates
		SET approval_status  = ?,
		    rejection_reason = ?,
		    updated_at       = CURRENT_TIMESTAMP
		WHERE event_type = ?`,
		status, rejectionReason, eventType,
	)
	if err != nil {
		return fmt.Errorf("UpdateApprovalStatus %q → %q: %w", eventType, status, err)
	}
	return nil
}

// ListApprovedTemplates returns all templates with APPROVED status.
// Used by the template engine (task 3.2.1) to confirm a template is ready
// before the outbox worker attempts a send.
func (dao *TemplateDAO) ListApprovedTemplates() ([]MessageTemplate, error) {
	rows, err := dao.db.Query(`
		SELECT id, event_type,
		       COALESCE(meta_template_name, ''),
		       language_code,
		       COALESCE(category, ''),
		       body_text,
		       COALESCE(variable_order, ''),
		       approval_status,
		       COALESCE(rejection_reason, ''),
		       updated_at
		FROM message_templates
		WHERE approval_status = ?
		ORDER BY event_type ASC`, TemplateStatusApproved)
	if err != nil {
		return nil, fmt.Errorf("ListApprovedTemplates: %w", err)
	}
	defer rows.Close()

	var templates []MessageTemplate
	for rows.Next() {
		var t MessageTemplate
		var updatedAtStr string
		if err := rows.Scan(
			&t.ID, &t.EventType, &t.MetaTemplateName, &t.LanguageCode,
			&t.Category, &t.BodyText, &t.VariableOrder,
			&t.ApprovalStatus, &t.RejectionReason, &updatedAtStr,
		); err != nil {
			return nil, fmt.Errorf("ListApprovedTemplates scan: %w", err)
		}
		t.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAtStr)
		templates = append(templates, t)
	}
	return templates, nil
}

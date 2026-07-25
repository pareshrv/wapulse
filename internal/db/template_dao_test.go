package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestTemplateDAO(t *testing.T) {
	tempDir := t.TempDir()
	database, err := InitDB(filepath.Join(tempDir, "template_test.db"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer database.Close()

	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	dao := NewTemplateDAO(database)

	// 1. GetTemplate on a non-existent event type returns sql.ErrNoRows.
	_, err = dao.GetTemplate("NONEXISTENT")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows for missing template, got: %v", err)
	}

	// 2. UpsertTemplate — insert a new DRAFT template.
	tmpl := &MessageTemplate{
		EventType:     "WHOLESALER_BILL",
		LanguageCode:  "en",
		Category:      "UTILITY",
		BodyText:      "Dear {{1}}, your balance is {{2}}.",
		VariableOrder: `["CustomerName","Balance"]`,
		ApprovalStatus: TemplateStatusDraft,
	}
	if err := dao.UpsertTemplate(tmpl); err != nil {
		t.Fatalf("UpsertTemplate (insert): %v", err)
	}

	got, err := dao.GetTemplate("WHOLESALER_BILL")
	if err != nil {
		t.Fatalf("GetTemplate after insert: %v", err)
	}
	if got.BodyText != tmpl.BodyText {
		t.Errorf("BodyText mismatch: expected %q, got %q", tmpl.BodyText, got.BodyText)
	}
	if got.ApprovalStatus != TemplateStatusDraft {
		t.Errorf("expected DRAFT status, got %q", got.ApprovalStatus)
	}

	// 3. UpsertTemplate — update the same event type.
	tmpl.BodyText = "Hi {{1}}, your outstanding balance is {{2}}. Please pay soon."
	tmpl.MetaTemplateName = "wholesaler_bill_v1"
	if err := dao.UpsertTemplate(tmpl); err != nil {
		t.Fatalf("UpsertTemplate (update): %v", err)
	}

	updated, err := dao.GetTemplate("WHOLESALER_BILL")
	if err != nil {
		t.Fatalf("GetTemplate after update: %v", err)
	}
	if updated.BodyText != tmpl.BodyText {
		t.Errorf("updated BodyText mismatch: expected %q, got %q", tmpl.BodyText, updated.BodyText)
	}
	if updated.MetaTemplateName != "wholesaler_bill_v1" {
		t.Errorf("MetaTemplateName mismatch: got %q", updated.MetaTemplateName)
	}

	// 4. UpdateApprovalStatus — advance through lifecycle.
	if err := dao.UpdateApprovalStatus("WHOLESALER_BILL", TemplateStatusPendingReview, ""); err != nil {
		t.Fatalf("UpdateApprovalStatus PENDING_REVIEW: %v", err)
	}
	if err := dao.UpdateApprovalStatus("WHOLESALER_BILL", TemplateStatusApproved, ""); err != nil {
		t.Fatalf("UpdateApprovalStatus APPROVED: %v", err)
	}

	approved, err := dao.GetTemplate("WHOLESALER_BILL")
	if err != nil {
		t.Fatalf("GetTemplate after approval: %v", err)
	}
	if approved.ApprovalStatus != TemplateStatusApproved {
		t.Errorf("expected APPROVED, got %q", approved.ApprovalStatus)
	}

	// 5. UpdateApprovalStatus — rejection records reason.
	if err := dao.UpsertTemplate(&MessageTemplate{
		EventType:      "DOCTOR_REMINDER",
		BodyText:       "Your appointment is on {{1}}.",
		ApprovalStatus: TemplateStatusPendingReview,
	}); err != nil {
		t.Fatalf("UpsertTemplate DOCTOR_REMINDER: %v", err)
	}
	if err := dao.UpdateApprovalStatus("DOCTOR_REMINDER", TemplateStatusRejected, "Variable missing in body"); err != nil {
		t.Fatalf("UpdateApprovalStatus REJECTED: %v", err)
	}

	rejected, err := dao.GetTemplate("DOCTOR_REMINDER")
	if err != nil {
		t.Fatalf("GetTemplate after rejection: %v", err)
	}
	if rejected.RejectionReason != "Variable missing in body" {
		t.Errorf("RejectionReason mismatch: got %q", rejected.RejectionReason)
	}

	// 6. UpdateApprovalStatus rejects unknown statuses.
	if err := dao.UpdateApprovalStatus("WHOLESALER_BILL", "UNKNOWN_STATUS", ""); err == nil {
		t.Error("expected error for invalid status, got nil")
	}

	// 7. ListApprovedTemplates returns only APPROVED rows.
	approved_list, err := dao.ListApprovedTemplates()
	if err != nil {
		t.Fatalf("ListApprovedTemplates: %v", err)
	}
	if len(approved_list) != 1 {
		t.Errorf("expected 1 approved template, got %d", len(approved_list))
	}
	if approved_list[0].EventType != "WHOLESALER_BILL" {
		t.Errorf("expected WHOLESALER_BILL in approved list, got %q", approved_list[0].EventType)
	}

	// 8. UpsertTemplate rejects empty EventType and empty BodyText.
	if err := dao.UpsertTemplate(&MessageTemplate{BodyText: "body"}); err == nil {
		t.Error("expected error for empty EventType")
	}
	if err := dao.UpsertTemplate(&MessageTemplate{EventType: "X"}); err == nil {
		t.Error("expected error for empty BodyText")
	}
}

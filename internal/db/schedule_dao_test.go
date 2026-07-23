package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestScheduleDAO(t *testing.T) {
	tempDir := t.TempDir()
	testDBPath := filepath.Join(tempDir, "test_schedule.db")

	database, err := InitDB(testDBPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	scheduleDAO := NewScheduleDAO(database)

	// Use UTC explicitly for time arithmetic
	pastTime := time.Now().UTC().Add(-10 * time.Second)
	rule := &ScheduledRule{
		RuleKey:            "DOCTOR_REMINDER",
		MetadataJSON:       `{"patient_phone": "+919999999999"}`,
		ScheduledFor:       pastTime,
		RepeatIntervalDays: 0,
	}

	ruleID, err := scheduleDAO.CreateScheduledRule(rule)
	if err != nil || ruleID == 0 {
		t.Fatalf("Failed to create scheduled rule: %v", err)
	}

	// 2. Fetch due rules
	dueRules, err := scheduleDAO.FetchDueRules()
	if err != nil || len(dueRules) != 1 {
		t.Fatalf("Expected 1 due rule, got %d: %v", len(dueRules), err)
	}

	// 3. Complete rule execution
	err = scheduleDAO.UpdateRuleStatus(&dueRules[0])
	if err != nil {
		t.Fatalf("Failed to update rule status: %v", err)
	}

	// Verify no rules remain due
	dueAfterUpdate, err := scheduleDAO.FetchDueRules()
	if err != nil || len(dueAfterUpdate) != 0 {
		t.Errorf("Expected 0 due rules, got %d", len(dueAfterUpdate))
	}
}

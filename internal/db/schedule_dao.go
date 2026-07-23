package db

import (
	"database/sql"
	"fmt"
	"time"
)

// ScheduledRule represents a row in the scheduled_notifications table.
type ScheduledRule struct {
	ID                 int64
	RuleKey            string // Domain identifier (e.g., 'VILLAGE_A_RECURRING', 'DOCTOR_FOLLOWUP')
	MetadataJSON       string // Dynamic parameters passed to the plugin
	ScheduledFor       time.Time
	RepeatIntervalDays int // 0 = One-time, >0 = Recurring interval in days
	Status             string // 'SCHEDULED', 'EXECUTED', 'CANCELLED'
	CreatedAt          time.Time
}

// ScheduleDAO manages queries for the scheduled_notifications table.
type ScheduleDAO struct {
	db *sql.DB
}

// NewScheduleDAO creates a new ScheduleDAO instance.
func NewScheduleDAO(db *sql.DB) *ScheduleDAO {
	return &ScheduleDAO{db: db}
}

// CreateScheduledRule inserts a new scheduled rule into the database.
func (dao *ScheduleDAO) CreateScheduledRule(rule *ScheduledRule) (int64, error) {
	query := `
	INSERT INTO scheduled_notifications (rule_key, metadata_json, scheduled_for, repeat_interval_days, status)
	VALUES (?, ?, ?, ?, 'SCHEDULED');
	`
	// Always format using UTC for SQLite time comparison compatibility
	scheduledForStr := rule.ScheduledFor.UTC().Format("2006-01-02 15:04:05")
	res, err := dao.db.Exec(query, rule.RuleKey, rule.MetadataJSON, scheduledForStr, rule.RepeatIntervalDays)
	if err != nil {
		return 0, fmt.Errorf("failed to create scheduled rule: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	return id, nil
}

// FetchDueRules returns all active rules whose scheduled_for time is due (<= CURRENT_TIMESTAMP).
func (dao *ScheduleDAO) FetchDueRules() ([]ScheduledRule, error) {
	query := `
	SELECT id, rule_key, COALESCE(metadata_json, '{}'), scheduled_for, repeat_interval_days, status, created_at
	FROM scheduled_notifications
	WHERE status = 'SCHEDULED' AND scheduled_for <= CURRENT_TIMESTAMP
	ORDER BY scheduled_for ASC;
	`
	rows, err := dao.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch due scheduled rules: %w", err)
	}
	defer rows.Close()

	var rules []ScheduledRule
	for rows.Next() {
		var rule ScheduledRule
		var scheduledForStr, createdAtStr string
		err := rows.Scan(
			&rule.ID, &rule.RuleKey, &rule.MetadataJSON,
			&scheduledForStr, &rule.RepeatIntervalDays, &rule.Status, &createdAtStr,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan scheduled rule: %w", err)
		}

		rule.ScheduledFor, _ = time.Parse("2006-01-02 15:04:05", scheduledForStr)
		rule.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
		rules = append(rules, rule)
	}

	return rules, nil
}

// UpdateRuleStatus completes a rule execution. If recurring, it advances the scheduled_for timestamp.
func (dao *ScheduleDAO) UpdateRuleStatus(rule *ScheduledRule) error {
	if rule.RepeatIntervalDays > 0 {
		// Calculate next execution time by adding repeat_interval_days in UTC
		nextSchedule := rule.ScheduledFor.UTC().AddDate(0, 0, rule.RepeatIntervalDays).Format("2006-01-02 15:04:05")
		query := `
		UPDATE scheduled_notifications
		SET scheduled_for = ?
		WHERE id = ?;
		`
		_, err := dao.db.Exec(query, nextSchedule, rule.ID)
		if err != nil {
			return fmt.Errorf("failed to reschedule recurring rule: %w", err)
		}
		return nil
	}

	// For one-time rules, mark status as EXECUTED
	query := `
	UPDATE scheduled_notifications
	SET status = 'EXECUTED'
	WHERE id = ?;
	`
	_, err := dao.db.Exec(query, rule.ID)
	if err != nil {
		return fmt.Errorf("failed to mark rule as executed: %w", err)
	}

	return nil
}

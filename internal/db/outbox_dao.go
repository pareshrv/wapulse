package db

import (
	"database/sql"
	"fmt"
	"time"
)

// ActionItem represents a queued action in the message_outbox table.
type ActionItem struct {
	ID             int64
	IdempotencyKey string
	ActionType     string // 'WHATSAPP' or 'DESKTOP_NOTIFICATION'
	CustomerPhone  string
	MessageBody    string
	PayloadPath    string
	Status         string
	RetryCount     int
	NextRetryAt    time.Time
	ErrorLog       string
	CreatedAt      time.Time
	SentAt         *time.Time
}

// OutboxDAO manages queries for the message_outbox table.
type OutboxDAO struct {
	db *sql.DB
}

// NewOutboxDAO creates a new OutboxDAO instance.
func NewOutboxDAO(db *sql.DB) *OutboxDAO {
	return &OutboxDAO{db: db}
}

// EnqueueAction inserts a new action into the outbox. Duplicate idempotency keys are ignored.
func (dao *OutboxDAO) EnqueueAction(item *ActionItem) (bool, error) {
	query := `
	INSERT OR IGNORE INTO message_outbox 
		(idempotency_key, action_type, customer_phone, message_body, payload_path, status)
	VALUES (?, ?, ?, ?, ?, 'PENDING');
	`
	res, err := dao.db.Exec(query, item.IdempotencyKey, item.ActionType, item.CustomerPhone, item.MessageBody, item.PayloadPath)
	if err != nil {
		return false, fmt.Errorf("failed to enqueue outbox action: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	return rows > 0, nil
}

// FetchPendingActions fetches up to limit pending actions whose next_retry_at is due.
func (dao *OutboxDAO) FetchPendingActions(limit int) ([]ActionItem, error) {
	query := `
	SELECT id, idempotency_key, action_type, customer_phone, message_body, 
	       COALESCE(payload_path, ''), status, retry_count, next_retry_at, COALESCE(error_log, '')
	FROM message_outbox
	WHERE status = 'PENDING' AND next_retry_at <= CURRENT_TIMESTAMP
	ORDER BY id ASC
	LIMIT ?;
	`
	rows, err := dao.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pending actions: %w", err)
	}
	defer rows.Close()

	var items []ActionItem
	for rows.Next() {
		var item ActionItem
		var nextRetryStr string
		err := rows.Scan(
			&item.ID, &item.IdempotencyKey, &item.ActionType, &item.CustomerPhone,
			&item.MessageBody, &item.PayloadPath, &item.Status, &item.RetryCount,
			&nextRetryStr, &item.ErrorLog,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan outbox item: %w", err)
		}

		item.NextRetryAt, _ = time.Parse("2006-01-02 15:04:05", nextRetryStr)
		items = append(items, item)
	}

	return items, nil
}

// MarkActionSent updates an item's status to SENT.
func (dao *OutboxDAO) MarkActionSent(id int64) error {
	query := `
	UPDATE message_outbox
	SET status = 'SENT', sent_at = CURRENT_TIMESTAMP
	WHERE id = ?;
	`
	_, err := dao.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to mark action as sent: %w", err)
	}
	return nil
}

// MarkActionFailedWithBackoff schedules a retry with exponential backoff, or marks as FAILED_PERMANENT if max retries exceeded.
func (dao *OutboxDAO) MarkActionFailedWithBackoff(id int64, currentRetries int, maxRetries int, errLog string) error {
	if currentRetries+1 >= maxRetries {
		query := `
		UPDATE message_outbox
		SET status = 'FAILED_PERMANENT', error_log = ?
		WHERE id = ?;
		`
		_, err := dao.db.Exec(query, errLog, id)
		return err
	}

	// Calculate exponential backoff: 2^retry_count * 1 minute
	backoffMinutes := 1 << currentRetries
	nextRetryAt := time.Now().Add(time.Duration(backoffMinutes) * time.Minute).Format("2006-01-02 15:04:05")

	query := `
	UPDATE message_outbox
	SET retry_count = retry_count + 1,
	    next_retry_at = ?,
	    error_log = ?
	WHERE id = ?;
	`
	_, err := dao.db.Exec(query, nextRetryAt, errLog, id)
	return err
}

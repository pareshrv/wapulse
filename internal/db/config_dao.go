package db

import (
	"database/sql"
	"fmt"
	"time"
)

// ConfigDAO manages key-value configurations and high-watermark timestamps in the app_config table.
type ConfigDAO struct {
	db *sql.DB
}

// NewConfigDAO creates a new ConfigDAO instance.
func NewConfigDAO(db *sql.DB) *ConfigDAO {
	return &ConfigDAO{db: db}
}

// SetKV inserts or updates a key-value configuration pair in app_config.
func (dao *ConfigDAO) SetKV(key string, value string) error {
	query := `
	INSERT INTO app_config (config_key, config_value, updated_at)
	VALUES (?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(config_key) DO UPDATE SET
		config_value = excluded.config_value,
		updated_at = CURRENT_TIMESTAMP;
	`
	_, err := dao.db.Exec(query, key, value)
	if err != nil {
		return fmt.Errorf("failed to set config key '%s': %w", key, err)
	}
	return nil
}

// GetKV retrieves a configuration value by key. Returns sql.ErrNoRows if key doesn't exist.
func (dao *ConfigDAO) GetKV(key string) (string, error) {
	query := `SELECT config_value FROM app_config WHERE config_key = ?;`
	var value string
	err := dao.db.QueryRow(query, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// GetWatermark retrieves a high-watermark timestamp by key. Returns zero time.Time if missing.
func (dao *ConfigDAO) GetWatermark(key string) (time.Time, error) {
	val, err := dao.GetKV(key)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}

	parsedTime, err := time.Parse("2006-01-02 15:04:05", val)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid watermark timestamp format for key '%s': %w", key, err)
	}

	return parsedTime.UTC(), nil
}

// SetWatermark updates a high-watermark timestamp stored in UTC.
func (dao *ConfigDAO) SetWatermark(key string, t time.Time) error {
	timeStr := t.UTC().Format("2006-01-02 15:04:05")
	return dao.SetKV(key, timeStr)
}

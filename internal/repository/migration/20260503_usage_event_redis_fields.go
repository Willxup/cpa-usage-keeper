package migration

import (
	"encoding/json"
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
)

func addUsageEventRedisFieldsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&models.UsageEvent{}) {
		return nil
	}
	columns := []struct {
		name string
		sql  string
	}{
		{name: "provider", sql: "ALTER TABLE usage_events ADD COLUMN provider TEXT"},
		{name: "endpoint", sql: "ALTER TABLE usage_events ADD COLUMN endpoint TEXT"},
		{name: "auth_type", sql: "ALTER TABLE usage_events ADD COLUMN auth_type TEXT"},
		{name: "request_id", sql: "ALTER TABLE usage_events ADD COLUMN request_id TEXT"},
	}
	for _, column := range columns {
		if tx.Migrator().HasColumn(&models.UsageEvent{}, column.name) {
			continue
		}
		if err := tx.Exec(column.sql).Error; err != nil {
			return fmt.Errorf("add usage_events.%s column: %w", column.name, err)
		}
	}
	return nil
}

type redisUsageBackfillPayload struct {
	Provider  string `json:"provider"`
	Endpoint  string `json:"endpoint"`
	AuthType  string `json:"auth_type"`
	RequestID string `json:"request_id"`
}

func backfillUsageEventRedisFieldsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&models.UsageEvent{}) || !tx.Migrator().HasTable(&models.RedisUsageInbox{}) {
		return nil
	}
	for _, column := range []string{"provider", "endpoint", "auth_type", "request_id"} {
		if !tx.Migrator().HasColumn(&models.UsageEvent{}, column) {
			return nil
		}
	}

	var inboxRows []models.RedisUsageInbox
	return tx.Where("status = ?", redisUsageInboxStatusProcessed).
		Order("id asc").
		FindInBatches(&inboxRows, 500, func(_ *gorm.DB, _ int) error {
			for _, inbox := range inboxRows {
				var payload redisUsageBackfillPayload
				if err := json.Unmarshal([]byte(inbox.RawMessage), &payload); err != nil {
					continue
				}
				payload.Provider = strings.TrimSpace(payload.Provider)
				payload.Endpoint = strings.TrimSpace(payload.Endpoint)
				payload.AuthType = normalizeUsageEventRedisAuthType(payload.AuthType)
				payload.RequestID = strings.TrimSpace(payload.RequestID)
				if payload.Provider == "" && payload.Endpoint == "" && payload.AuthType == "" && payload.RequestID == "" {
					continue
				}
				if err := backfillUsageEventRedisFields(tx, strings.TrimSpace(inbox.UsageEventKey), payload, true); err != nil {
					return err
				}
			}
			return nil
		}).Error
}

func backfillUsageEventRedisFields(tx *gorm.DB, usageEventKey string, payload redisUsageBackfillPayload, allowRequestIDFallback bool) error {
	if usageEventKey == "" {
		if allowRequestIDFallback && payload.RequestID != "" {
			return backfillUsageEventRedisFields(tx, payload.RequestID, payload, false)
		}
		return nil
	}

	var event models.UsageEvent
	result := tx.Where("event_key = ?", usageEventKey).Limit(1).Find(&event)
	if result.Error != nil {
		return fmt.Errorf("load usage event %q for redis backfill: %w", usageEventKey, result.Error)
	}
	if result.RowsAffected == 0 {
		if allowRequestIDFallback && payload.RequestID != "" && payload.RequestID != usageEventKey {
			return backfillUsageEventRedisFields(tx, payload.RequestID, payload, false)
		}
		return nil
	}

	updates := map[string]any{}
	if strings.TrimSpace(event.Provider) == "" && payload.Provider != "" {
		updates["provider"] = payload.Provider
	}
	if strings.TrimSpace(event.Endpoint) == "" && payload.Endpoint != "" {
		updates["endpoint"] = payload.Endpoint
	}
	if strings.TrimSpace(event.AuthType) == "" && payload.AuthType != "" {
		updates["auth_type"] = payload.AuthType
	}
	if strings.TrimSpace(event.RequestID) == "" && payload.RequestID != "" {
		updates["request_id"] = payload.RequestID
	}
	if len(updates) == 0 {
		return nil
	}
	if err := tx.Model(&models.UsageEvent{}).Where("id = ?", event.ID).Updates(updates).Error; err != nil {
		return fmt.Errorf("backfill usage event %q redis fields: %w", event.EventKey, err)
	}
	return nil
}

func normalizeUsageEventRedisAuthType(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "api_key" {
		return "apikey"
	}
	return trimmed
}

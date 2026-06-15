package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

// CooldownUpsertResult 描述 UpsertOrExtendActiveCooldown 的写入结果，让调用方区分新建/延长/无变化。
type CooldownUpsertResult string

const (
	// CooldownUpsertCreated 表示新建了一条 active cooldown 记录，后续通常需要执行禁用。
	CooldownUpsertCreated CooldownUpsertResult = "created"
	// CooldownUpsertExtended 表示已存在 active 记录，recover_at 被延长，不需要重复禁用。
	CooldownUpsertExtended CooldownUpsertResult = "extended"
	// CooldownUpsertUnchanged 表示已存在 active 记录且 recover_at 未被更新（新值不晚于旧值）。
	CooldownUpsertUnchanged CooldownUpsertResult = "unchanged"
)

const authFileCooldownBatchSize = 50

// UpsertOrExtendActiveCooldown 创建或延长 cooldown 记录。
// recover_at 只延长不缩短：已存在 active cooldown 时，只有新 recover_at 更晚才更新。
// 按 auth_index + state = active 匹配（不限定 owner），确保同一 auth_index 只有一条 active cooldown。
// 返回写入结果（created/extended/unchanged），调用方据此判断是否需要执行禁用操作。
func UpsertOrExtendActiveCooldown(db *gorm.DB, cooldown *entities.AuthFileCooldown) (CooldownUpsertResult, error) {
	if db == nil {
		return "", fmt.Errorf("database is nil")
	}
	if cooldown == nil {
		return "", fmt.Errorf("cooldown is nil")
	}

	var existing entities.AuthFileCooldown
	result := db.Where("provider = ? AND auth_index = ? AND state = ?",
		cooldown.Provider, cooldown.AuthIndex, entities.AuthFileCooldownActive).
		First(&existing)

	if result.Error == nil {
		// 已存在 active 记录，只延长不缩短
		if !cooldown.RecoverAt.After(existing.RecoverAt) {
			return CooldownUpsertUnchanged, nil // 新 recover_at 不晚于已有值，不更新
		}
		existing.RecoverAt = timeutil.NormalizeStorageTime(cooldown.RecoverAt)
		existing.UpdatedAt = timeutil.NormalizeStorageTime(time.Now())
		// 更新来源信息
		existing.LastSource = existing.Source
		existing.Source = cooldown.Source
		if cooldown.Owner != "" {
			existing.Owner = cooldown.Owner
		}
		if cooldown.Reason != "" {
			existing.Reason = cooldown.Reason
		}
		if cooldown.UpstreamStatusCode != 0 {
			existing.UpstreamStatusCode = cooldown.UpstreamStatusCode
		}
		if cooldown.UpstreamMessage != "" {
			existing.UpstreamMessage = cooldown.UpstreamMessage
		}
		if cooldown.LastError != "" {
			existing.LastError = cooldown.LastError
		}
		if cooldown.LastErrorBody != "" {
			existing.LastErrorBody = boundedCooldownErrorBody(cooldown.LastErrorBody)
		}
		if cooldown.SourceEventKey != "" {
			existing.SourceEventKey = cooldown.SourceEventKey
		}
		if cooldown.SourceRequestID != "" {
			existing.SourceRequestID = cooldown.SourceRequestID
		}
		if err := db.Save(&existing).Error; err != nil {
			return "", err
		}
		cooldown.ID = existing.ID
		return CooldownUpsertExtended, nil
	}

	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("query existing cooldown: %w", result.Error)
	}

	// 不存在 active 记录，创建新记录
	cooldown.CreatedAt = timeutil.NormalizeStorageTime(time.Now())
	cooldown.UpdatedAt = cooldown.CreatedAt
	cooldown.RecoverAt = timeutil.NormalizeStorageTime(cooldown.RecoverAt)
	cooldown.LastErrorBody = boundedCooldownErrorBody(cooldown.LastErrorBody)
	if cooldown.Source == "" {
		cooldown.Source = entities.AuthFileCooldownSourceRequestEvent
	}
	if cooldown.Owner == "" {
		cooldown.Owner = entities.AuthFileCooldownOwnerUsage429
	}
	if cooldown.Reason == "" {
		cooldown.Reason = entities.AuthFileCooldownReasonCodex429
	}
	return CooldownUpsertCreated, db.Create(cooldown).Error
}

// ListDueCooldowns 查询到期待恢复的 cooldown 记录。
// state in (active, restore_failed) AND recover_at <= now。
// 当 maxAttempts > 0 时，排除 restore_failed 且 restore_attempts >= maxAttempts 的记录，避免饿死队列。
func ListDueCooldowns(db *gorm.DB, now time.Time, limit int, maxAttempts int) ([]entities.AuthFileCooldown, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if limit <= 0 {
		limit = authFileCooldownBatchSize
	}
	if limit > 100 {
		limit = 100
	}
	normalizedNow := timeutil.FormatStorageTime(now)
	var rows []entities.AuthFileCooldown
	query := db.Where("recover_at <= ?", normalizedNow)
	if maxAttempts > 0 {
		// active 状态全查；restore_failed 只查 restore_attempts < maxAttempts
		query = query.Where(
			"(state = ?) OR (state = ? AND restore_attempts < ?)",
			entities.AuthFileCooldownActive,
			entities.AuthFileCooldownRestoreFailed,
			maxAttempts,
		)
	} else {
		query = query.Where("state IN ?",
			[]entities.AuthFileCooldownState{entities.AuthFileCooldownActive, entities.AuthFileCooldownRestoreFailed},
		)
	}
	err := query.Order("recover_at ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list due cooldowns: %w", err)
	}
	return rows, nil
}

// GetActiveCooldownByAuthIndex 查询指定 auth_index 的活跃 cooldown 记录（不限 owner）。
func GetActiveCooldownByAuthIndex(db *gorm.DB, provider, authIndex string) (*entities.AuthFileCooldown, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	var cooldown entities.AuthFileCooldown
	err := db.Where("provider = ? AND auth_index = ? AND state = ?",
		provider, authIndex, entities.AuthFileCooldownActive).
		First(&cooldown).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active cooldown by auth index: %w", err)
	}
	return &cooldown, nil
}

// MarkDisabled 标记 cooldown 为已禁用。
func MarkDisabled(db *gorm.DB, id int64) error {
	return updateAuthFileCooldown(db, id, map[string]any{
		"disabled_by_keeper": true,
		"disabled_at":        timeutil.FormatStorageTime(time.Now()),
		"updated_at":         timeutil.FormatStorageTime(time.Now()),
	})
}

// MarkDisableFailed 标记 cooldown 禁用失败。
func MarkDisableFailed(db *gorm.DB, id int64, lastError string, lastErrorBody string) error {
	return updateAuthFileCooldown(db, id, map[string]any{
		"state":           entities.AuthFileCooldownDisableFailed,
		"last_error":      lastError,
		"last_error_body": boundedCooldownErrorBody(lastErrorBody),
		"updated_at":      timeutil.FormatStorageTime(time.Now()),
	})
}

// MarkRecovered 标记 cooldown 为已恢复。
func MarkRecovered(db *gorm.DB, id int64) error {
	now := timeutil.NormalizeStorageTime(time.Now())
	return updateAuthFileCooldown(db, id, map[string]any{
		"state":        entities.AuthFileCooldownRecovered,
		"recovered_at": timeutil.FormatStorageTime(now),
		"updated_at":   timeutil.FormatStorageTime(now),
		"last_error":   "",
	})
}

// MarkRecoveredExternal 标记 cooldown 为外部已恢复（auth file 已被其他系统启用）。
func MarkRecoveredExternal(db *gorm.DB, id int64) error {
	now := timeutil.NormalizeStorageTime(time.Now())
	return updateAuthFileCooldown(db, id, map[string]any{
		"state":        entities.AuthFileCooldownRecoveredExt,
		"recovered_at": timeutil.FormatStorageTime(now),
		"updated_at":   timeutil.FormatStorageTime(now),
		"last_error":   "",
	})
}

// MarkRestoreFailed 标记 cooldown 恢复失败（增加尝试次数）。
func MarkRestoreFailed(db *gorm.DB, id int64, lastError string) error {
	return updateAuthFileCooldown(db, id, map[string]any{
		"state":            entities.AuthFileCooldownRestoreFailed,
		"restore_attempts": gorm.Expr("restore_attempts + ?", 1),
		"last_error":       lastError,
		"updated_at":       timeutil.FormatStorageTime(time.Now()),
	})
}

// MarkSkippedManual 标记 cooldown 跳过恢复（用户手动禁用的不恢复）。
func MarkSkippedManual(db *gorm.DB, id int64) error {
	now := timeutil.NormalizeStorageTime(time.Now())
	return updateAuthFileCooldown(db, id, map[string]any{
		"state":        entities.AuthFileCooldownSkippedManual,
		"recovered_at": timeutil.FormatStorageTime(now),
		"updated_at":   timeutil.FormatStorageTime(now),
	})
}

// MarkMissing 标记 cooldown 为 auth file 不存在。
func MarkMissing(db *gorm.DB, id int64) error {
	now := timeutil.NormalizeStorageTime(time.Now())
	return updateAuthFileCooldown(db, id, map[string]any{
		"state":        entities.AuthFileCooldownMissing,
		"recovered_at": timeutil.FormatStorageTime(now),
		"updated_at":   timeutil.FormatStorageTime(now),
	})
}

func updateAuthFileCooldown(db *gorm.DB, id int64, updates map[string]any) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	result := db.Model(&entities.AuthFileCooldown{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update cooldown %d: %w", id, result.Error)
	}
	return nil
}

func boundedCooldownErrorBody(body string) string {
	if body == "" {
		return ""
	}
	if len(body) > 2048 {
		return body[:2048]
	}
	return body
}

// ResolveCooldownRecoverAt 从已解析的输入计算恢复时间，按优先级 resets_at > resets_in_seconds。
// 解析不到有效时间返回 error；计算出的时间不晚于 now 也返回 error，避免写入已过期的 cooldown。
// recover_at 不晚于 now 是 cooldown 的基本前提：到期或已过期的 cooldown 没有存在意义。
func ResolveCooldownRecoverAt(now time.Time, resetsAt *time.Time, resetsInSeconds *int64) (time.Time, error) {
	var raw time.Time
	if resetsAt != nil && !resetsAt.IsZero() {
		raw = *resetsAt
	} else if resetsInSeconds != nil && *resetsInSeconds > 0 {
		raw = now.Add(time.Duration(*resetsInSeconds) * time.Second)
	} else {
		return time.Time{}, fmt.Errorf("no valid resets_at or resets_in_seconds")
	}
	normalized := timeutil.NormalizeStorageTime(raw)
	if !normalized.After(now) {
		return time.Time{}, fmt.Errorf("recover_at %s is not after now %s", normalized.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	return normalized, nil
}

// ListAllActiveCooldowns 返回所有 active 状态的 cooldown 记录，供 API 展示用。
func ListAllActiveCooldowns(db *gorm.DB) ([]entities.AuthFileCooldown, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	var rows []entities.AuthFileCooldown
	if err := db.Where("state = ?", entities.AuthFileCooldownActive).
		Order("recover_at ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list active cooldowns: %w", err)
	}
	return rows, nil
}

// ListActiveCooldownsByAuthIndexes 按 auth_index 列表批量查询 active/restore_failed 的 cooldown。
// limit 限制最多查询数量。用于 Auth Files 列表批量展示 cooldown 状态。
func ListActiveCooldownsByAuthIndexes(db *gorm.DB, authIndexes []string, limit int) (map[string]*entities.AuthFileCooldown, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if len(authIndexes) == 0 {
		return map[string]*entities.AuthFileCooldown{}, nil
	}
	if limit <= 0 {
		limit = 100
	}
	if len(authIndexes) > limit {
		authIndexes = authIndexes[:limit]
	}
	var rows []entities.AuthFileCooldown
	if err := db.Where("auth_index IN ? AND state IN ?",
		authIndexes,
		[]entities.AuthFileCooldownState{
			entities.AuthFileCooldownActive,
			entities.AuthFileCooldownRestoreFailed,
			entities.AuthFileCooldownDisableFailed,
			entities.AuthFileCooldownSkippedManual,
		},
	).Order("auth_index ASC, updated_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list cooldowns by auth indexes: %w", err)
	}
	result := make(map[string]*entities.AuthFileCooldown, len(rows))
	for i := range rows {
		result[rows[i].AuthIndex] = &rows[i]
	}
	return result, nil
}

// ListAllCooldowns 返回所有 cooldown 记录，供 API 展示用。
func ListAllCooldowns(db *gorm.DB, limit int) ([]entities.AuthFileCooldown, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if limit <= 0 {
		limit = 100
	}
	var rows []entities.AuthFileCooldown
	if err := db.Order("updated_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list cooldowns: %w", err)
	}
	return rows, nil
}

// CountActiveCooldowns 统计活跃 cooldown 数量。
func CountActiveCooldowns(db *gorm.DB) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	var count int64
	if err := db.Model(&entities.AuthFileCooldown{}).
		Where("state = ?", entities.AuthFileCooldownActive).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count active cooldowns: %w", err)
	}
	return count, nil
}

// DeleteAllAuthFileCooldowns 删除所有 cooldown 记录（用于测试清理）。
func DeleteAllAuthFileCooldowns(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	return db.Where("1 = 1").Delete(&entities.AuthFileCooldown{}).Error
}

// TruncateAuthFileCooldowns 清空 cooldown 表（仅用于测试）。
func TruncateAuthFileCooldowns(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	return db.Exec("DELETE FROM auth_file_cooldowns").Error
}

var reservedCooldownStateKeywords = []string{
	strings.ToLower(string(entities.AuthFileCooldownActive)),
	strings.ToLower(string(entities.AuthFileCooldownRecovered)),
	strings.ToLower(string(entities.AuthFileCooldownRecoveredExt)),
	strings.ToLower(string(entities.AuthFileCooldownRestoreFailed)),
	strings.ToLower(string(entities.AuthFileCooldownSkippedManual)),
	strings.ToLower(string(entities.AuthFileCooldownMissing)),
	strings.ToLower(string(entities.AuthFileCooldownDisableFailed)),
}

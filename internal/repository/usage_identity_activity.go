package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const usageIdentityActivityLimit = 100

type UsageIdentityActivityEvent struct {
	ID         int64
	Timestamp  time.Time
	Failed     bool
	StatusCode *int
}

type UsageIdentityActivity struct {
	WindowStart   time.Time
	WindowEnd     time.Time
	Latest        *UsageIdentityActivityEvent
	LastSuccess   *UsageIdentityActivityEvent
	LastFailure   *UsageIdentityActivityEvent
	Streak        int
	StreakAtLeast bool
}

// LoadUsageIdentityActivity 只读取当前身份近五小时中每种结果最多 101 条事件。
func LoadUsageIdentityActivity(ctx context.Context, db *gorm.DB, id int64, now time.Time) (UsageIdentityActivity, error) {
	if db == nil {
		return UsageIdentityActivity{}, fmt.Errorf("database is nil")
	}
	end := timeutil.NormalizeStorageTime(now)
	result := UsageIdentityActivity{WindowStart: end.Add(-5 * time.Hour), WindowEnd: end}
	err := db.Clauses(dbresolver.Read).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var identity entities.UsageIdentity
		if err := tx.Select("id, auth_type, identity").Where("id = ?", id).First(&identity).Error; err != nil {
			return fmt.Errorf("find usage identity by id: %w", err)
		}
		var authType string
		switch identity.AuthType {
		case entities.UsageIdentityAuthTypeCredential:
			authType = "oauth"
		case entities.UsageIdentityAuthTypeAIProvider:
			authType = "apikey"
		}
		authIndex := strings.TrimSpace(identity.Identity)
		if authType == "" || authIndex == "" {
			return nil
		}
		// 分别索引定位两种结果，限量后再合并，不能从健康桶推断事件先后。
		successes, err := loadUsageIdentityActivityEvents(tx, authType, authIndex, false, result.WindowStart, end)
		if err != nil {
			return err
		}
		failures, err := loadUsageIdentityActivityEvents(tx, authType, authIndex, true, result.WindowStart, end)
		if err != nil {
			return err
		}
		if len(successes) > 0 {
			result.LastSuccess = &successes[0]
		}
		if len(failures) > 0 {
			result.LastFailure = &failures[0]
		}
		if result.LastFailure == nil || (result.LastSuccess != nil && activityEventAfter(*result.LastSuccess, *result.LastFailure)) {
			result.Latest = result.LastSuccess
		} else {
			result.Latest = result.LastFailure
		}
		if result.Latest == nil {
			return nil
		}
		same, opposite := successes, result.LastFailure
		if result.Latest.Failed {
			same, opposite = failures, result.LastSuccess
		}
		for _, event := range same {
			if opposite != nil && !activityEventAfter(event, *opposite) {
				break
			}
			if result.Streak == usageIdentityActivityLimit {
				result.StreakAtLeast = true
				break
			}
			result.Streak++
		}
		return nil
	})
	if err != nil {
		return UsageIdentityActivity{}, err
	}
	return result, nil
}

func loadUsageIdentityActivityEvents(tx *gorm.DB, authType, authIndex string, failed bool, start, end time.Time) ([]UsageIdentityActivityEvent, error) {
	key := entities.UsageIdentityActivityTimestamp
	var rows []entities.UsageEvent
	if err := tx.Model(&entities.UsageEvent{}).
		Select("id, timestamp, failed, status_code").
		Where("auth_type = ? AND auth_index = ? AND failed = ?", authType, authIndex, failed).
		Where("("+key+") >= ? AND ("+key+") < ?", timeutil.FormatSortableStorageTime(start), timeutil.FormatSortableStorageTime(end)).
		Order(key + " DESC").Order("id DESC").Limit(usageIdentityActivityLimit + 1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage identity activity: %w", err)
	}
	events := make([]UsageIdentityActivityEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, UsageIdentityActivityEvent{
			ID: row.ID, Timestamp: timeutil.NormalizeStorageTime(row.Timestamp), Failed: row.Failed, StatusCode: row.StatusCode,
		})
	}
	return events, nil
}

func activityEventAfter(left, right UsageIdentityActivityEvent) bool {
	return left.Timestamp.After(right.Timestamp) || left.Timestamp.Equal(right.Timestamp) && left.ID > right.ID
}

package releasecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const retryInterval = 6 * time.Hour
const maxAttempts = 5

type state struct {
	Attempts int       `json:"attempts"`
	NextAt   time.Time `json:"next_at"`
	Complete bool      `json:"complete"`
}

type Runner struct {
	db      *gorm.DB
	version string
	client  *http.Client
}

func New(db *gorm.DB, version string, client *http.Client) *Runner {
	return &Runner{db: db, version: version, client: client}
}

// Check 执行一次到期检查，返回下一次检查的等待时间；零表示已完成或无需继续。
func (r *Runner) Check(ctx context.Context, now time.Time) (time.Duration, error) {
	if !stableV2.MatchString(r.version) {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if r.db == nil {
		return 0, fmt.Errorf("version file database is nil")
	}
	key := "release.version_file." + r.version
	var current state
	var claimed bool
	var wait time.Duration
	// 请求前先持久化尝试次数和下次时间，重启、并发检查都不能绕过间隔与上限。
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		setting, found, err := repository.GetAppSetting(ctx, tx, key)
		if err != nil {
			return err
		}
		if found {
			if setting.Value == nil {
				return fmt.Errorf("empty version file state")
			}
			if err := json.Unmarshal([]byte(*setting.Value), &current); err != nil {
				return err
			}
			if current.Attempts < 0 {
				return fmt.Errorf("invalid version file attempt count")
			}
		}
		if current.Complete || current.Attempts >= maxAttempts {
			return nil
		}
		if current.NextAt.After(now) {
			wait = current.NextAt.Sub(now)
			return nil
		}
		current.Attempts++
		current.NextAt = now.Add(retryInterval)
		if err := save(ctx, tx, key, current); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	if err != nil || !claimed {
		return wait, err
	}

	// 网络请求在事务外执行，不占用 SQLite writer。
	if err := DownloadVersion(ctx, r.client, r.version); err != nil {
		if current.Attempts < maxAttempts {
			return retryInterval, err
		}
		return 0, err
	}
	current.Complete = true
	err = save(ctx, r.db, key, current)
	// 下载已成功，只重试完成状态写入；等待期间不持有数据库事务。
	for _, delay := range [...]time.Duration{time.Second, 3 * time.Second, 10 * time.Second} {
		if err == nil {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, ctx.Err()
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		err = save(ctx, r.db, key, current)
	}
	return 0, err
}

func save(ctx context.Context, db *gorm.DB, key string, value state) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded := string(data)
	_, err = repository.UpsertAppSetting(ctx, db, entities.AppSetting{
		SettingKey: key, Value: &encoded, ValueType: entities.AppSettingValueTypeJSON,
	})
	return err
}

func (r *Runner) Run(ctx context.Context) {
	for {
		wait, err := r.Check(ctx, time.Now())
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			// 附件尚未发布或网络不可用不属于业务错误，最多每次尝试记录一条调试日志。
			logrus.WithError(err).Debug("release version file unavailable")
		}
		if wait <= 0 {
			return
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

package app

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"time"

	"cpa-usage-keeper/internal/backup"
	"github.com/sirupsen/logrus"
)

type DatabaseBackupWriter interface {
	WriteDatabase(context.Context, time.Time) (string, error)
}

type DatabaseBackupCleaner interface {
	Cleanup(retentionDays int, now time.Time) (int, error)
}

type DatabaseBackupHistory interface {
	LastBackupAt() (time.Time, bool, error)
}

type databaseBackupStore struct {
	db     *sql.DB
	dir    string
	writer *backup.Writer
}

func newDatabaseBackupStore(db *sql.DB, dir string) *databaseBackupStore {
	return &databaseBackupStore{db: db, dir: dir, writer: backup.NewWriter(dir)}
}

func (s *databaseBackupStore) WriteDatabase(ctx context.Context, backupAt time.Time) (string, error) {
	return s.writer.WriteDatabase(ctx, s.db, backupAt)
}

func (s *databaseBackupStore) Cleanup(retentionDays int, now time.Time) (int, error) {
	return s.writer.Cleanup(retentionDays, now)
}

func (s *databaseBackupStore) LastBackupAt() (time.Time, bool, error) {
	files, err := backup.ListFiles(s.dir)
	if err != nil {
		return time.Time{}, false, err
	}
	var latest time.Time
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			return time.Time{}, false, err
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest, !latest.IsZero(), nil
}

type DatabaseBackupRunner struct {
	writer        DatabaseBackupWriter
	cleaner       DatabaseBackupCleaner
	history       DatabaseBackupHistory
	interval      time.Duration
	retentionDays int
	lastBackupAt  time.Time
	retryDelay    time.Duration
	retryAttempts int
	pendingRetry  bool
	now           func() time.Time
	sleep         func(context.Context, time.Duration) bool

	mu      sync.Mutex
	running bool
}

func NewDatabaseBackupRunner(writer DatabaseBackupWriter, cleaner DatabaseBackupCleaner, interval time.Duration, retentionDays int) *DatabaseBackupRunner {
	history, _ := writer.(DatabaseBackupHistory)
	return &DatabaseBackupRunner{
		writer:        writer,
		cleaner:       cleaner,
		history:       history,
		interval:      interval,
		retentionDays: retentionDays,
		retryDelay:    15 * time.Minute,
		now:           time.Now,
		sleep:         maintenanceSleepContext,
	}
}

func (r *DatabaseBackupRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	logrus.Info("database backup task started")
	r.setRunning(true)
	defer r.setRunning(false)

	for {
		now := r.now()
		delay := r.nextDelay(now)
		if delay < 0 {
			delay = 0
		}
		if !r.sleep(ctx, delay) {
			return nil
		}
		backupAt := r.now()
		if _, err := r.writer.WriteDatabase(ctx, backupAt); err != nil {
			logrus.WithError(err).Error("database backup failed")
			r.retryAttempts++
			r.pendingRetry = r.retryAttempts <= 3
		} else {
			r.lastBackupAt = backupAt
			r.retryAttempts = 0
			r.pendingRetry = false
		}
		r.cleanup(backupAt)
	}
}

func (r *DatabaseBackupRunner) nextDelay(now time.Time) time.Duration {
	if r.pendingRetry {
		return r.retryDelay
	}
	if r.usesDailySchedule() {
		return r.nextDailyDelay(now)
	}
	lastBackupAt, ok := r.lastBackupAtFromHistory()
	if !ok {
		return 0
	}
	return lastBackupAt.Add(r.interval).Sub(now)
}

func (r *DatabaseBackupRunner) cleanup(now time.Time) {
	if r.cleaner == nil || r.retentionDays <= 0 {
		return
	}
	if _, err := r.cleaner.Cleanup(r.retentionDays, now); err != nil {
		logrus.WithError(err).Error("database backup cleanup failed")
	}
}

func (r *DatabaseBackupRunner) nextDailyDelay(now time.Time) time.Duration {
	localNow := now.In(time.Local)
	nextBackupAt := nextDailyBackupAt(localNow)
	if lastBackupAt, ok := r.lastBackupAtFromHistory(); ok {
		lastLocalBackup := lastBackupAt.In(time.Local)
		lastBackupDay := time.Date(lastLocalBackup.Year(), lastLocalBackup.Month(), lastLocalBackup.Day(), 4, 0, 0, 0, time.Local)
		candidate := lastBackupDay.AddDate(0, 0, r.dailyScheduleDays())
		if localNow.Before(candidate) {
			nextBackupAt = candidate
		}
	}
	return nextBackupAt.Sub(localNow)
}

func (r *DatabaseBackupRunner) usesDailySchedule() bool {
	return r.interval >= 24*time.Hour && r.interval%(24*time.Hour) == 0
}

func (r *DatabaseBackupRunner) dailyScheduleDays() int {
	return int(r.interval / (24 * time.Hour))
}

func (r *DatabaseBackupRunner) lastBackupAtFromHistory() (time.Time, bool) {
	lastBackupAt := r.lastBackupAt
	if r.history != nil {
		storedBackupAt, ok, err := r.history.LastBackupAt()
		if err != nil {
			logrus.WithError(err).Error("load last database backup time failed")
		} else if ok && storedBackupAt.After(lastBackupAt) {
			lastBackupAt = storedBackupAt
		}
	}
	return lastBackupAt, !lastBackupAt.IsZero()
}

func nextDailyBackupAt(now time.Time) time.Time {
	localNow := now.In(time.Local)
	backupAt := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 4, 0, 0, 0, time.Local)
	if !localNow.Before(backupAt) {
		backupAt = backupAt.AddDate(0, 0, 1)
	}
	return backupAt
}

func (r *DatabaseBackupRunner) validate() error {
	if r == nil {
		return fmt.Errorf("database backup runner is nil")
	}
	if r.writer == nil {
		return fmt.Errorf("database backup writer is nil")
	}
	if r.interval <= 0 {
		return fmt.Errorf("database backup interval must be positive")
	}
	if r.retryDelay <= 0 {
		r.retryDelay = 15 * time.Minute
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.sleep == nil {
		r.sleep = maintenanceSleepContext
	}
	return nil
}

func (r *DatabaseBackupRunner) setRunning(running bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = running
}

package logging_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/logging"
	"github.com/sirupsen/logrus"
)

const errorLogFilePrefix = "cpa-usage-keeper-error-"

func TestConfigureDuplicatesOnlyErrorLevelsToDedicatedDailyFile(t *testing.T) {
	logDir := t.TempDir()
	cfg := config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           logDir,
		LogRetentionDays: 7,
	}

	_ = captureConsole(t, cfg, func() {
		logger := logrus.StandardLogger()
		logger.Log(logrus.InfoLevel, "info stays in combined log")
		logger.Log(logrus.WarnLevel, "warn stays in combined log")
		logger.Log(logrus.ErrorLevel, "error is duplicated")
		logger.Log(logrus.FatalLevel, "fatal is duplicated")
		func() {
			defer func() { _ = recover() }()
			logger.Log(logrus.PanicLevel, "panic is duplicated")
		}()
	})

	combined := readLogFile(t, filepath.Join(logDir, "cpa-usage-keeper-"+today()+".log"))
	for _, message := range []string{
		"info stays in combined log",
		"warn stays in combined log",
		"error is duplicated",
		"fatal is duplicated",
		"panic is duplicated",
	} {
		if !strings.Contains(combined, message) {
			t.Fatalf("expected combined log to contain %q, got %q", message, combined)
		}
	}

	errorOnly := readLogFile(t, filepath.Join(logDir, errorLogFilePrefix+today()+".log"))
	for _, message := range []string{"error is duplicated", "fatal is duplicated", "panic is duplicated"} {
		if !strings.Contains(errorOnly, message) {
			t.Fatalf("expected error log to contain %q, got %q", message, errorOnly)
		}
	}
	for _, message := range []string{"info stays in combined log", "warn stays in combined log"} {
		if strings.Contains(errorOnly, message) {
			t.Fatalf("expected error log to exclude %q, got %q", message, errorOnly)
		}
	}
	if ansiPattern.MatchString(errorOnly) {
		t.Fatalf("expected error log without ANSI sequences, got %q", errorOnly)
	}
	if lines := strings.Count(errorOnly, "\n"); lines != 3 {
		t.Fatalf("expected three error-level log lines, got %d in %q", lines, errorOnly)
	}
}

func TestConfigureCleansCombinedAndErrorRetentionOnInitialization(t *testing.T) {
	logDir := t.TempDir()
	now := time.Now()
	expiredCombined := filepath.Join(logDir, "cpa-usage-keeper-"+now.AddDate(0, 0, -8).Format("2006-01-02")+".log")
	oldestRetainedCombined := filepath.Join(logDir, "cpa-usage-keeper-"+now.AddDate(0, 0, -7).Format("2006-01-02")+".log")
	expiredError := filepath.Join(logDir, errorLogFilePrefix+now.AddDate(0, 0, -31).Format("2006-01-02")+".log")
	oldestRetainedError := filepath.Join(logDir, errorLogFilePrefix+now.AddDate(0, 0, -30).Format("2006-01-02")+".log")
	otherLog := filepath.Join(logDir, "other.log")
	for _, path := range []string{expiredCombined, oldestRetainedCombined, expiredError, oldestRetainedError, otherLog} {
		if err := os.WriteFile(path, []byte("fixture"), 0644); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}

	_ = captureConsole(t, config.Config{
		LogLevel:         "error",
		LogFileEnabled:   true,
		LogDir:           logDir,
		LogRetentionDays: 7,
	}, func() {})

	for _, path := range []string{expiredCombined, expiredError} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected expired log %s to be removed, stat err=%v", path, err)
		}
	}
	for _, path := range []string{oldestRetainedCombined, oldestRetainedError, otherLog} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected retained file %s to remain: %v", path, err)
		}
	}
}

func TestConfigureCloseRestoresExistingLogrusHooks(t *testing.T) {
	logger := logrus.StandardLogger()
	previousHooks := logger.ReplaceHooks(make(logrus.LevelHooks))
	t.Cleanup(func() { logger.ReplaceHooks(previousHooks) })
	existing := &sentinelHook{}
	logger.AddHook(existing)

	closer, err := logging.Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           t.TempDir(),
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	if hooks := logger.Hooks[logrus.ErrorLevel]; len(hooks) != 2 {
		_ = closer.Close()
		t.Fatalf("expected existing hook plus dedicated error hook, got %d", len(hooks))
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close logging: %v", err)
	}

	hooks := logger.Hooks[logrus.ErrorLevel]
	if len(hooks) != 1 || hooks[0] != existing {
		t.Fatalf("expected only the original hook after close, got %#v", hooks)
	}
}

func TestConfigureReportsDedicatedErrorFileInitializationFailure(t *testing.T) {
	logDir := t.TempDir()
	errorLogPath := filepath.Join(logDir, errorLogFilePrefix+today()+".log")
	if err := os.Mkdir(errorLogPath, 0755); err != nil {
		t.Fatalf("create error-log failure fixture: %v", err)
	}

	_, err := logging.Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           logDir,
		LogRetentionDays: 7,
	})
	if err == nil {
		t.Fatal("expected error log initialization failure")
	}
	if !strings.Contains(err.Error(), "initialize dedicated error log writer") {
		t.Fatalf("expected dedicated writer context, got %v", err)
	}
}

func TestConfigureMaintainsErrorRetentionWhenOnlyInfoLogsContinue(t *testing.T) {
	earlier, later := consecutiveDateLocations(t)
	previousLocal := time.Local
	time.Local = earlier
	t.Cleanup(func() { time.Local = previousLocal })

	logDir := t.TempDir()
	closer, err := logging.Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           logDir,
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	expiredDate := time.Now().In(later).AddDate(0, 0, -40).Format("2006-01-02")
	expiredPath := filepath.Join(logDir, errorLogFilePrefix+expiredDate+".log")
	if err := os.WriteFile(expiredPath, []byte("expired"), 0644); err != nil {
		t.Fatalf("write expired error log: %v", err)
	}

	time.Local = later
	logrus.Info("ordinary traffic triggers daily maintenance")

	if _, err := os.Stat(expiredPath); !os.IsNotExist(err) {
		t.Fatalf("expected ordinary logs to clean expired error log, stat err=%v", err)
	}
}

func TestDedicatedErrorHookRunsFirstAndFailureDoesNotStopFollowingHooks(t *testing.T) {
	earlier, later := consecutiveDateLocations(t)
	previousLocal := time.Local
	time.Local = earlier
	t.Cleanup(func() { time.Local = previousLocal })

	logger := logrus.StandardLogger()
	previousHooks := logger.ReplaceHooks(make(logrus.LevelHooks))
	t.Cleanup(func() { logger.ReplaceHooks(previousHooks) })
	existing := &sentinelHook{}
	logger.AddHook(existing)

	logDir := t.TempDir()
	closer, err := logging.Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           logDir,
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	configuredHooks := logger.Hooks[logrus.ErrorLevel]
	if len(configuredHooks) != 2 || configuredHooks[0] == existing || configuredHooks[1] != existing {
		t.Fatalf("expected dedicated error hook before existing hook, got %#v", configuredHooks)
	}
	expiredPath := filepath.Join(logDir, errorLogFilePrefix+time.Now().In(later).AddDate(0, 0, -40).Format("2006-01-02")+".log")
	if err := os.WriteFile(expiredPath, []byte("expired"), 0644); err != nil {
		t.Fatalf("write expired error log: %v", err)
	}
	errorLogPath := filepath.Join(logDir, errorLogFilePrefix+time.Now().In(later).Format("2006-01-02")+".log")
	if err := os.Mkdir(errorLogPath, 0755); err != nil {
		t.Fatalf("create write failure fixture: %v", err)
	}
	time.Local = later

	output := captureStderr(t, func() {
		logrus.Error("dedicated writer fails")
	})
	if !strings.Contains(output, "dedicated error log write failed") {
		t.Fatalf("expected active dedicated hook failure to be exercised, got %q", output)
	}
	if existing.fireCount != 1 {
		t.Fatalf("expected following hook to fire once, got %d", existing.fireCount)
	}
	if _, err := os.Stat(expiredPath); !os.IsNotExist(err) {
		t.Fatalf("expected maintenance before failed error write to remove %s, stat err=%v", expiredPath, err)
	}
}

func TestClosedDedicatedErrorHookCannotReopenItsFile(t *testing.T) {
	logger := logrus.StandardLogger()
	previousHooks := logger.ReplaceHooks(make(logrus.LevelHooks))
	t.Cleanup(func() { logger.ReplaceHooks(previousHooks) })

	logDir := t.TempDir()
	closer, err := logging.Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           logDir,
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	dedicated := logger.Hooks[logrus.ErrorLevel][0]
	if err := closer.Close(); err != nil {
		t.Fatalf("close logging: %v", err)
	}

	errorLogPath := filepath.Join(logDir, errorLogFilePrefix+today()+".log")
	if err := os.Remove(errorLogPath); err != nil {
		t.Fatalf("remove closed error log: %v", err)
	}
	entry := logrus.NewEntry(logger)
	entry.Level = logrus.ErrorLevel
	entry.Message = "late error"
	_ = captureStderr(t, func() {
		if err := dedicated.Fire(entry); err != nil {
			t.Fatalf("expected closed hook failure to stay isolated, got %v", err)
		}
	})
	if _, err := os.Stat(errorLogPath); !os.IsNotExist(err) {
		t.Fatalf("expected closed hook not to recreate error log, stat err=%v", err)
	}
}

func TestConfigureRecoversAfterDailyFileOpenFailure(t *testing.T) {
	earlier, later := consecutiveDateLocations(t)
	previousLocal := time.Local
	time.Local = earlier
	t.Cleanup(func() { time.Local = previousLocal })

	logDir := t.TempDir()
	closer, err := logging.Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           logDir,
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	laterDate := time.Now().In(later).Format("2006-01-02")
	laterPath := filepath.Join(logDir, "cpa-usage-keeper-"+laterDate+".log")
	if err := os.Mkdir(laterPath, 0755); err != nil {
		t.Fatalf("create rollover failure fixture: %v", err)
	}
	time.Local = later
	logrus.Info("first rollover attempt fails")
	if err := os.Remove(laterPath); err != nil {
		t.Fatalf("remove rollover failure fixture: %v", err)
	}

	logrus.Info("rollover recovers")
	content, err := os.ReadFile(laterPath)
	if err != nil {
		t.Fatalf("read recovered daily log: %v", err)
	}
	if !strings.Contains(string(content), "rollover recovers") {
		t.Fatalf("expected recovered writer to persist later log, got %q", content)
	}
}

func TestDedicatedErrorMaintenanceFailureIsReportedOnceForSameDate(t *testing.T) {
	earlier, later := consecutiveDateLocations(t)
	previousLocal := time.Local
	time.Local = earlier
	t.Cleanup(func() { time.Local = previousLocal })

	baseDir := t.TempDir()
	logDir := filepath.Join(baseDir, "logs")
	closer, err := logging.Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           logDir,
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	dedicated := logrus.StandardLogger().Hooks[logrus.InfoLevel][0]
	entry := logrus.NewEntry(logrus.StandardLogger())
	entry.Level = logrus.InfoLevel
	entry.Message = "ordinary maintenance"
	if err := dedicated.Fire(entry); err != nil {
		t.Fatalf("initial maintenance hook: %v", err)
	}

	movedLogDir := filepath.Join(baseDir, "moved-logs")
	if err := os.Rename(logDir, movedLogDir); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("moving a directory with open files is unavailable on Windows: %v", err)
		}
		t.Fatalf("move log directory: %v", err)
	}
	time.Local = later

	output := captureStderr(t, func() {
		if err := dedicated.Fire(entry); err != nil {
			t.Fatalf("later-date maintenance hook: %v", err)
		}
		if err := dedicated.Fire(entry); err != nil {
			t.Fatalf("repeated same-date maintenance hook: %v", err)
		}
	})
	if count := strings.Count(output, "dedicated error log maintenance failed"); count != 1 {
		t.Fatalf("expected one maintenance failure report per day, got %d in %q", count, output)
	}
	if strings.Contains(output, "dedicated error log write failed") {
		t.Fatalf("expected maintenance failure not to be reported as write failure, got %q", output)
	}
}

func TestConfigureCloseIsIdempotent(t *testing.T) {
	first, err := logging.Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           t.TempDir(),
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("configure first logger: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	if err := first.Close(); err != nil {
		t.Fatalf("close first logger: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first logger twice: %v", err)
	}
}

func TestDedicatedErrorMaintenanceClosesStaleDailyFile(t *testing.T) {
	earlier, later := consecutiveDateLocations(t)
	previousLocal := time.Local
	time.Local = earlier
	t.Cleanup(func() { time.Local = previousLocal })

	logDir := t.TempDir()
	closer, err := logging.Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           logDir,
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	dedicated := logrus.StandardLogger().Hooks[logrus.InfoLevel][0]
	stalePath := filepath.Join(logDir, errorLogFilePrefix+time.Now().Format("2006-01-02")+".log")
	if count := openFileDescriptorCount(t, stalePath); count == 0 {
		t.Fatalf("expected dedicated writer to hold %s before maintenance", stalePath)
	}

	time.Local = later
	entry := logrus.NewEntry(logrus.StandardLogger())
	entry.Level = logrus.InfoLevel
	entry.Message = "advance daily maintenance"
	if err := dedicated.Fire(entry); err != nil {
		t.Fatalf("maintenance hook: %v", err)
	}

	if count := openFileDescriptorCount(t, stalePath); count != 0 {
		t.Fatalf("expected stale dedicated error file to be closed, found %d descriptors", count)
	}
}

func readLogFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file %s: %v", path, err)
	}
	return string(content)
}

func today() string {
	return time.Now().Format("2006-01-02")
}

func consecutiveDateLocations(t *testing.T) (*time.Location, *time.Location) {
	t.Helper()
	earlier, err := time.LoadLocation("Pacific/Honolulu")
	if err != nil {
		t.Fatalf("load earlier timezone: %v", err)
	}
	later, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatalf("load later timezone: %v", err)
	}
	if time.Now().In(earlier).Format("2006-01-02") == time.Now().In(later).Format("2006-01-02") {
		t.Fatal("expected test timezones to produce consecutive dates")
	}
	return earlier, later
}

func openFileDescriptorCount(t *testing.T, targetPath string) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("file descriptor inspection unavailable: %v", err)
	}
	targetPath, err = filepath.Abs(targetPath)
	if err != nil {
		t.Fatalf("resolve target path: %v", err)
	}
	count := 0
	for _, entry := range entries {
		link, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err != nil {
			continue
		}
		link = strings.TrimSuffix(link, " (deleted)")
		if link == targetPath {
			count++
		}
	}
	return count
}

type sentinelHook struct {
	fireCount int
}

func (*sentinelHook) Levels() []logrus.Level { return logrus.AllLevels }
func (h *sentinelHook) Fire(*logrus.Entry) error {
	h.fireCount++
	return nil
}

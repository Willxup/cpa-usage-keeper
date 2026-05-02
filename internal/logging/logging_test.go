package logging

import (
	"bytes"
	stdlog "log"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestResolveLogDirUsesWorkDirFallback(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "work")

	logDir := resolveLogDir(config.Config{WorkDir: workDir})

	if logDir != filepath.Join(workDir, filepath.Base(config.DefaultLogDir)) {
		t.Fatalf("expected log dir under work dir, got %q", logDir)
	}
}

func TestConfigureWritesLogrusToDailyFile(t *testing.T) {
	reset := captureGlobalLogState(t)
	defer reset()

	logDir := t.TempDir()
	closer, err := Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           logDir,
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	defer closer.Close()

	logrus.Info("file logging works")

	content := readTodayLogFile(t, logDir)
	if !strings.Contains(content, "file logging works") {
		t.Fatalf("expected log file to contain logrus message, got %q", content)
	}
	if !logLineHasTimestamp(content) {
		t.Fatalf("expected log file to include timestamp, got %q", content)
	}
}

func TestConfigureUsesFullTimestampFormatter(t *testing.T) {
	reset := captureGlobalLogState(t)
	defer reset()

	closer, err := Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   false,
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	defer closer.Close()

	formatter, ok := logrus.StandardLogger().Formatter.(*logrus.TextFormatter)
	if !ok {
		t.Fatalf("expected text formatter, got %T", logrus.StandardLogger().Formatter)
	}
	if !formatter.FullTimestamp || formatter.TimestampFormat == "" {
		t.Fatalf("expected full timestamp formatter, got FullTimestamp=%v TimestampFormat=%q", formatter.FullTimestamp, formatter.TimestampFormat)
	}
}

func TestConfigureWritesLogrusConsoleWithTimestamp(t *testing.T) {
	reset := captureGlobalLogState(t)
	defer reset()

	previousStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = writer
	defer func() {
		os.Stderr = previousStderr
		_ = reader.Close()
	}()

	closer, err := Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   false,
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}

	logrus.Info("console timestamp works")
	if err := closer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(reader); err != nil {
		t.Fatalf("read stderr output: %v", err)
	}
	content := output.String()
	if !strings.Contains(content, "console timestamp works") {
		t.Fatalf("expected console output to contain logrus message, got %q", content)
	}
	if !logLineHasTimestamp(content) {
		t.Fatalf("expected console output to include timestamp, got %q", content)
	}
}

func TestConfigureDisablesFileLogging(t *testing.T) {
	reset := captureGlobalLogState(t)
	defer reset()

	logDir := t.TempDir()
	closer, err := Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   false,
		LogDir:           logDir,
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	defer closer.Close()

	logrus.Info("stderr only")

	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("read log dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no log files when file logging disabled, got %d", len(entries))
	}
}

func TestConfigureRoutesStdlibLogAndSlogToFile(t *testing.T) {
	reset := captureGlobalLogState(t)
	defer reset()

	logDir := t.TempDir()
	closer, err := Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           logDir,
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	defer closer.Close()

	stdlog.Print("stdlib message")
	slog.Error("slog message")

	content := readTodayLogFile(t, logDir)
	if !strings.Contains(content, "stdlib message") || !strings.Contains(content, "slog message") {
		t.Fatalf("expected stdlib and slog messages in file, got %q", content)
	}
}

func TestConfigureRoutesGinDebugToTimestampedLogrusOutput(t *testing.T) {
	reset := captureGlobalLogState(t)
	defer reset()

	previousStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = writer
	defer func() {
		os.Stderr = previousStderr
		_ = reader.Close()
	}()

	closer, err := Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   false,
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}

	if gin.DebugPrintFunc == nil {
		t.Fatal("expected Configure to install Gin debug print function")
	}
	gin.DebugPrintFunc("GET %s", "/api/v1/status")
	if err := closer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(reader); err != nil {
		t.Fatalf("read stderr output: %v", err)
	}
	content := output.String()
	if !strings.Contains(content, "[GIN-debug] GET /api/v1/status") {
		t.Fatalf("expected Gin debug output to be routed through logrus, got %q", content)
	}
	if !logLineHasTimestamp(content) {
		t.Fatalf("expected Gin debug output to include timestamp, got %q", content)
	}
}

func TestConfigureCloseRestoresGlobalLoggers(t *testing.T) {
	reset := captureGlobalLogState(t)
	defer reset()

	var restoredOutput bytes.Buffer
	logrus.SetOutput(&restoredOutput)
	stdlog.SetOutput(&restoredOutput)
	slog.SetDefault(slog.New(slog.NewTextHandler(&restoredOutput, nil)))
	gin.DefaultWriter = &restoredOutput
	gin.DefaultErrorWriter = &restoredOutput
	gin.DebugPrintFunc = func(format string, values ...interface{}) {
		restoredOutput.WriteString("after close gin debug")
	}
	gin.DebugPrintRouteFunc = func(httpMethod, absolutePath, handlerName string, nuHandlers int) {
		restoredOutput.WriteString(" after close gin route")
	}

	closer, err := Configure(config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           t.TempDir(),
		LogRetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	logrus.Info("after close logrus")
	stdlog.Print("after close stdlib")
	slog.Error("after close slog")
	gin.DebugPrintFunc("ignored")
	gin.DebugPrintRouteFunc("GET", "/", "handler", 1)

	content := restoredOutput.String()
	for _, want := range []string{"after close logrus", "after close stdlib", "after close slog", "after close gin debug", "after close gin route"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected global loggers to be restored after close with %q, got %q", want, content)
		}
	}
}

func TestConfigureErrorLeavesGlobalLoggerStateUnchanged(t *testing.T) {
	reset := captureGlobalLogState(t)
	defer reset()

	logrus.SetLevel(logrus.DebugLevel)
	invalidLogDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(invalidLogDir, []byte("file"), 0644); err != nil {
		t.Fatalf("write invalid log dir fixture: %v", err)
	}

	_, err := Configure(config.Config{
		LogLevel:         "error",
		LogFileEnabled:   true,
		LogDir:           invalidLogDir,
		LogRetentionDays: 7,
	})
	if err == nil {
		t.Fatal("expected Configure to return an error")
	}
	if level := logrus.GetLevel(); level != logrus.DebugLevel {
		t.Fatalf("expected logrus level to remain debug after configure error, got %s", level)
	}
}

func TestRetentionDeletesOnlyOldAppLogs(t *testing.T) {
	logDir := t.TempDir()
	oldAppLog := filepath.Join(logDir, "cpa-usage-keeper-2020-01-01.log")
	freshAppLog := filepath.Join(logDir, "cpa-usage-keeper-2099-01-01.log")
	otherLog := filepath.Join(logDir, "other.log")
	for _, path := range []string{oldAppLog, freshAppLog, otherLog} {
		if err := os.WriteFile(path, []byte("log"), 0644); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}

	writer, err := newDailyFileWriter(logDir, 7, func() time.Time {
		return time.Date(2026, 4, 28, 12, 0, 0, 0, time.Local)
	})
	if err != nil {
		t.Fatalf("newDailyFileWriter returned error: %v", err)
	}
	defer writer.Close()

	if _, err := os.Stat(oldAppLog); !os.IsNotExist(err) {
		t.Fatalf("expected old app log to be removed, stat err=%v", err)
	}
	for _, path := range []string{freshAppLog, otherLog} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to remain: %v", path, err)
		}
	}
}

func readTodayLogFile(t *testing.T, logDir string) string {
	t.Helper()
	path := filepath.Join(logDir, "cpa-usage-keeper-"+time.Now().Format("2006-01-02")+".log")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read today log file: %v", err)
	}
	return string(content)
}

func logLineHasTimestamp(content string) bool {
	return regexp.MustCompile(`time="?\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`).MatchString(content)
}

func captureGlobalLogState(t *testing.T) func() {
	t.Helper()
	previousLogrusOutput := logrus.StandardLogger().Out
	previousLogrusLevel := logrus.GetLevel()
	previousLogrusFormatter := logrus.StandardLogger().Formatter
	previousStdlogOutput := stdlog.Writer()
	previousSlog := slog.Default()
	previousGinDefaultWriter := gin.DefaultWriter
	previousGinErrorWriter := gin.DefaultErrorWriter
	previousGinDebugPrint := gin.DebugPrintFunc
	previousGinDebugPrintRoute := gin.DebugPrintRouteFunc
	var stderr bytes.Buffer
	logrus.SetOutput(&stderr)
	stdlog.SetOutput(&stderr)
	return func() {
		logrus.SetOutput(previousLogrusOutput)
		logrus.SetLevel(previousLogrusLevel)
		logrus.SetFormatter(previousLogrusFormatter)
		stdlog.SetOutput(previousStdlogOutput)
		slog.SetDefault(previousSlog)
		gin.DefaultWriter = previousGinDefaultWriter
		gin.DefaultErrorWriter = previousGinErrorWriter
		gin.DebugPrintFunc = previousGinDebugPrint
		gin.DebugPrintRouteFunc = previousGinDebugPrintRoute
	}
}

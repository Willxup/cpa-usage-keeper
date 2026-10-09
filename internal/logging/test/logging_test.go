package logging_test

import (
	"bytes"
	. "cpa-usage-keeper/internal/logging"
	stdlog "log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cpa-usage-keeper/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestResolveLogDirUsesWorkDirFallback(t *testing.T) {
	captureGlobalLogState(t)
	workDir := filepath.Join(t.TempDir(), "work")
	closer, err := Configure(config.Config{WorkDir: workDir, LogFileEnabled: true, LogRetentionDays: 7})
	if err != nil {
		t.Fatalf("Configure with work dir: %v", err)
	}
	defer closer.Close()
	logrus.Info("work dir fallback")
	content := readTodayLogFile(t, filepath.Join(workDir, filepath.Base(config.DefaultLogDir)))
	if !strings.Contains(content, "work dir fallback") {
		t.Fatalf("expected log under work dir, got %q", content)
	}
}

func TestConfigureWritesLogrusToDailyFile(t *testing.T) {
	captureGlobalLogState(t)

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

func TestConfigureDisablesFileLogging(t *testing.T) {
	captureGlobalLogState(t)

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

func TestConfigureRoutesStdlibLogToFile(t *testing.T) {
	captureGlobalLogState(t)

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

	content := readTodayLogFile(t, logDir)
	if !strings.Contains(content, "stdlib message") {
		t.Fatalf("expected stdlib message in file, got %q", content)
	}
}

func TestConfigureCloseRestoresGlobalLoggers(t *testing.T) {
	captureGlobalLogState(t)

	var restoredOutput bytes.Buffer
	logrus.SetOutput(&restoredOutput)
	stdlog.SetOutput(&restoredOutput)
	stdlog.SetFlags(stdlog.Lshortfile)
	stdlog.SetPrefix("restored-prefix ")
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
	if stdlog.Flags() != stdlog.Lshortfile || stdlog.Prefix() != "restored-prefix " {
		t.Fatalf("expected standard logger flags and prefix to be restored, got flags=%d prefix=%q", stdlog.Flags(), stdlog.Prefix())
	}

	logrus.Info("after close logrus")
	stdlog.Print("after close stdlib")
	gin.DebugPrintFunc("ignored")
	gin.DebugPrintRouteFunc("GET", "/", "handler", 1)

	content := restoredOutput.String()
	for _, want := range []string{"after close logrus", "after close stdlib", "after close gin debug", "after close gin route"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected global loggers to be restored after close with %q, got %q", want, content)
		}
	}
}

func TestConfigureErrorLeavesGlobalLoggerStateUnchanged(t *testing.T) {
	captureGlobalLogState(t)

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
	synctest.Test(t, func(t *testing.T) {
		captureGlobalLogState(t)
		// 固定旧测试的维护日期，文件写入仍经生产 Configure 和 writer。
		time.Sleep(time.Date(2026, 4, 28, 12, 0, 0, 0, time.Local).Sub(time.Now()))
		logDir := t.TempDir()
		oldAppLog := filepath.Join(logDir, "cpa-usage-keeper-2020-01-01.log")
		freshAppLog := filepath.Join(logDir, "cpa-usage-keeper-2099-01-01.log")
		otherLog := filepath.Join(logDir, "other.log")
		for _, path := range []string{oldAppLog, freshAppLog, otherLog} {
			if err := os.WriteFile(path, []byte("log"), 0644); err != nil {
				t.Fatalf("write fixture %s: %v", path, err)
			}
		}

		writer, err := Configure(config.Config{LogFileEnabled: true, LogDir: logDir, LogRetentionDays: 7})
		if err != nil {
			t.Fatalf("Configure returned error: %v", err)
		}
		defer writer.Close()
		logrus.Info("trigger log retention maintenance")

		if _, err := os.Stat(oldAppLog); !os.IsNotExist(err) {
			t.Fatalf("expected old app log to be removed, stat err=%v", err)
		}
		for _, path := range []string{freshAppLog, otherLog} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("expected %s to remain: %v", path, err)
			}
		}
	})
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
	var plain bytes.Buffer
	_, _ = NewPlainWriter(&plain).Write([]byte(content))
	return regexp.MustCompile(`(?m)^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2}) \|`).MatchString(plain.String())
}

func captureGlobalLogState(t *testing.T) {
	t.Helper()
	previousLogrusOutput := logrus.StandardLogger().Out
	previousLogrusLevel := logrus.GetLevel()
	previousLogrusFormatter := logrus.StandardLogger().Formatter
	previousStdlogOutput := stdlog.Writer()
	previousStdlogFlags := stdlog.Flags()
	previousStdlogPrefix := stdlog.Prefix()
	previousGinDefaultWriter := gin.DefaultWriter
	previousGinErrorWriter := gin.DefaultErrorWriter
	previousGinDebugPrint := gin.DebugPrintFunc
	previousGinDebugPrintRoute := gin.DebugPrintRouteFunc
	var stderr bytes.Buffer
	logrus.SetOutput(&stderr)
	stdlog.SetOutput(&stderr)
	t.Cleanup(func() {
		logrus.SetOutput(previousLogrusOutput)
		logrus.SetLevel(previousLogrusLevel)
		logrus.SetFormatter(previousLogrusFormatter)
		stdlog.SetOutput(previousStdlogOutput)
		stdlog.SetFlags(previousStdlogFlags)
		stdlog.SetPrefix(previousStdlogPrefix)
		gin.DefaultWriter = previousGinDefaultWriter
		gin.DefaultErrorWriter = previousGinErrorWriter
		gin.DebugPrintFunc = previousGinDebugPrint
		gin.DebugPrintRouteFunc = previousGinDebugPrintRoute
	})
}

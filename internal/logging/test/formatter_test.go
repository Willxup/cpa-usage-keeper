package logging_test

import (
	"errors"
	"io"
	stdlog "log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/logging"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestConfigureFormatsConsoleEntriesInStableColumns(t *testing.T) {
	output := captureConsole(t, config.Config{LogLevel: "info"}, func() {
		logrus.WithFields(logrus.Fields{
			"zeta":  "last",
			"alpha": "first",
		}).Info("example message")
	})

	plain := ansiPattern.ReplaceAllString(output, "")
	want := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2}) \| info  \| example message \| alpha=first zeta=last\n$`)
	if !want.MatchString(plain) {
		t.Fatalf("expected stable console columns, got %q", plain)
	}
	if !strings.Contains(output, "\x1b[32minfo \x1b[0m") {
		t.Fatalf("expected info level to be green, got %q", output)
	}
	if !strings.Contains(output, "\x1b[90m | \x1b[0malpha=first zeta=last\n") {
		t.Fatalf("expected structured fields without their own colors, got %q", output)
	}
}

func TestConfigureKeepsStructuredValuesOnOnePhysicalLine(t *testing.T) {
	output := captureConsole(t, config.Config{LogLevel: "info"}, func() {
		logrus.WithField("detail", "first line\nsecond line").Info("message\ncontinued")
	})

	plain := ansiPattern.ReplaceAllString(output, "")
	if strings.Count(plain, "\n") != 1 {
		t.Fatalf("expected one physical log line, got %q", plain)
	}
	if !strings.Contains(plain, `message\ncontinued | detail="first line\nsecond line"`) {
		t.Fatalf("expected escaped message and field newlines, got %q", plain)
	}
}

func TestConfigureEscapesControlCharactersInMessagesAndFieldKeys(t *testing.T) {
	output := captureConsole(t, config.Config{LogLevel: "info"}, func() {
		logrus.WithField("request\n\x1b[2Jid", "value").Info("unsafe\x1b[31mred\x1b[0m\x1b[2J")
	})

	plain := ansiPattern.ReplaceAllString(output, "")
	if strings.ContainsRune(plain, '\x1b') {
		t.Fatalf("expected control characters to be escaped, got %q", plain)
	}
	if strings.Count(plain, "\n") != 1 {
		t.Fatalf("expected one physical log line, got %q", plain)
	}
	if !strings.Contains(plain, `unsafe\x1b[31mred\x1b[0m\x1b[2J | "request\n\x1b[2Jid"=value`) {
		t.Fatalf("expected readable escaped controls, got %q", plain)
	}
}

func TestConfigureWritesPlainFormatWithoutANSIToDailyFile(t *testing.T) {
	logDir := t.TempDir()
	cfg := config.Config{
		LogLevel:         "info",
		LogFileEnabled:   true,
		LogDir:           logDir,
		LogRetentionDays: 7,
	}

	_ = captureConsole(t, cfg, func() {
		logrus.WithField("source", "refresh").Info("file message")
	})

	path := filepath.Join(logDir, "cpa-usage-keeper-"+time.Now().Format("2006-01-02")+".log")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read daily log file: %v", err)
	}
	if ansiPattern.Match(content) {
		t.Fatalf("expected file log without ANSI sequences, got %q", content)
	}
	want := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2}) \| info  \| file message \| source=refresh\n$`)
	if !want.Match(content) {
		t.Fatalf("expected plain file format, got %q", content)
	}
}

func TestConfigureRoutesStandardLoggerThroughUnifiedFormat(t *testing.T) {
	output := captureConsole(t, config.Config{LogLevel: "info"}, func() {
		stdlog.Print("standard library message")
	})

	plain := ansiPattern.ReplaceAllString(output, "")
	want := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2}) \| info  \| standard library message\n$`)
	if !want.MatchString(plain) {
		t.Fatalf("expected standard logger to use unified format, got %q", plain)
	}
}

func TestNewStandardLoggerUsesRequestedLogrusLevel(t *testing.T) {
	output := captureConsole(t, config.Config{LogLevel: "info"}, func() {
		logging.NewStandardLogger(logrus.ErrorLevel).Print("HTTP server failure")
	})

	plain := ansiPattern.ReplaceAllString(output, "")
	if !strings.Contains(plain, "| error | HTTP server failure") {
		t.Fatalf("expected requested standard logger level, got %q", plain)
	}
	if !strings.Contains(output, "\x1b[31merror\x1b[0m") {
		t.Fatalf("expected error logger to be red, got %q", output)
	}
}

func TestConfigureRoutesGinWritersThroughUnifiedLevels(t *testing.T) {
	output := captureConsole(t, config.Config{LogLevel: "info"}, func() {
		if _, err := gin.DefaultWriter.Write([]byte("gin access\n")); err != nil {
			t.Fatalf("write Gin access log: %v", err)
		}
		if _, err := gin.DefaultErrorWriter.Write([]byte("gin recovery\n")); err != nil {
			t.Fatalf("write Gin error log: %v", err)
		}
	})

	plain := ansiPattern.ReplaceAllString(output, "")
	if !strings.Contains(plain, "| info  | gin access") || !strings.Contains(plain, "| error | gin recovery") {
		t.Fatalf("expected Gin writers to use unified levels, got %q", plain)
	}
}

func TestConfigureBootstrapFormatsEarlyLogrusEntries(t *testing.T) {
	previousOutput := logrus.StandardLogger().Out
	previousLevel := logrus.GetLevel()
	previousFormatter := logrus.StandardLogger().Formatter
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetLevel(previousLevel)
		logrus.SetFormatter(previousFormatter)
	})

	output := captureStderr(t, func() {
		logging.ConfigureBootstrap()
		logrus.WithError(errors.New("invalid environment")).Error("initialize app")
	})

	plain := ansiPattern.ReplaceAllString(output, "")
	want := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2}) \| error \| initialize app \| error="invalid environment"\n$`)
	if !want.MatchString(plain) {
		t.Fatalf("expected bootstrap to use unified format, got %q", plain)
	}
	if !strings.Contains(output, "\x1b[31merror\x1b[0m") {
		t.Fatalf("expected bootstrap error level to be red, got %q", output)
	}
}

func TestLogTerminalErrorBypassesRestrictiveLevelsAndRestoresThem(t *testing.T) {
	for _, level := range []string{"fatal", "panic"} {
		t.Run(level, func(t *testing.T) {
			output := captureConsole(t, config.Config{LogLevel: level}, func() {
				logging.LogTerminalError("run app", errors.New("listen failed"))
				if got := logrus.GetLevel().String(); got != level {
					t.Fatalf("expected log level %q to be restored, got %q", level, got)
				}
			})

			plain := ansiPattern.ReplaceAllString(output, "")
			if !strings.Contains(plain, `| error | run app | error="listen failed"`) {
				t.Fatalf("expected terminal error to remain visible at %s level, got %q", level, plain)
			}
		})
	}
}

func captureConsole(t *testing.T, cfg config.Config, writeLog func()) string {
	t.Helper()
	return captureStderr(t, func() {
		closer, err := logging.Configure(cfg)
		if err != nil {
			t.Fatalf("configure logging: %v", err)
		}
		closed := false
		t.Cleanup(func() {
			if !closed {
				_ = closer.Close()
			}
		})
		writeLog()
		if err := closer.Close(); err != nil {
			t.Fatalf("close logging: %v", err)
		}
		closed = true
	})
}

func captureStderr(t *testing.T, action func()) string {
	t.Helper()

	previousStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = previousStderr
		_ = reader.Close()
		_ = writer.Close()
	})

	action()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(content)
}

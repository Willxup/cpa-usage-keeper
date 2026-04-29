package logging

import (
	"fmt"
	"io"
	stdlog "log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

const logFilePrefix = "cpa-usage-keeper-"

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

type restoreCloser struct {
	closer                     io.Closer
	previousLogrusOutput       io.Writer
	previousLogrusLevel        logrus.Level
	previousLogrusFormatter    logrus.Formatter
	previousStdlogOutput       io.Writer
	previousSlog               *slog.Logger
	previousGinDefaultWriter   io.Writer
	previousGinErrorWriter     io.Writer
	previousGinDebugPrint      func(string, ...interface{})
	previousGinDebugPrintRoute func(string, string, string, int)
}

func (c *restoreCloser) Close() error {
	logrus.SetOutput(c.previousLogrusOutput)
	logrus.SetLevel(c.previousLogrusLevel)
	logrus.SetFormatter(c.previousLogrusFormatter)
	stdlog.SetOutput(c.previousStdlogOutput)
	slog.SetDefault(c.previousSlog)
	gin.DefaultWriter = c.previousGinDefaultWriter
	gin.DefaultErrorWriter = c.previousGinErrorWriter
	gin.DebugPrintFunc = c.previousGinDebugPrint
	gin.DebugPrintRouteFunc = c.previousGinDebugPrintRoute
	return c.closer.Close()
}

func Configure(cfg config.Config) (io.Closer, error) {
	previousLogrusOutput := logrus.StandardLogger().Out
	previousLogrusLevel := logrus.GetLevel()
	previousLogrusFormatter := logrus.StandardLogger().Formatter
	previousStdlogOutput := stdlog.Writer()
	previousSlog := slog.Default()
	previousGinDefaultWriter := gin.DefaultWriter
	previousGinErrorWriter := gin.DefaultErrorWriter
	previousGinDebugPrint := gin.DebugPrintFunc
	previousGinDebugPrintRoute := gin.DebugPrintRouteFunc

	level, err := logrus.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
	}

	writer := io.Writer(os.Stderr)
	var closer io.Closer = noopCloser{}
	if cfg.LogFileEnabled {
		logDir := strings.TrimSpace(cfg.LogDir)
		if logDir == "" {
			logDir = "/data/logs"
		}
		dailyWriter, err := newDailyFileWriter(logDir, cfg.LogRetentionDays, time.Now)
		if err != nil {
			return nil, err
		}
		writer = io.MultiWriter(os.Stderr, dailyWriter)
		closer = dailyWriter
	}

	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339,
	})
	logrus.SetOutput(writer)
	stdlog.SetOutput(writer)
	slog.SetDefault(slog.New(slog.NewTextHandler(writer, nil)))
	configureGinLogging()
	return &restoreCloser{
		closer:                     closer,
		previousLogrusOutput:       previousLogrusOutput,
		previousLogrusLevel:        previousLogrusLevel,
		previousLogrusFormatter:    previousLogrusFormatter,
		previousStdlogOutput:       previousStdlogOutput,
		previousSlog:               previousSlog,
		previousGinDefaultWriter:   previousGinDefaultWriter,
		previousGinErrorWriter:     previousGinErrorWriter,
		previousGinDebugPrint:      previousGinDebugPrint,
		previousGinDebugPrintRoute: previousGinDebugPrintRoute,
	}, nil
}

type logrusWriter struct {
	level logrus.Level
}

func (w logrusWriter) Write(p []byte) (int, error) {
	message := strings.TrimRight(string(p), "\r\n")
	if message != "" {
		logrus.StandardLogger().Log(w.level, message)
	}
	return len(p), nil
}

func configureGinLogging() {
	gin.DefaultWriter = logrusWriter{level: logrus.InfoLevel}
	gin.DefaultErrorWriter = logrusWriter{level: logrus.ErrorLevel}
	gin.DebugPrintFunc = func(format string, values ...interface{}) {
		logrus.Infof("[GIN-debug] "+strings.TrimRight(format, "\r\n"), values...)
	}
	gin.DebugPrintRouteFunc = func(httpMethod, absolutePath, handlerName string, nuHandlers int) {
		logrus.Infof("[GIN-debug] %-6s %s --> %s (%d handlers)", httpMethod, absolutePath, handlerName, nuHandlers)
	}
}

type dailyFileWriter struct {
	mu            sync.Mutex
	dir           string
	retentionDays int
	now           func() time.Time
	currentDate   string
	file          *os.File
}

func newDailyFileWriter(dir string, retentionDays int, now func() time.Time) (*dailyFileWriter, error) {
	if now == nil {
		now = time.Now
	}
	writer := &dailyFileWriter{
		dir:           dir,
		retentionDays: retentionDays,
		now:           now,
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	if err := writer.rotateLocked(); err != nil {
		return nil, err
	}
	if err := writer.cleanupLocked(); err != nil {
		_ = writer.Close()
		return nil, err
	}
	return writer, nil
}

func (w *dailyFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	date := w.now().Format("2006-01-02")
	if w.file == nil || w.currentDate != date {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
		if err := w.cleanupLocked(); err != nil {
			return 0, err
		}
	}
	return w.file.Write(p)
}

func (w *dailyFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *dailyFileWriter) rotateLocked() error {
	date := w.now().Format("2006-01-02")
	if w.file != nil && w.currentDate == date {
		return nil
	}
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("close log file: %w", err)
		}
	}
	filePath := filepath.Join(w.dir, logFilePrefix+date+".log")
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	w.file = file
	w.currentDate = date
	return nil
}

func (w *dailyFileWriter) cleanupLocked() error {
	if w.retentionDays <= 0 {
		return nil
	}
	cutoff := w.now().AddDate(0, 0, -w.retentionDays)
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return fmt.Errorf("read log dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, logFilePrefix) || !strings.HasSuffix(name, ".log") {
			continue
		}
		datePart := strings.TrimSuffix(strings.TrimPrefix(name, logFilePrefix), ".log")
		logDate, err := time.ParseInLocation("2006-01-02", datePart, time.Local)
		if err != nil {
			continue
		}
		if logDate.Before(dateOnly(cutoff)) {
			if err := os.Remove(filepath.Join(w.dir, name)); err != nil {
				return fmt.Errorf("remove old log file: %w", err)
			}
		}
	}
	return nil
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}

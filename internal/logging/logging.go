package logging

import (
	"errors"
	"fmt"
	"io"
	stdlog "log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

const (
	logFilePrefix         = "cpa-usage-keeper-"
	errorLogFilePrefix    = "cpa-usage-keeper-error-"
	errorLogRetentionDays = 30
)

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

type multiCloser []io.Closer

func (closers multiCloser) Close() error {
	var closeErr error
	for _, closer := range closers {
		closeErr = errors.Join(closeErr, closer.Close())
	}
	return closeErr
}

type restoreCloser struct {
	closer                     io.Closer
	previousLogrusOutput       io.Writer
	previousLogrusLevel        logrus.Level
	previousLogrusFormatter    logrus.Formatter
	previousLogrusHooks        logrus.LevelHooks
	restoreLogrusHooks         bool
	previousStdlogOutput       io.Writer
	previousStdlogFlags        int
	previousStdlogPrefix       string
	previousGinDefaultWriter   io.Writer
	previousGinErrorWriter     io.Writer
	previousGinDebugPrint      func(string, ...interface{})
	previousGinDebugPrintRoute func(string, string, string, int)
	closeOnce                  sync.Once
	closeErr                   error
}

func (c *restoreCloser) Close() error {
	c.closeOnce.Do(func() {
		if c.restoreLogrusHooks {
			logrus.StandardLogger().ReplaceHooks(c.previousLogrusHooks)
		}
		logrus.SetOutput(c.previousLogrusOutput)
		logrus.SetLevel(c.previousLogrusLevel)
		logrus.SetFormatter(c.previousLogrusFormatter)
		stdlog.SetOutput(c.previousStdlogOutput)
		stdlog.SetFlags(c.previousStdlogFlags)
		stdlog.SetPrefix(c.previousStdlogPrefix)
		gin.DefaultWriter = c.previousGinDefaultWriter
		gin.DefaultErrorWriter = c.previousGinErrorWriter
		gin.DebugPrintFunc = c.previousGinDebugPrint
		gin.DebugPrintRouteFunc = c.previousGinDebugPrintRoute
		c.closeErr = c.closer.Close()
	})
	return c.closeErr
}

func resolveLogDir(cfg config.Config) string {
	logDir := strings.TrimSpace(cfg.LogDir)
	if logDir != "" {
		return logDir
	}
	workDir := strings.TrimSpace(cfg.WorkDir)
	if workDir == "" {
		workDir = config.DefaultWorkDir
	}
	return filepath.Join(workDir, filepath.Base(config.DefaultLogDir))
}

func Configure(cfg config.Config) (io.Closer, error) {
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

	level, err := logrus.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
	}

	writer := io.Writer(os.Stderr)
	var closer io.Closer = noopCloser{}
	var errorHook *errorFileHook
	if cfg.LogFileEnabled {
		logDir := resolveLogDir(cfg)
		dailyWriter, err := newDailyFileWriter(logDir, cfg.LogRetentionDays, time.Now)
		if err != nil {
			return nil, fmt.Errorf("initialize combined log writer: %w", err)
		}
		errorWriter, err := newDailyFileWriterWithPrefix(logDir, errorLogFilePrefix, errorLogRetentionDays, time.Now)
		if err != nil {
			initializationErr := fmt.Errorf("initialize dedicated error log writer: %w", err)
			if closeErr := dailyWriter.Close(); closeErr != nil {
				initializationErr = errors.Join(initializationErr, fmt.Errorf("close combined log writer: %w", closeErr))
			}
			return nil, initializationErr
		}
		dailyWriter.maintenanceReporter = func(err error) {
			reportInternalError("combined log maintenance failed", err)
		}
		errorWriter.maintenanceReporter = func(err error) {
			reportInternalError("dedicated error log maintenance failed", err)
		}
		reportInternalError("combined log maintenance failed", dailyWriter.Maintain())
		reportInternalError("dedicated error log maintenance failed", errorWriter.Maintain())
		writer = io.MultiWriter(os.Stderr, NewPlainWriter(dailyWriter))
		closer = multiCloser{dailyWriter, errorWriter}
		errorHook = &errorFileHook{
			writer: errorWriter,
		}
	}

	logger := logrus.StandardLogger()
	var previousLogrusHooks logrus.LevelHooks
	restoreLogrusHooks := errorHook != nil
	if restoreLogrusHooks {
		previousLogrusHooks = logger.ReplaceHooks(make(logrus.LevelHooks))
		configuredHooks := cloneLevelHooks(previousLogrusHooks)
		// 专用 hook 必须先落盘且永远返回 nil，避免它自身的故障阻断已有 hook。
		for _, hookLevel := range errorHook.Levels() {
			configuredHooks[hookLevel] = append([]logrus.Hook{errorHook}, configuredHooks[hookLevel]...)
		}
		logger.ReplaceHooks(configuredHooks)
	}

	logrus.SetLevel(level)
	logrus.SetFormatter(keeperFormatter{})
	logrus.SetOutput(writer)
	stdlog.SetFlags(0)
	stdlog.SetPrefix("")
	stdlog.SetOutput(logrusWriter{level: logrus.InfoLevel})
	configureGinLogging()
	return &restoreCloser{
		closer:                     closer,
		previousLogrusOutput:       previousLogrusOutput,
		previousLogrusLevel:        previousLogrusLevel,
		previousLogrusFormatter:    previousLogrusFormatter,
		previousLogrusHooks:        previousLogrusHooks,
		restoreLogrusHooks:         restoreLogrusHooks,
		previousStdlogOutput:       previousStdlogOutput,
		previousStdlogFlags:        previousStdlogFlags,
		previousStdlogPrefix:       previousStdlogPrefix,
		previousGinDefaultWriter:   previousGinDefaultWriter,
		previousGinErrorWriter:     previousGinErrorWriter,
		previousGinDebugPrint:      previousGinDebugPrint,
		previousGinDebugPrintRoute: previousGinDebugPrintRoute,
	}, nil
}

type errorFileHook struct {
	writer *dailyFileWriter
}

func (*errorFileHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *errorFileHook) Fire(entry *logrus.Entry) error {
	if entry.Level > logrus.ErrorLevel {
		reportInternalError("dedicated error log maintenance failed", h.writer.Maintain())
		return nil
	}
	content, err := (keeperFormatter{}).Format(entry)
	if err != nil {
		reportInternalError("dedicated error log format failed", err)
		return nil
	}
	_, err = NewPlainWriter(h.writer).Write(content)
	reportInternalError("dedicated error log write failed", err)
	return nil
}

func reportInternalError(message string, err error) {
	if err == nil {
		return
	}
	entry := &logrus.Entry{
		Logger:  logrus.StandardLogger(),
		Data:    logrus.Fields{"error": err.Error()},
		Time:    time.Now(),
		Level:   logrus.ErrorLevel,
		Message: message,
	}
	content, formatErr := (keeperFormatter{}).Format(entry)
	if formatErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s: %v: %v\n", message, err, formatErr)
		return
	}
	_, _ = os.Stderr.Write(content)
}

func cloneLevelHooks(hooks logrus.LevelHooks) logrus.LevelHooks {
	cloned := make(logrus.LevelHooks, len(hooks))
	for level, levelHooks := range hooks {
		cloned[level] = append([]logrus.Hook(nil), levelHooks...)
	}
	return cloned
}

// ConfigureBootstrap 让配置加载阶段的致命错误也沿用 Keeper 控制台格式。
func ConfigureBootstrap() {
	logrus.SetLevel(logrus.InfoLevel)
	logrus.SetFormatter(keeperFormatter{})
	logrus.SetOutput(os.Stderr)
}

// NewStandardLogger 为只接受标准库 logger 的组件保留明确的 Logrus 级别。
func NewStandardLogger(level logrus.Level) *stdlog.Logger {
	return stdlog.New(logrusWriter{level: level}, "", 0)
}

// LogTerminalError 保证进程即将失败退出时的原因不被 fatal/panic 阈值过滤。
func LogTerminalError(message string, err error) {
	logTerminal(logrus.ErrorLevel, message, err)
}

// LogTerminalFatal 同步写入 fatal 日志但不退出，让调用方先释放日志文件再结束进程。
func LogTerminalFatal(message string, err error) {
	logTerminal(logrus.FatalLevel, message, err)
}

func logTerminal(level logrus.Level, message string, err error) {
	logger := logrus.StandardLogger()
	previousLevel := logger.GetLevel()
	if !logger.IsLevelEnabled(level) {
		logger.SetLevel(level)
		defer logger.SetLevel(previousLevel)
	}
	logger.WithError(err).Log(level, message)
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
		logrus.Debugf("[GIN-debug] "+strings.TrimRight(format, "\r\n"), values...)
	}
	gin.DebugPrintRouteFunc = func(httpMethod, absolutePath, handlerName string, nuHandlers int) {
		logrus.Debugf("[GIN-debug] %-6s %s --> %s (%d handlers)", httpMethod, absolutePath, handlerName, nuHandlers)
	}
}

type dailyFileWriter struct {
	mu                  sync.Mutex
	dir                 string
	prefix              string
	retentionDays       int
	now                 func() time.Time
	currentDate         string
	lastMaintenanceDate string
	file                *os.File
	closed              bool
	maintenanceReporter func(error)
}

func newDailyFileWriter(dir string, retentionDays int, now func() time.Time) (*dailyFileWriter, error) {
	return newDailyFileWriterWithPrefix(dir, logFilePrefix, retentionDays, now)
}

func newDailyFileWriterWithPrefix(dir, prefix string, retentionDays int, now func() time.Time) (*dailyFileWriter, error) {
	if now == nil {
		now = time.Now
	}
	writer := &dailyFileWriter{
		dir:           dir,
		prefix:        prefix,
		retentionDays: retentionDays,
		now:           now,
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	if err := writer.rotateLocked(now()); err != nil {
		return nil, err
	}
	return writer, nil
}

func (w *dailyFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return 0, os.ErrClosed
	}

	// 一次写入只采样一次时间，避免跨日瞬间的轮转、写入和维护观察到不同日期。
	now := w.now()
	maintenanceErr := w.maintainLocked(now)
	reporter := w.maintenanceReporter

	date := now.Format("2006-01-02")
	var written int
	var writeErr error
	if w.file == nil || w.currentDate != date {
		writeErr = w.rotateLocked(now)
	}
	if writeErr == nil {
		written, writeErr = w.file.Write(p)
		if writeErr == nil && written != len(p) {
			writeErr = io.ErrShortWrite
		}
	}
	w.mu.Unlock()
	if maintenanceErr != nil && reporter != nil {
		reporter(maintenanceErr)
	}
	return written, writeErr
}

func (w *dailyFileWriter) Maintain() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.maintainLocked(w.now())
}

func (w *dailyFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *dailyFileWriter) closeCurrentFileLocked() error {
	if w.file == nil {
		w.currentDate = ""
		return nil
	}
	file := w.file
	w.file = nil
	w.currentDate = ""
	if err := file.Close(); err != nil {
		return fmt.Errorf("close log file: %w", err)
	}
	return nil
}

func (w *dailyFileWriter) rotateLocked(now time.Time) error {
	if w.closed {
		return os.ErrClosed
	}
	date := now.Format("2006-01-02")
	if w.file != nil && w.currentDate == date {
		return nil
	}
	if err := w.closeCurrentFileLocked(); err != nil {
		return err
	}
	filePath := filepath.Join(w.dir, w.prefix+date+".log")
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	w.file = file
	w.currentDate = date
	return nil
}

func (w *dailyFileWriter) maintainLocked(now time.Time) error {
	if w.closed {
		return os.ErrClosed
	}
	date := now.Format("2006-01-02")
	var maintenanceErr error
	// 当天尚未触发文件写入时只关闭跨日旧句柄，不为新日期创建空文件。
	if w.file != nil && w.currentDate != date {
		if err := w.closeCurrentFileLocked(); err != nil {
			maintenanceErr = errors.Join(maintenanceErr, err)
		}
	}
	if w.lastMaintenanceDate == date {
		return maintenanceErr
	}
	// 即使清理失败，同一日期也不反复扫描和刷屏；下一自然日再尝试。
	w.lastMaintenanceDate = date
	return errors.Join(maintenanceErr, w.cleanupLocked(now))
}

func (w *dailyFileWriter) cleanupLocked(now time.Time) error {
	if w.closed {
		return os.ErrClosed
	}
	if w.retentionDays <= 0 {
		return nil
	}
	// 保留配置指定的历史自然日，再加上当天；例如 30 表示历史 30 天加今天。
	cutoff := dateOnly(now.AddDate(0, 0, -w.retentionDays))
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return fmt.Errorf("read log dir: %w", err)
	}
	var cleanupErr error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, w.prefix) || !strings.HasSuffix(name, ".log") {
			continue
		}
		datePart := strings.TrimSuffix(strings.TrimPrefix(name, w.prefix), ".log")
		logDate, err := time.ParseInLocation("2006-01-02", datePart, now.Location())
		if err != nil {
			continue
		}
		if logDate.Before(cutoff) {
			if err := os.Remove(filepath.Join(w.dir, name)); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove old log file %s: %w", name, err))
			}
		}
	}
	return cleanupErr
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}
